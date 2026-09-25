// Package oidctest is a fake OpenID Connect provider for tests: discovery,
// JWKS, an authorize endpoint and a token endpoint with PKCE, signing RS256
// ID tokens. Tamper makes the next tokens wrong in one specific way.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// Tamper selects a defect in issued ID tokens.
type Tamper int

const (
	None Tamper = iota
	WrongNonce
	WrongAudience
	Expired
	BadSignature
)

type grant struct {
	user, nonce, challenge, redirect string
}

// Provider is the fake IdP. Set User to auto-approve /authorize as that
// user; leave it empty to get a sign-in form (the Playwright suite fills
// it). Claims are merged into every ID token; a nil value removes a claim.
type Provider struct {
	Issuer, ClientID, ClientSecret string

	mu     sync.Mutex
	User   string
	Claims map[string]any
	Tamper Tamper
	// FailToken makes the token endpoint answer 500.
	FailToken bool

	key, wrongKey *rsa.PrivateKey
	codes         map[string]grant
}

// New builds a provider for the given issuer URL.
func New(issuer, clientID, clientSecret string) (*Provider, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	wrong, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &Provider{Issuer: issuer, ClientID: clientID, ClientSecret: clientSecret,
		key: key, wrongKey: wrong, codes: map[string]grant{}}, nil
}

// Serve starts a provider on httptest for one test.
func Serve(t testing.TB) *Provider {
	t.Helper()
	p, err := New("", "console", "secret")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)
	p.Issuer = srv.URL
	return p
}

// Set changes provider behavior under its lock.
func (p *Provider) Set(fn func(p *Provider)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fn(p)
}

// Handler serves the provider's endpoints.
func (p *Provider) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /jwks", p.jwks)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /authorize", p.authorize)
	mux.HandleFunc("POST /token", p.token)
	return mux
}

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                p.Issuer,
		"authorization_endpoint":                p.Issuer + "/authorize",
		"token_endpoint":                        p.Issuer + "/token",
		"jwks_uri":                              p.Issuer + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

func (p *Provider) jwks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: &p.key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig",
	}}})
}

var form = template.Must(template.New("f").Parse(`<!doctype html><title>Fake IdP</title>
<form method="post"><label>Email <input name="user" type="email" required></label>
{{range $k, $v := .}}<input type="hidden" name="{{$k}}" value="{{index $v 0}}">{{end}}
<button>Sign in</button></form>`))

func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	q := r.Form
	p.mu.Lock()
	user := p.User
	p.mu.Unlock()
	if r.Method == http.MethodPost {
		user = q.Get("user")
	}
	if user == "" {
		params := url.Values{}
		for _, k := range []string{"client_id", "redirect_uri", "state", "nonce", "code_challenge", "code_challenge_method"} {
			params.Set(k, q.Get(k))
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = form.Execute(w, params)
		return
	}
	if q.Get("client_id") != p.ClientID || q.Get("code_challenge_method") != "S256" {
		http.Error(w, "bad authorize request", http.StatusBadRequest)
		return
	}
	code := randomString()
	p.mu.Lock()
	p.codes[code] = grant{user: user, nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: q.Get("redirect_uri")}
	p.mu.Unlock()
	back, _ := url.Parse(q.Get("redirect_uri"))
	v := back.Query()
	v.Set("code", code)
	v.Set("state", q.Get("state"))
	back.RawQuery = v.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.Form.Get("client_id"), r.Form.Get("client_secret")
	}
	p.mu.Lock()
	g, found := p.codes[r.Form.Get("code")]
	delete(p.codes, r.Form.Get("code"))
	fail, tamper, extra := p.FailToken, p.Tamper, p.Claims
	p.mu.Unlock()
	if fail {
		http.Error(w, "token endpoint down", http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
	if id != p.ClientID || secret != p.ClientSecret || !found ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge ||
		r.Form.Get("redirect_uri") != g.redirect {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid_grant"})
		return
	}
	now := time.Now()
	claims := map[string]any{
		"iss": p.Issuer, "sub": g.user, "aud": p.ClientID, "iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(), "nonce": g.nonce,
		"email": g.user, "email_verified": true,
	}
	for k, v := range extra {
		if v == nil {
			delete(claims, k)
		} else {
			claims[k] = v
		}
	}
	signWith := p.key
	switch tamper {
	case WrongNonce:
		claims["nonce"] = "not-the-nonce"
	case WrongAudience:
		claims["aud"] = "someone-else"
	case Expired:
		claims["exp"] = now.Add(-time.Hour).Unix()
	case BadSignature:
		signWith = p.wrongKey
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: signWith},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"access_token": randomString(), "token_type": "Bearer", "expires_in": 300, "id_token": raw})
}

// Login drives /authorize for p.User without a browser and returns the
// code and state the IdP would send back to the client.
func (p *Provider) Login(t testing.TB, authURL string) (code, state string) {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || resp.StatusCode != http.StatusFound {
		t.Fatalf("authorize: %d %v", resp.StatusCode, err)
	}
	return loc.Query().Get("code"), loc.Query().Get("state")
}

func randomString() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(fmt.Sprintf("oidctest: encoding: %v", err))
	}
}
