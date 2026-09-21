package handler

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// bearer returns the token in an Authorization: Bearer header, or "".
func bearer(r *http.Request) string {
	header := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return ""
	}
	return strings.TrimSpace(token)
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
		next(w, r)
	}
}
