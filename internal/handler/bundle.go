package handler

import (
	"context"
	"encoding/base64"
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

	format := ""
	if h.Signer != nil {
		chosen, ok := signing.Choose(signing.ParseFormats(r.Header.Get("X-AW-Signature-Formats")), h.Signer.Current.Formats())
		if !ok {
			// Only a v2-only signer (KMS) gets here: it cannot sign the
			// body itself, so an old aw-sync must upgrade. It keeps the
			// bundle it has meanwhile.
			writeError(w, http.StatusUpgradeRequired, "aw-sync too old for this control plane's signing; upgrade aw-sync to a build that supports signature format v2")
			return
		}
		format = chosen
	}

	// The rollover statement is self-contained and signed by the outgoing
	// key, so it rides on a 304 as well as on a 200 — on a stable fleet
	// every cycle is a 304, and a rotation announced only with changed
	// bytes would never finish there.
	if h.Signer != nil && h.Signer.Previous != nil {
		header, err := signing.SignRollover(r.Context(), cachingSigner{h.sigs, h.Signer.Previous}, h.Signer.Public())
		if err != nil {
			h.signingUnavailable(w, r, err)
			return
		}
		w.Header().Set("X-AW-Key-Rollover", header)
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
		msg := body
		if format == signing.FormatV2 {
			msg = signing.BundleStatement(h.Signer.KeyID(), body)
		}
		sig, err := h.sigs.sign(r.Context(), h.Signer.Current, msg)
		if err != nil {
			h.signingUnavailable(w, r, err)
			return
		}
		w.Header().Set("X-AW-Signature", base64.StdEncoding.EncodeToString(sig))
		w.Header().Set("X-AW-Signature-Format", format)
		w.Header().Set("X-AW-Key-Id", h.Signer.KeyID())
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(body); err != nil {
		h.log.ErrorContext(r.Context(), "writing the bundle response", "err", err)
	}
}

// cachingSigner routes a Signer through the handler's cache, so the
// constant rollover statement is signed once, not once per request.
type cachingSigner struct {
	cache *sigCache
	signing.Signer
}

func (c cachingSigner) Sign(ctx context.Context, msg []byte) ([]byte, error) {
	return c.cache.sign(ctx, c.Signer, msg)
}

// signingUnavailable answers 503 rather than ever serving an unsigned or
// unannounced response. err carries the backend's error code (for KMS,
// the AWS error name); it is logged, never sent.
func (h *Handler) signingUnavailable(w http.ResponseWriter, r *http.Request, err error) {
	h.log.ErrorContext(r.Context(), "signing a bundle response", "err", err)
	writeError(w, http.StatusServiceUnavailable, "signing unavailable")
}
