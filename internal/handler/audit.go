package handler

import (
	"net/http"
	"strconv"
)

const (
	defaultAuditLimit = 50
	maxAuditLimit     = 200
)

// getAudit lists audit events, newest first. before pages backwards by
// event id, which stays stable while new events arrive.
func (h *Handler) getAudit(w http.ResponseWriter, r *http.Request) {
	limit := defaultAuditLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxAuditLimit {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 200")
			return
		}
		limit = n
	}
	var before int64
	if raw := r.URL.Query().Get("before"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "before must be a positive event id")
			return
		}
		before = n
	}
	events, err := h.store.AuditEvents(r.Context(), limit, before)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, events)
}
