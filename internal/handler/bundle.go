package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/signing"
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

	// Group resolution is the union of what the policy authored, what the
	// IdP last exported and what SCIM provisioned; with neither feed, the
	// authored map alone, exactly as before either existed.
	groups, err := h.resolveGroups(r.Context(), ruleSet, machine.User)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	bundle := ruleSet.Slice(groups.Effective)
	bundle.User = machine.User
	body, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		h.fail(w, r, fmt.Errorf("encoding the bundle: %w", err))
		return
	}

	// Touching is bookkeeping; a failure is logged, not surfaced, because
	// the machine still needs its policy.
	if err := h.store.TouchMachine(r.Context(), machine.ID, h.Now(), bundle.Version, r.Header.Get("X-AW-Key-Id")); err != nil {
		h.log.WarnContext(r.Context(), "recording a bundle fetch", "machine", machine.ID, "err", err)
	}

	// The rollover statement is self-contained and signed by the outgoing
	// key, so it stands on its own and rides on a 304 as well as on a 200.
	// It has to: on a fleet whose policy is stable every cycle is a 304, and
	// a rotation announced only with changed bundle bytes would never finish
	// there. The operator would then drop the previous key and strand every
	// machine still pinned to it.
	if h.Signer != nil && h.Signer.Previous != nil {
		w.Header().Set("X-AW-Key-Rollover", signing.SignRollover(h.Signer.Previous, h.Signer.Public()))
	}
	etag := etagOf(body)
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		// A 304 has no body to sign, and the machine still holds the
		// signature it verified when it first received these bytes.
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if h.Signer != nil {
		w.Header().Set("X-AW-Signature", signing.Sign(h.Signer.Key, body))
		w.Header().Set("X-AW-Key-Id", h.Signer.KeyID())
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		h.log.ErrorContext(r.Context(), "writing the bundle response", "err", err)
	}
}
