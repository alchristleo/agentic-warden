package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
)

// getBundle serves the machine's user their slice of the current policy:
// group targeting resolved here, repository targeting left for the client.
//
// An organization with no policy yet gets an empty bundle and a 200, for
// the same reason /v1/policy does: a client that falls back on error must
// not be pushed there by the ordinary case.
func (h *Handler) getBundle(w http.ResponseWriter, r *http.Request, machine model.Machine) {
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

	bundle := ruleSet.Slice(ruleSet.GroupsFor(machine.User))
	bundle.User = machine.User
	body, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		h.fail(w, r, fmt.Errorf("encoding the bundle: %w", err))
		return
	}

	// Touching is bookkeeping; a failure is logged, not surfaced, because
	// the machine still needs its policy.
	if err := h.store.TouchMachine(r.Context(), machine.ID, h.Now(), bundle.Version); err != nil {
		h.log.WarnContext(r.Context(), "recording a bundle fetch", "machine", machine.ID, "err", err)
	}

	etag := etagOf(body)
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		h.log.ErrorContext(r.Context(), "writing the bundle response", "err", err)
	}
}
