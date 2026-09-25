// Package sso signs console users in with OpenID Connect: authorization
// code flow with PKCE, awd as a confidential client. It knows nothing of
// cookies or sessions; the handler stores the Attempt between Start and
// Finish.
package sso

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var (
	ErrUnavailable  = errors.New("sso: identity provider unavailable")
	ErrExchange     = errors.New("sso: code exchange failed")
	ErrInvalidToken = errors.New("sso: invalid ID token")
	ErrUnverified   = errors.New("sso: email not verified")
	ErrNoUser       = errors.New("sso: user claim missing")
)

// Config configures a Client.
type Config struct {
	Issuer, ClientID, ClientSecret, RedirectURL string
	// UserClaim names the claim that identifies the user; default "email".
	UserClaim string
	// HTTPClient reaches the IdP; nil means a client with a 10 s timeout.
	HTTPClient *http.Client
}

// Attempt is one login in flight: what Finish needs to check the IdP's
// answer. All three values are random base64url, so "." cannot occur in
// them.
type Attempt struct{ State, Nonce, Verifier string }

// Encode packs an Attempt into a cookie value.
func (a Attempt) Encode() string { return a.State + "." + a.Nonce + "." + a.Verifier }

// DecodeAttempt unpacks Encode's output.
func DecodeAttempt(s string) (Attempt, bool) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return Attempt{}, false
	}
	return Attempt{State: parts[0], Nonce: parts[1], Verifier: parts[2]}, true
}

// Client talks to one IdP. Discovery is lazy and retried until it succeeds,
// so an IdP that is down when awd starts does not need a restart.
type Client struct {
	cfg  Config
	http *http.Client

	mu       sync.Mutex
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// New builds a Client without contacting the IdP.
func New(cfg Config) *Client {
	if cfg.UserClaim == "" {
		cfg.UserClaim = "email"
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{cfg: cfg, http: hc}
}

// Discover fetches the IdP's configuration if it has not been fetched.
func (c *Client) Discover(ctx context.Context) error {
	_, _, err := c.discovered(ctx)
	return err
}

func (c *Client) discovered(ctx context.Context) (*oauth2.Config, *oidc.IDTokenVerifier, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.oauth != nil {
		return c.oauth, c.verifier, nil
	}
	// The provider keeps this context for later JWKS fetches, so it must
	// not be the request's: WithoutCancel keeps the client, drops the
	// deadline.
	pctx := oidc.ClientContext(context.WithoutCancel(ctx), c.http)
	provider, err := oidc.NewProvider(pctx, c.cfg.Issuer)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	c.oauth = &oauth2.Config{
		ClientID: c.cfg.ClientID, ClientSecret: c.cfg.ClientSecret,
		RedirectURL: c.cfg.RedirectURL, Endpoint: provider.Endpoint(),
		Scopes: []string{oidc.ScopeOpenID, "email", "profile"},
	}
	c.verifier = provider.Verifier(&oidc.Config{ClientID: c.cfg.ClientID})
	return c.oauth, c.verifier, nil
}

// Start begins a login and returns the IdP URL to redirect to.
func (c *Client) Start(ctx context.Context) (Attempt, string, error) {
	oc, _, err := c.discovered(ctx)
	if err != nil {
		return Attempt{}, "", err
	}
	a := Attempt{State: random(), Nonce: random(), Verifier: oauth2.GenerateVerifier()}
	url := oc.AuthCodeURL(a.State, oidc.Nonce(a.Nonce), oauth2.S256ChallengeOption(a.Verifier))
	return a, url, nil
}

// Finish exchanges the code, verifies the ID token against the attempt and
// returns the user named by the configured claim.
func (c *Client) Finish(ctx context.Context, a Attempt, code string) (string, error) {
	oc, verifier, err := c.discovered(ctx)
	if err != nil {
		return "", err
	}
	ctx = oidc.ClientContext(ctx, c.http)
	tok, err := oc.Exchange(ctx, code, oauth2.VerifierOption(a.Verifier))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrExchange, err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return "", fmt.Errorf("%w: no id_token in the token response", ErrInvalidToken)
	}
	idt, err := verifier.Verify(ctx, raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(a.Nonce)) != 1 {
		return "", fmt.Errorf("%w: nonce mismatch", ErrInvalidToken)
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if v, present := claims["email_verified"]; present && !truthy(v) {
		return "", ErrUnverified
	}
	user, _ := claims[c.cfg.UserClaim].(string)
	user = strings.TrimSpace(user)
	if user == "" {
		return "", fmt.Errorf("%w: %q", ErrNoUser, c.cfg.UserClaim)
	}
	return user, nil
}

// truthy accepts the boolean true and the string "true": some IdPs send
// email_verified as a string.
func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true")
	}
	return false
}

func random() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("sso: reading random bytes: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
