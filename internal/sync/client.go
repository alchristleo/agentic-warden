package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
)

// maxBundleBytes bounds what is read from the control plane. A bundle is a
// few kilobytes; anything near this is a broken server, not a policy.
const maxBundleBytes = 4 << 20

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
	MachineID  string `json:"machineId"`
	Credential string `json:"credential"`
	User       string `json:"user"`
}

// Fetched is one answer to a conditional bundle request. Unchanged means
// the server answered 304 and Bundle is nil.
type Fetched struct {
	Bundle    *policy.Bundle
	ETag      string
	Unchanged bool
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
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, maxBundleBytes))
	if resp.StatusCode != http.StatusCreated {
		return Enrollment{}, fmt.Errorf("sync: enrollment refused: %s: %s", resp.Status, strings.TrimSpace(string(payload)))
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
func (c *Client) Fetch(ctx context.Context, credential, etag string) (Fetched, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/v1/bundle"), nil)
	if err != nil {
		return Fetched{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+credential)
	req.Header.Set("User-Agent", "aw-sync")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return Fetched{}, fmt.Errorf("sync: reaching the control plane at %s: %w", c.Server, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		return Fetched{ETag: etag, Unchanged: true}, nil
	case http.StatusUnauthorized:
		return Fetched{}, fmt.Errorf("sync: %w: the control plane rejected this machine's credential; re-enroll", model.ErrUnauthorized)
	case http.StatusOK:
	default:
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Fetched{}, fmt.Errorf("sync: the control plane answered %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxBundleBytes+1))
	if err != nil {
		return Fetched{}, fmt.Errorf("sync: reading the bundle: %w", err)
	}
	if len(payload) > maxBundleBytes {
		return Fetched{}, fmt.Errorf("sync: the bundle exceeds %d bytes", maxBundleBytes)
	}
	var bundle policy.Bundle
	if err := json.Unmarshal(payload, &bundle); err != nil {
		return Fetched{}, fmt.Errorf("sync: parsing the bundle: %w", err)
	}
	return Fetched{Bundle: &bundle, ETag: resp.Header.Get("ETag")}, nil
}

func (c *Client) endpoint(path string) string {
	return strings.TrimSuffix(c.Server, "/") + path
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: defaultTimeout}
}
