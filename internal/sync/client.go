package sync

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/signing"
)

// maxBundleBytes bounds what is read from the control plane. A bundle is a
// few kilobytes; anything near this is a broken server, not a policy.
const maxBundleBytes = 4 << 20

// maxErrorBody bounds what is read from a non-success response. An error
// body is a short message; a huge one is a broken server, not worth holding
// in memory to report.
const maxErrorBody = 4096

// defaultTimeout bounds one exchange when the caller supplies no client. A
// timer-driven cycle can afford to wait, but not forever.
const defaultTimeout = 15 * time.Second

// Client talks to the control plane on behalf of one machine.
type Client struct {
	// Server is the control plane's base URL.
	Server string
	// HTTP overrides the client used; nil means one with defaultTimeout.
	HTTP *http.Client
}

// Enrollment is what the control plane returns for a consumed token. The
// credential is shown once; the control plane keeps only its hash.
type Enrollment struct {
	// MachineID identifies this machine to the control plane going forward.
	MachineID string `json:"machineId"`
	// Credential authenticates this machine's future bundle fetches; it is
	// shown once here and never retrievable again.
	Credential string `json:"credential"`
	// User is the account this machine's enrollment token was minted for.
	User string `json:"user"`
	// PublicKey is the control plane's bundle signing key, pinned here at
	// the one moment this machine has authenticated to the server with a
	// token an operator minted. Empty when the server does not sign.
	PublicKey string `json:"publicKey,omitempty"`
	// KeyID names that key in listings and headers.
	KeyID string `json:"keyId,omitempty"`
}

// Fetched is one answer to a conditional bundle request. Unchanged means
// the server answered 304 and Bundle is nil.
type Fetched struct {
	// Bundle is the decoded policy, or nil when Unchanged is true.
	Bundle *policy.Bundle
	// ETag identifies the bundle revision, for the next call's If-None-Match.
	ETag string
	// Unchanged means the server answered 304: the caller's ETag is still
	// current and Bundle was not sent.
	Unchanged bool
	// Raw is the response body exactly as the server wrote it. The
	// signature covers these bytes, so this — not a re-encoding of Bundle —
	// is what goes on disk.
	Raw []byte
	// Signature is what the server sent, for the caller to store beside the
	// bundle. Empty for an unsigned deployment. The key it belongs to is not
	// carried with it: the caller names the key it actually verified under,
	// which after a rollover is the new one, not whatever header the server
	// sent.
	Signature string
	// Rollover is the new key this fetch repinned to, or nil. The caller
	// persists it: the verification already happened here.
	Rollover *signing.Rollover
	// Format is the signature format the server used ("v1" or "v2"), empty
	// when the response was unsigned. It decides the signature file's
	// first token.
	Format string
}

