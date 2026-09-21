// Package handler is the control plane's HTTP layer.
//
// It is the only layer that knows about HTTP. Everything below it speaks in
// domain types and the sentinel errors in internal/model, which this package
// translates into status codes.
package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/store"
)

// defaultRevisionLimit bounds an unfiltered revision listing.
const defaultRevisionLimit = 50

// Handler serves the control plane API.
type Handler struct {
	store store.Store
	log   *slog.Logger
	// Now supplies the current time. Tests replace it to make stored
	// revisions deterministic.
	Now func() time.Time
	// ManagedValidator checks each rule's managed settings in the agent's
	// own schema when a revision is applied, so a document that would make
	// an agent refuse to start is rejected here, in front of the author,
	// rather than discovered on a developer's machine. Nil skips the check.
	ManagedValidator policy.ManagedValidator
	// AdminToken is the bearer token administrative routes require. Empty
	// disables them with a 503; it never leaves them open.
	AdminToken string
}

// New builds a Handler. A nil logger falls back to the default one.
func New(s store.Store, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	return &Handler{store: s, log: log, Now: time.Now}
}

// Routes returns the router with middleware applied.
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)
	mux.HandleFunc("GET /v1/policy", h.getPolicy)
	mux.HandleFunc("GET /v1/bundle", h.requireMachine(h.getBundle))
	mux.HandleFunc("POST /v1/policy/revisions", h.requireAdmin(h.postRevision))
	mux.HandleFunc("GET /v1/policy/revisions", h.requireAdmin(h.getRevisions))
	mux.HandleFunc("POST /v1/enrollment-tokens", h.requireAdmin(h.postEnrollmentToken))
	mux.HandleFunc("POST /v1/machines/enroll", h.postEnroll)
	mux.HandleFunc("GET /v1/machines", h.requireAdmin(h.getMachines))
	mux.HandleFunc("DELETE /v1/machines/{id}", h.requireAdmin(h.deleteMachine))
	return Logging(h.log)(Recovery(h.log)(mux))
}

func (h *Handler) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz reports whether the control plane can serve policy, which means
// reaching the store. A store with no policy yet is ready: it has nothing to
// serve, but it can serve it.
func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	_, err := h.store.CurrentRuleSet(r.Context())
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		h.log.ErrorContext(r.Context(), "readiness check failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "store unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// getPolicy compiles the current rule set for the requested subject.
//
// An organization with no policy yet gets an empty document and a 200, not an
// error. A client that treats an error as "fall back to the cached policy"
// must not be pushed down that path by the ordinary case of a policy that has
// not been authored yet.
func (h *Handler) getPolicy(w http.ResponseWriter, r *http.Request) {
	subject := policy.Subject{
		Groups: r.URL.Query()["group"],
		Repo:   r.URL.Query().Get("repo"),
	}

	var ruleSet *policy.RuleSet
	revision, err := h.store.CurrentRuleSet(r.Context())
	switch {
	case err == nil:
		ruleSet = &revision.RuleSet
	case errors.Is(err, model.ErrNotFound):
		ruleSet = &policy.RuleSet{}
	default:
		h.fail(w, r, err)
		return
	}

	doc := ruleSet.Compile(subject)
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		h.fail(w, r, fmt.Errorf("encoding the compiled policy: %w", err))
		return
	}

	// The ETag covers the compiled body, so it already accounts for the
	// subject: two groups that compile differently can never share one.
	etag := etagOf(body)
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		h.log.ErrorContext(r.Context(), "writing the policy response", "err", err)
	}
}

func (h *Handler) postRevision(w http.ResponseWriter, r *http.Request) {
	var ruleSet policy.RuleSet
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ruleSet); err != nil {
		writeError(w, http.StatusBadRequest, "the request body is not a valid rule set: "+err.Error())
		return
	}
	if h.ManagedValidator != nil {
		if err := ruleSet.Validate(h.ManagedValidator); err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}

	revision := model.Revision{
		Version:   ruleSet.Version,
		RuleSet:   ruleSet,
		CreatedAt: h.Now(),
		CreatedBy: r.Header.Get("X-Applied-By"),
	}
	if err := h.store.PutRuleSet(r.Context(), revision); err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, revision)
}

func (h *Handler) getRevisions(w http.ResponseWriter, r *http.Request) {
	revisions, err := h.store.Revisions(r.Context(), defaultRevisionLimit)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, revisions)
}

// fail maps a domain error to a status code. It is the only place in the
// control plane that makes that translation.
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, model.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, model.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, model.ErrBadInput):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, model.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, model.ErrOverBudget):
		writeError(w, http.StatusPaymentRequired, err.Error())
	case errors.Is(err, model.ErrUnauthorized):
		writeError(w, http.StatusUnauthorized, "unauthorized")
	default:
		// An unexpected error is logged in full and reported vaguely: the
		// detail is for the operator, not the caller.
		h.log.ErrorContext(r.Context(), "unhandled error", "err", err, "path", r.URL.Path)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func etagOf(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line is already sent; there is nothing to tell the
		// client, so record it and move on.
		slog.Error("encoding a JSON response", "err", err)
	}
}
