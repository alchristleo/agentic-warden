package sso_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/acme/agent-wrapper/internal/console/oidctest"
	"github.com/acme/agent-wrapper/internal/console/sso"
)

func client(p *oidctest.Provider, claim string) *sso.Client {
	return sso.New(sso.Config{Issuer: p.Issuer, ClientID: p.ClientID, ClientSecret: p.ClientSecret,
		RedirectURL: "http://127.0.0.1:1/console/auth/callback", UserClaim: claim})
}

func login(t *testing.T, p *oidctest.Provider, c *sso.Client) (string, error) {
	t.Helper()
	attempt, authURL, err := c.Start(context.Background())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	u, _ := url.Parse(authURL)
	if u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("nonce") != attempt.Nonce {
		t.Fatalf("auth URL lacks PKCE or nonce: %s", authURL)
	}
	code, state := p.Login(t, authURL)
	if state != attempt.State {
		t.Fatalf("state %q, want %q", state, attempt.State)
	}
	return c.Finish(context.Background(), attempt, code)
}

func TestLoginReturnsTheConfiguredClaim(t *testing.T) {
	p := oidctest.Serve(t)
	p.Set(func(p *oidctest.Provider) {
		p.User = "alice@example.com"
		p.Claims = map[string]any{"preferred_username": "alice@corp"}
	})
	if user, err := login(t, p, client(p, "")); err != nil || user != "alice@example.com" {
		t.Fatalf("default claim → %q, %v", user, err)
	}
	if user, err := login(t, p, client(p, "preferred_username")); err != nil || user != "alice@corp" {
		t.Fatalf("preferred_username → %q, %v", user, err)
	}
}

func TestLoginRejectsBadTokens(t *testing.T) {
	cases := []struct {
		name  string
		setup func(p *oidctest.Provider)
		want  error
	}{
		{"wrong nonce", func(p *oidctest.Provider) { p.Tamper = oidctest.WrongNonce }, sso.ErrInvalidToken},
		{"wrong audience", func(p *oidctest.Provider) { p.Tamper = oidctest.WrongAudience }, sso.ErrInvalidToken},
		{"expired", func(p *oidctest.Provider) { p.Tamper = oidctest.Expired }, sso.ErrInvalidToken},
		{"bad signature", func(p *oidctest.Provider) { p.Tamper = oidctest.BadSignature }, sso.ErrInvalidToken},
		{"email not verified", func(p *oidctest.Provider) { p.Claims = map[string]any{"email_verified": false} }, sso.ErrUnverified},
		{"email_verified as string false", func(p *oidctest.Provider) { p.Claims = map[string]any{"email_verified": "false"} }, sso.ErrUnverified},
		{"no user claim", func(p *oidctest.Provider) { p.Claims = map[string]any{"email": nil} }, sso.ErrNoUser},
		{"blank user claim", func(p *oidctest.Provider) { p.Claims = map[string]any{"email": "  "} }, sso.ErrNoUser},
		{"token endpoint fails", func(p *oidctest.Provider) { p.FailToken = true }, sso.ErrExchange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := oidctest.Serve(t)
			p.Set(func(p *oidctest.Provider) { p.User = "alice@example.com"; tc.setup(p) })
			if _, err := login(t, p, client(p, "")); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestMissingEmailVerifiedIsAccepted(t *testing.T) {
	p := oidctest.Serve(t)
	p.Set(func(p *oidctest.Provider) { p.User = "a@x"; p.Claims = map[string]any{"email_verified": nil} })
	if _, err := login(t, p, client(p, "")); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestDiscoveryFailureIsUnavailableAndRetried(t *testing.T) {
	p := oidctest.Serve(t)
	c := sso.New(sso.Config{Issuer: "http://127.0.0.1:1", ClientID: "x", ClientSecret: "y", RedirectURL: "http://127.0.0.1:2/cb"})
	if _, _, err := c.Start(context.Background()); !errors.Is(err, sso.ErrUnavailable) {
		t.Fatalf("Start = %v, want ErrUnavailable", err)
	}
	// Same client, working issuer: discovery is attempted again, not cached as failed.
	c = client(p, "")
	if err := c.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestDiscoveryIsRetriedAfterFailureOnTheSameClient proves retry-after-failure
// on the SAME *sso.Client: discovery answers 503 once, then succeeds by
// proxying to the fake IdP. Start must fail on the first call and succeed on
// the second, without constructing a new Client.
func TestDiscoveryIsRetriedAfterFailureOnTheSameClient(t *testing.T) {
	p := oidctest.Serve(t)

	var failed atomic.Bool
	var proxy *httptest.Server
	proxy = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" && failed.CompareAndSwap(false, true) {
			http.Error(w, "discovery unavailable", http.StatusServiceUnavailable)
			return
		}
		// Proxy everything else (and subsequent discovery attempts) to the
		// fake IdP, rewriting the issuer so returned URLs point back here.
		req, err := http.NewRequest(r.Method, p.Issuer+r.URL.RequestURI(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header = r.Header
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if r.URL.Path == "/.well-known/openid-configuration" {
			body = bytes.ReplaceAll(body, []byte(p.Issuer), []byte(proxy.URL))
		}
		for k, vs := range resp.Header {
			if k == "Content-Length" {
				continue
			}
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
	}))
	t.Cleanup(proxy.Close)

	c := sso.New(sso.Config{Issuer: proxy.URL, ClientID: p.ClientID, ClientSecret: p.ClientSecret,
		RedirectURL: "http://127.0.0.1:1/console/auth/callback"})

	if _, _, err := c.Start(context.Background()); !errors.Is(err, sso.ErrUnavailable) {
		t.Fatalf("first Start = %v, want ErrUnavailable", err)
	}
	if _, _, err := c.Start(context.Background()); err != nil {
		t.Fatalf("second Start (same client) = %v, want success", err)
	}
}

func TestAttemptRoundTrip(t *testing.T) {
	a := sso.Attempt{State: "s", Nonce: "n", Verifier: "v"}
	got, ok := sso.DecodeAttempt(a.Encode())
	if !ok || got != a {
		t.Fatalf("round trip = %+v, %v", got, ok)
	}
	for _, bad := range []string{"", "a.b", "a..c", "a.b.c.d"} {
		if _, ok := sso.DecodeAttempt(bad); ok {
			t.Errorf("DecodeAttempt(%q) ok", bad)
		}
	}
}
