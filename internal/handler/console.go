package handler

import (
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/acme/agent-wrapper/internal/console/authz"
	"github.com/acme/agent-wrapper/internal/console/session"
	"github.com/acme/agent-wrapper/internal/console/sso"
	"github.com/acme/agent-wrapper/internal/model"
)

const (
	sessionCookie = "aw_session"
	loginCookie   = "aw_login"
	consoleCSP    = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
)

// Console is the web console's wiring. A nil Handler.Console means the
// console is off and every /console path is a 404.
type Console struct {
	// PublicURL is awd's external origin: the redirect URI's base and the
	// only Origin accepted on console writes.
	PublicURL *url.URL
	SSO       *sso.Client
	Sessions  *session.Manager
	Authz     *authz.Checker
	// Assets is the built SPA (console.Assets() in production).
	Assets fs.FS
}

func (c *Console) origin() string { return c.PublicURL.Scheme + "://" + c.PublicURL.Host }

// secure reports whether cookies carry Secure: always, except plain http
// on loopback for development.
func (c *Console) secure() bool { return c.PublicURL.Scheme == "https" }

func (h *Handler) consoleRoutes(mux *http.ServeMux) {
	if h.Console == nil {
		return
	}
	mux.Handle("GET /console/", h.consoleStatic())
	mux.HandleFunc("GET /console/auth/login", h.consoleLogin)
	mux.HandleFunc("GET /console/auth/callback", h.consoleCallback)
	mux.HandleFunc("POST /console/auth/logout", h.consoleLogout)
	mux.HandleFunc("GET /console/api/me", h.consoleMe)
	mux.HandleFunc("GET /console/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
}

func securityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", consoleCSP)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
}

// consoleStatic serves hashed assets as immutable and every other path as
// index.html, so the SPA's client-side routes survive a reload.
func (h *Handler) consoleStatic() http.Handler {
	files := http.StripPrefix("/console/", http.FileServerFS(h.Console.Assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		securityHeaders(w)
		name := strings.TrimPrefix(r.URL.Path, "/console/")
		if strings.HasPrefix(name, "assets/") {
			if _, err := fs.Stat(h.Console.Assets, name); err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			files.ServeHTTP(w, r)
			return
		}
		index, err := fs.ReadFile(h.Console.Assets, "index.html")
		if err != nil {
			h.fail(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}

// consoleUser authenticates a console request and re-checks admin rights.
// On failure it has written the response and returns ok=false.
func (h *Handler) consoleUser(w http.ResponseWriter, r *http.Request) (model.ConsoleSession, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		writeError(w, http.StatusUnauthorized, "sign in")
		return model.ConsoleSession{}, false
	}
	s, err := h.Console.Sessions.Lookup(r.Context(), c.Value)
	switch {
	case errors.Is(err, session.ErrNoSession), errors.Is(err, session.ErrExpired):
		h.clearCookie(w, sessionCookie, "/")
		writeError(w, http.StatusUnauthorized, "sign in")
		return model.ConsoleSession{}, false
	case err != nil:
		h.log.ErrorContext(r.Context(), "console session lookup failed", "err", err)
		writeError(w, http.StatusServiceUnavailable, "session store unavailable")
		return model.ConsoleSession{}, false
	}
	admin, err := h.Console.Authz.Admin(r.Context(), s.User)
	switch {
	case errors.Is(err, authz.ErrNoGroupData):
		writeError(w, http.StatusServiceUnavailable, authz.ErrNoGroupData.Error())
		return model.ConsoleSession{}, false
	case err != nil:
		h.log.ErrorContext(r.Context(), "console admin check failed", "err", err)
		writeError(w, http.StatusServiceUnavailable, "group data unavailable")
		return model.ConsoleSession{}, false
	case !admin:
		if err := h.Console.Sessions.DeleteUser(r.Context(), s.User); err != nil {
			h.log.ErrorContext(r.Context(), "deleting sessions of a removed admin", "err", err)
		}
		h.clearCookie(w, sessionCookie, "/")
		writeError(w, http.StatusForbidden, "not a console admin")
		return model.ConsoleSession{}, false
	}
	return s, true
}

// sameOrigin guards console writes. Browsers send Origin on every
// non-GET fetch; a missing one means the request did not come from the SPA.
func (h *Handler) sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	if r.Header.Get("Origin") != h.Console.origin() {
		writeError(w, http.StatusForbidden, "cross-origin request refused")
		return false
	}
	return true
}

// consoleAdmin is requireAdmin's console path.
func (h *Handler) consoleAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, ok := h.consoleUser(w, r)
		if !ok || !h.sameOrigin(w, r) {
			return
		}
		next(w, withActor(r, s.User))
	}
}

func (h *Handler) consoleMe(w http.ResponseWriter, r *http.Request) {
	s, ok := h.consoleUser(w, r)
	if !ok {
		return
	}
	body := map[string]any{"user": s.User, "expiresAt": h.Console.Sessions.ExpiresAt(s).UTC().Format(time.RFC3339)}
	if h.Signer != nil {
		body["signingKeyId"] = h.Signer.KeyID()
	}
	writeJSON(w, http.StatusOK, body)
}

func (h *Handler) clearCookie(w http.ResponseWriter, name, path string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: path, MaxAge: -1,
		HttpOnly: true, Secure: h.Console.secure(), SameSite: http.SameSiteStrictMode})
}
