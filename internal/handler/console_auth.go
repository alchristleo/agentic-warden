package handler

import (
	"crypto/subtle"
	"errors"
	"html/template"
	"net/http"
	"strings"

	"github.com/acme/agent-wrapper/internal/console/authz"
	"github.com/acme/agent-wrapper/internal/console/sso"
	"github.com/acme/agent-wrapper/internal/model"
)

var loginErrorPage = template.Must(template.New("e").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8">
<title>Sign-in failed</title></head><body><main><h1>Sign-in failed</h1><p>{{.}}</p>
<p><a href="/console/">Back to the console</a></p></main></body></html>`))

func (h *Handler) loginError(w http.ResponseWriter, status int, message string) {
	securityHeaders(w)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = loginErrorPage.Execute(w, message)
}

func (h *Handler) consoleLogin(w http.ResponseWriter, r *http.Request) {
	attempt, authURL, err := h.Console.SSO.Start(r.Context())
	if err != nil {
		h.log.WarnContext(r.Context(), "console login: identity provider unavailable", "err", err)
		h.loginError(w, http.StatusServiceUnavailable, "The identity provider is unavailable. Try again shortly.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: loginCookie, Value: attempt.Encode(), Path: "/console/auth",
		MaxAge: 600, HttpOnly: true, Secure: h.Console.secure(), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, authURL, http.StatusFound)
}

// sanitizeIdPError keeps the IdP's `error` query parameter safe for the
// audit log: bytes outside RFC 6749's NQCHAR range (%x20-21 / %x23-5B /
// %x5D-7E — visible ASCII minus backslash and double-quote) are dropped,
// and the result is capped at 64 bytes, so a hostile or misconfigured IdP
// cannot stuff control characters or an unbounded string into an audit row.
func sanitizeIdPError(raw string) string {
	var b strings.Builder
	for i := 0; i < len(raw) && b.Len() < 64; i++ {
		c := raw[i]
		if (c >= 0x20 && c <= 0x21) || (c >= 0x23 && c <= 0x5B) || (c >= 0x5D && c <= 0x7E) {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// denied records a refused login. The audit write's own failure is logged,
// not surfaced: the user is already being refused.
func (h *Handler) denied(r *http.Request, actor, reason string) {
	event := model.AuditEvent{At: h.Now(), Actor: actor, Action: model.AuditLoginDenied, Detail: map[string]any{"reason": reason}}
	if err := h.store.RecordAudit(r.Context(), event); err != nil {
		h.log.ErrorContext(r.Context(), "recording a denied console login", "err", err)
	}
}

func (h *Handler) consoleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	cookie, cookieErr := r.Cookie(loginCookie)
	h.clearCookie(w, loginCookie, "/console/auth")
	var attempt sso.Attempt
	ok := cookieErr == nil
	if ok {
		attempt, ok = sso.DecodeAttempt(cookie.Value)
	}
	if !ok || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(attempt.State)) != 1 {
		// Back button onto a used callback: a signed-in user just goes home.
		if c, err := r.Cookie(sessionCookie); err == nil {
			if _, err := h.Console.Sessions.Lookup(r.Context(), c.Value); err == nil {
				http.Redirect(w, r, "/console/", http.StatusFound)
				return
			}
		}
		h.loginError(w, http.StatusBadRequest, "This sign-in link is stale. Start again from the console.")
		return
	}
	if idpErr := q.Get("error"); idpErr != "" {
		h.denied(r, "anonymous", "idp: "+sanitizeIdPError(idpErr))
		h.loginError(w, http.StatusUnauthorized, "The identity provider refused the sign-in.")
		return
	}

	user, err := h.Console.SSO.Finish(r.Context(), attempt, q.Get("code"))
	switch {
	case errors.Is(err, sso.ErrUnavailable):
		h.loginError(w, http.StatusServiceUnavailable, "The identity provider is unavailable. Try again shortly.")
		return
	case errors.Is(err, sso.ErrExchange):
		h.log.WarnContext(r.Context(), "console login: code exchange failed", "err", err)
		h.loginError(w, http.StatusBadGateway, "The identity provider did not complete the sign-in.")
		return
	case errors.Is(err, sso.ErrInvalidToken):
		h.log.WarnContext(r.Context(), "console login: invalid ID token", "err", err)
		h.denied(r, "anonymous", err.Error())
		h.loginError(w, http.StatusUnauthorized, "The identity provider's answer could not be verified.")
		return
	case errors.Is(err, sso.ErrUnverified), errors.Is(err, sso.ErrNoUser):
		h.denied(r, "anonymous", err.Error())
		h.loginError(w, http.StatusForbidden, "Your account has no verified identity awd can use.")
		return
	case err != nil:
		h.fail(w, r, err)
		return
	}

	admin, err := h.Console.Authz.Admin(r.Context(), user)
	switch {
	case errors.Is(err, authz.ErrNoGroupData):
		h.loginError(w, http.StatusServiceUnavailable, "The console needs group data (SCIM or awd groups apply).")
		return
	case err != nil:
		h.log.ErrorContext(r.Context(), "console login: admin check failed", "err", err)
		h.loginError(w, http.StatusServiceUnavailable, "Group data is unavailable. Try again shortly.")
		return
	case !admin:
		h.denied(r, user, "not in "+h.Console.Authz.Group)
		h.loginError(w, http.StatusForbidden, "You are not a console admin.")
		return
	}

	token, _, err := h.Console.Sessions.Create(r.Context(), user)
	if err != nil {
		h.log.ErrorContext(r.Context(), "console login: creating session", "err", err)
		h.loginError(w, http.StatusServiceUnavailable, "Sign-in is unavailable. Try again shortly.")
		return
	}
	if err := h.store.RecordAudit(r.Context(), model.AuditEvent{At: h.Now(), Actor: user, Action: model.AuditLogin}); err != nil {
		// No unaudited access: take the session back.
		_ = h.Console.Sessions.Delete(r.Context(), token)
		h.log.ErrorContext(r.Context(), "console login: recording audit", "err", err)
		h.loginError(w, http.StatusInternalServerError, "Sign-in could not be recorded.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/",
		HttpOnly: true, Secure: h.Console.secure(), SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/console/", http.StatusFound)
}

func (h *Handler) consoleLogout(w http.ResponseWriter, r *http.Request) {
	if !h.sameOrigin(w, r) {
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if s, err := h.Console.Sessions.Lookup(r.Context(), c.Value); err == nil {
			if err := h.store.RecordAudit(r.Context(), model.AuditEvent{At: h.Now(), Actor: s.User, Action: model.AuditLogout}); err != nil {
				h.log.ErrorContext(r.Context(), "recording console logout", "err", err)
			}
		}
		if err := h.Console.Sessions.Delete(r.Context(), c.Value); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	h.clearCookie(w, sessionCookie, "/")
	w.WriteHeader(http.StatusNoContent)
}
