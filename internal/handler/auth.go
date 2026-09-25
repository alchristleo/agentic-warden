package handler

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/acme/agent-wrapper/internal/credential"
	"github.com/acme/agent-wrapper/internal/model"
)

// bearer returns the token in an Authorization: Bearer header, or "". The
// scheme is matched case-insensitively, as RFC 7235 requires.
func bearer(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if len(header) < 7 || !strings.EqualFold(header[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[7:])
}

// requireAdmin admits a request only with the configured admin token. With
// no token configured the route answers 503 rather than opening: an
// operator who forgot to set AWD_ADMIN_TOKEN must find out from the error,
// not from an audit.
func (h *Handler) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.AdminToken == "" {
			writeError(w, http.StatusServiceUnavailable, "admin token not configured")
			return
		}
		presented := bearer(r)
		if subtle.ConstantTimeCompare([]byte(presented), []byte(h.AdminToken)) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, withActor(r, "token:"+r.Header.Get("X-Applied-By")))
	}
}

// requireMachine admits a request carrying an enrolled machine's credential
// and hands the machine to the handler. Lookup is by the credential's hash,
// so the store never sees the secret.
func (h *Handler) requireMachine(next func(http.ResponseWriter, *http.Request, model.Machine)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		presented := bearer(r)
		if presented == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		machine, err := h.store.MachineByCredential(r.Context(), credential.Hash(presented))
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			h.fail(w, r, err)
			return
		}
		next(w, r, machine)
	}
}