// Enroll exchanges a single-use token for this machine's credential.
func (c *Client) Enroll(ctx context.Context, token, name, goos string) (Enrollment, error) {
	body, err := json.Marshal(map[string]string{"token": token, "name": name, "os": goos})
	if err != nil {
		return Enrollment{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/v1/machines/enroll"), bytes.NewReader(body))
	if err != nil {
		return Enrollment{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return Enrollment{}, fmt.Errorf("sync: reaching the control plane at %s: %w", c.Server, err)
	}
	defer resp.Body.Close()
	// The body is read with the same cap as a bundle, not maxErrorBody: a
	// successful enrollment's body must decode in full, and maxErrorBody
	// applies only to the message built below for a non-201 response.
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxBundleBytes+1))
	if err != nil {
		return Enrollment{}, fmt.Errorf("sync: reading the enrollment response: %w", err)
	}
	if len(payload) > maxBundleBytes {
		return Enrollment{}, fmt.Errorf("sync: the enrollment response exceeds %d bytes", maxBundleBytes)
	}
	if resp.StatusCode != http.StatusCreated {
		msg := payload
		if len(msg) > maxErrorBody {
			msg = msg[:maxErrorBody]
		}
		return Enrollment{}, fmt.Errorf("sync: enrollment refused: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	var e Enrollment
	if err := json.Unmarshal(payload, &e); err != nil {
		return Enrollment{}, fmt.Errorf("sync: decoding the enrollment: %w", err)
	}
	if e.MachineID == "" || e.Credential == "" {
		return Enrollment{}, errors.New("sync: the control plane returned an incomplete enrollment")
	}
	return e, nil
}

// Fetch asks for the machine's bundle, revalidating etag when it is not
// empty. A 401 is reported as model.ErrUnauthorized so the caller can say
// "revoked" rather than "failed".
func (c *Client) Fetch(ctx context.Context, credential, etag string, pinned ed25519.PublicKey) (Fetched, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/v1/bundle"), nil)
	if err != nil {
		return Fetched{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("User-Agent", "aw-sync")
	// Advertising v2 is what lets a control plane whose key lives in KMS
	// sign for this machine; one that holds a seed picks v2 too.
	req.Header.Set("X-AW-Signature-Formats", signing.FormatV1+", "+signing.FormatV2)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if pinned != nil {
		// The control plane records this, so an operator can see which
		// machines have picked up a rotation and when it is safe to retire
		// the old key.
		req.Header.Set("X-AW-Key-Id", signing.KeyID(pinned))
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return Fetched{}, fmt.Errorf("sync: reaching the control plane at %s: %w", c.Server, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		// A 304 carries no body and no signature, but it can carry a
		// rollover: the statement is signed by the pinned key and says
		// nothing about the bundle, so it verifies on its own. Reading it
		// here is what lets a rotation finish on a fleet whose policy never
		// changes, where every cycle is a 304.
		rollover, err := rolloverFrom(resp, pinned)
		if err != nil {
			return Fetched{}, err
		}
		return Fetched{ETag: etag, Unchanged: true, Rollover: rollover}, nil
	case http.StatusUnauthorized:
		return Fetched{}, fmt.Errorf("sync: %w: the control plane rejected this machine's credential; re-enroll", model.ErrUnauthorized)
	case http.StatusOK:
	default:
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return Fetched{}, fmt.Errorf("sync: the control plane answered %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxBundleBytes+1))
	if err != nil {
		return Fetched{}, fmt.Errorf("sync: reading the bundle: %w", err)
	}
	if len(payload) > maxBundleBytes {
		return Fetched{}, fmt.Errorf("sync: the bundle exceeds %d bytes", maxBundleBytes)
	}
	signature := resp.Header.Get("X-AW-Signature")
	rollover, err := rolloverFrom(resp, pinned)
	if err != nil {
		return Fetched{}, err
	}
	format := ""
	if signature != "" {
		format = resp.Header.Get("X-AW-Signature-Format")
		if format == "" {
			format = signing.FormatV1 // an awd from before v2
		}
	}
	if pinned != nil {
		verifier := pinned
		if rollover != nil {
			verifier = rollover.PublicKey
		}
		if signature == "" {
			return Fetched{}, errors.New("sync: this machine pins a signing key but the control plane sent no signature")
		}
		if format != signing.FormatV1 && format != signing.FormatV2 {
			return Fetched{}, fmt.Errorf("sync: the control plane used an unknown signature format %q", format)
		}
		if format == signing.FormatV2 {
			if served := resp.Header.Get("X-AW-Key-Id"); served != signing.KeyID(verifier) {
				return Fetched{}, fmt.Errorf("sync: the bundle is signed by key %s but this machine trusts key %s", served, signing.KeyID(verifier))
			}
		}
		if !signing.VerifyBundle(format, verifier, payload, signature) {
			return Fetched{}, fmt.Errorf("sync: the bundle is not signed by key %s", signing.KeyID(verifier))
		}
	}

	var bundle policy.Bundle
	if err := json.Unmarshal(payload, &bundle); err != nil {
		return Fetched{}, fmt.Errorf("sync: parsing the bundle: %w", err)
	}
	return Fetched{Bundle: &bundle, Raw: payload, Signature: signature, ETag: resp.Header.Get("ETag"), Rollover: rollover, Format: format}, nil
}

// rolloverFrom reads the rollover a response announces, checked against the
// key this machine has pinned. A rollover is the only thing that may move
// the pin, and only the pinned key can sign one, so trust chains back to
// enrollment; a machine that pins nothing has nothing to check a statement
// with and ignores the header entirely. A statement that does not check out
// is an error rather than a header quietly dropped, because on a 304 there
// is no bundle whose own failure would otherwise surface it.
func rolloverFrom(resp *http.Response, pinned ed25519.PublicKey) (*signing.Rollover, error) {
	header := resp.Header.Get("X-AW-Key-Rollover")
	if pinned == nil || header == "" {
		return nil, nil
	}
	next, err := signing.VerifyRollover(pinned, header)
	if err != nil {
		return nil, fmt.Errorf("sync: %w", err)
	}
	return &next, nil
}

func (c *Client) endpoint(path string) string {
	return strings.TrimSuffix(c.Server, "/") + path
}

// client returns the configured HTTP client, or a fresh one with
// defaultTimeout when none was set. A timer-driven cycle makes at most one
// or two requests per run, so there is no connection pool worth keeping
// warm between calls; building one here is cheap enough.
func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: defaultTimeout}
}
