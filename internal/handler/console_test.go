package handler_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/acme/agent-wrapper/internal/console/authz"
	"github.com/acme/agent-wrapper/internal/console/oidctest"
	"github.com/acme/agent-wrapper/internal/console/session"
	"github.com/acme/agent-wrapper/internal/console/sso"
	"github.com/acme/agent-wrapper/internal/handler"
	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/store"
)

type consoleEnv struct {
	srv      *httptest.Server
	idp      *oidctest.Provider
	store    store.Store
	sessions *session.Manager
	now      *time.Time
	browser  *http.Client // cookie jar, no automatic redirects
}

// newConsole starts awd with the console on, against a fake IdP. members
// seeds the group snapshot; nil leaves awd with no group data.
func newConsole(t *testing.T, members map[string][]string) *consoleEnv {
	t.Helper()
	return newConsoleWithStore(t, store.NewMemory(), members)
}

func newConsoleWithStore(t *testing.T, s store.Store, members map[string][]string) *consoleEnv {
	t.Helper()
	idp := oidctest.Serve(t)
	now := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	env := &consoleEnv{idp: idp, store: s, now: &now}
	if members != nil {
		if err := s.PutGroupSnapshot(context.Background(), model.GroupSnapshot{Source: "t", SyncedAt: now, Members: members},
			model.AuditEvent{Actor: "token:seed", Action: model.AuditGroupsApply}); err != nil {
			t.Fatal(err)
		}
	}
	h := handler.New(s, nil)
	h.AdminToken = adminToken
	h.Now = func() time.Time { return *env.now }
	env.sessions = session.NewManager(s)
	env.sessions.Now = h.Now
	srv := httptest.NewUnstartedServer(nil)
	public, _ := url.Parse("http://" + srv.Listener.Addr().String())
	h.Console = &handler.Console{
		PublicURL: public,
		SSO: sso.New(sso.Config{Issuer: idp.Issuer, ClientID: idp.ClientID, ClientSecret: idp.ClientSecret,
			RedirectURL: public.String() + "/console/auth/callback"}),
		Sessions: env.sessions,
		Authz:    &authz.Checker{Store: s, Group: "console-admins"},
		Assets: fstest.MapFS{
			"index.html":        {Data: []byte("<!doctype html><div id=root></div>")},
			"assets/app-abc.js": {Data: []byte("console.log(1)")},
		},
	}
	srv.Config.Handler = h.Routes()
	srv.Start()
	t.Cleanup(srv.Close)
	env.srv = srv
	jar, _ := cookiejar.New(nil)
	env.browser = &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return env
}

// login runs the whole browser flow for user and returns the callback response.
func (e *consoleEnv) login(t *testing.T, user string) *http.Response {
	t.Helper()
	e.idp.Set(func(p *oidctest.Provider) { p.User = user })
	resp := e.do(t, "GET", "/console/auth/login", nil)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("login = %d", resp.StatusCode)
	}
	code, state := e.idp.Login(t, resp.Header.Get("Location"))
	return e.do(t, "GET", "/console/auth/callback?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state), nil)
}

// do sends a request through the browser; header adds or overrides headers.
func (e *consoleEnv) do(t *testing.T, method, path string, header http.Header) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := e.browser.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (e *consoleEnv) origin() http.Header { return http.Header{"Origin": {e.srv.URL}} }

func (e *consoleEnv) auditActions(t *testing.T) []model.AuditEvent {
	t.Helper()
	events, err := e.store.AuditEvents(context.Background(), 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

var admins = map[string][]string{"alice@example.com": {"console-admins"}, "bob@example.com": {"devs"}}

// failingStore wraps a store.Store and lets a test fail one method without
// implementing the whole interface. A nil field falls through to the
// wrapped store, so only the method under test needs to be set.
type failingStore struct {
	store.Store
	sessionByHash        func(ctx context.Context, hash string) (model.ConsoleSession, error)
	recordAudit          func(ctx context.Context, e model.AuditEvent) error
	currentGroupSnapshot func(ctx context.Context) (model.GroupSnapshot, error)
	scimCounts           func(ctx context.Context) (model.SCIMCounts, error)
}

func (f failingStore) SessionByHash(ctx context.Context, hash string) (model.ConsoleSession, error) {
	if f.sessionByHash != nil {
		return f.sessionByHash(ctx, hash)
	}
	return f.Store.SessionByHash(ctx, hash)
}

func (f failingStore) RecordAudit(ctx context.Context, e model.AuditEvent) error {
	if f.recordAudit != nil {
		return f.recordAudit(ctx, e)
	}
	return f.Store.RecordAudit(ctx, e)
}

func (f failingStore) CurrentGroupSnapshot(ctx context.Context) (model.GroupSnapshot, error) {
	if f.currentGroupSnapshot != nil {
		return f.currentGroupSnapshot(ctx)
	}
	return f.Store.CurrentGroupSnapshot(ctx)
}

func (f failingStore) SCIMCounts(ctx context.Context) (model.SCIMCounts, error) {
	if f.scimCounts != nil {
		return f.scimCounts(ctx)
	}
	return f.Store.SCIMCounts(ctx)
}

func TestConsoleOffAnswers404(t *testing.T) {
	srv := newServer(t) // no Console
	for _, p := range []string{"/console/", "/console/auth/login", "/console/api/me"} {
		if resp := get(t, srv, p, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", p, resp.StatusCode)
		}
	}
	if resp := getAs(t, srv, "/v1/machines", adminToken); resp.StatusCode != http.StatusOK {
		t.Errorf("token path broken: %d", resp.StatusCode)
	}
}

func TestConsoleLoginHappyPath(t *testing.T) {
	e := newConsole(t, admins)
	resp := e.login(t, "alice@example.com")
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/console/" {
		t.Fatalf("callback = %d → %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "aw_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode ||
		sessionCookie.Path != "/" || sessionCookie.MaxAge != 0 || sessionCookie.Secure {
		t.Fatalf("session cookie = %+v (loopback http: not Secure)", sessionCookie)
	}
	me := e.do(t, "GET", "/console/api/me", nil)
	body, _ := io.ReadAll(me.Body)
	if me.StatusCode != http.StatusOK || !strings.Contains(string(body), `"user":"alice@example.com"`) {
		t.Fatalf("me = %d %s", me.StatusCode, body)
	}
	if ev := e.auditActions(t); ev[0].Action != model.AuditLogin || ev[0].Actor != "alice@example.com" {
		t.Fatalf("audit = %+v", ev[0])
	}
}

func TestConsoleSessionCanUseAdminAPIAndIsTheActor(t *testing.T) {
	e := newConsole(t, admins)
	e.login(t, "alice@example.com")
	req, _ := http.NewRequest("POST", e.srv.URL+"/v1/policy/revisions", strings.NewReader(`{"version":"v1"}`))
	req.Header.Set("Origin", e.srv.URL)
	resp, err := e.browser.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST as console = %v %v", resp.StatusCode, err)
	}
	if ev := e.auditActions(t); ev[0].Action != model.AuditRevisionCreate || ev[0].Actor != "alice@example.com" {
		t.Fatalf("audit = %+v", ev[0])
	}
}

// TestConsoleIdPDownAtLoginIs503ThenRecovers proves retry-after-failure on
// the same *sso.Client wired into a running console: discovery answers 503
// once, via a proxy in front of the fake IdP, then succeeds. The same
// technique as sso_test.go's TestDiscoveryIsRetriedAfterFailureOnTheSameClient.
func TestConsoleIdPDownAtLoginIs503ThenRecovers(t *testing.T) {
	idp := oidctest.Serve(t)
	idp.Set(func(p *oidctest.Provider) { p.User = "alice@example.com" })

	var failed atomic.Bool
	var proxy *httptest.Server
	proxy = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/openid-configuration" && failed.CompareAndSwap(false, true) {
			http.Error(w, "discovery unavailable", http.StatusServiceUnavailable)
			return
		}
		// Proxy everything else (and subsequent discovery attempts) to the
		// fake IdP, rewriting the issuer so returned URLs point back here.
		req, err := http.NewRequest(r.Method, idp.Issuer+r.URL.RequestURI(), r.Body)
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
			body = bytes.ReplaceAll(body, []byte(idp.Issuer), []byte(proxy.URL))
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

	s := store.NewMemory()
	now := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	if err := s.PutGroupSnapshot(context.Background(), model.GroupSnapshot{Source: "t", SyncedAt: now, Members: admins},
		model.AuditEvent{Actor: "token:seed", Action: model.AuditGroupsApply}); err != nil {
		t.Fatal(err)
	}
	h := handler.New(s, nil)
	h.AdminToken = adminToken
	h.Now = func() time.Time { return now }
	sessions := session.NewManager(s)
	sessions.Now = h.Now
	srv := httptest.NewUnstartedServer(nil)
	public, _ := url.Parse("http://" + srv.Listener.Addr().String())
	h.Console = &handler.Console{
		PublicURL: public,
		SSO: sso.New(sso.Config{Issuer: proxy.URL, ClientID: idp.ClientID, ClientSecret: idp.ClientSecret,
			RedirectURL: public.String() + "/console/auth/callback"}),
		Sessions: sessions,
		Authz:    &authz.Checker{Store: s, Group: "console-admins"},
		Assets:   fstest.MapFS{"index.html": {Data: []byte("<!doctype html><div id=root></div>")}},
	}
	srv.Config.Handler = h.Routes()
	srv.Start()
	t.Cleanup(srv.Close)

	first, err := http.Get(srv.URL + "/console/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	first.Body.Close()
	if first.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("first login = %d, want 503", first.StatusCode)
	}

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	second, err := client.Get(srv.URL + "/console/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	second.Body.Close()
	if second.StatusCode != http.StatusFound {
		t.Fatalf("second login = %d, want 302", second.StatusCode)
	}
}

func TestConsoleCallbackStateMismatchIs400(t *testing.T) {
	e := newConsole(t, admins)
	e.idp.Set(func(p *oidctest.Provider) { p.User = "alice@example.com" })
	resp := e.do(t, "GET", "/console/auth/login", nil)
	code, _ := e.idp.Login(t, resp.Header.Get("Location"))
	cb := e.do(t, "GET", "/console/auth/callback?code="+code+"&state=forged", nil)
	if cb.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", cb.StatusCode)
	}
	if me := e.do(t, "GET", "/console/api/me", nil); me.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session created anyway: %d", me.StatusCode)
	}
}

func TestConsoleCallbackWithoutLoginCookieIs400(t *testing.T) {
	e := newConsole(t, admins)
	if cb := e.do(t, "GET", "/console/auth/callback?code=x&state=y", nil); cb.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", cb.StatusCode)
	}
}

// Review Focus 2.
func TestConsoleCallbackReplayWithSessionRedirects(t *testing.T) {
	e := newConsole(t, admins)
	e.login(t, "alice@example.com")
	cb := e.do(t, "GET", "/console/auth/callback?code=used&state=stale", nil)
	if cb.StatusCode != http.StatusFound || cb.Header.Get("Location") != "/console/" {
		t.Fatalf("replay = %d → %q", cb.StatusCode, cb.Header.Get("Location"))
	}
}

func TestConsoleCodeExchangeFailureIs502(t *testing.T) {
	e := newConsole(t, admins)
	e.idp.Set(func(p *oidctest.Provider) { p.FailToken = true })
	if cb := e.login(t, "alice@example.com"); cb.StatusCode != http.StatusBadGateway {
		t.Fatalf("status %d", cb.StatusCode)
	}
}

// Final review finding 4: the IdP's `error` query parameter is attacker
// controlled and must not land unbounded or with control/quote characters
// in the audit log.
func TestConsoleCallbackIdPErrorIsSanitizedInAudit(t *testing.T) {
	e := newConsole(t, admins)
	login := e.do(t, "GET", "/console/auth/login", nil)
	loc, err := url.Parse(login.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := loc.Query().Get("state")

	raw := "access_denied\"\\" + strings.Repeat("x", 100) + "\x00\x07"
	cb := e.do(t, "GET", "/console/auth/callback?state="+url.QueryEscape(state)+"&error="+url.QueryEscape(raw), nil)
	if cb.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", cb.StatusCode)
	}
	ev := e.auditActions(t)
	reason, _ := ev[0].Detail["reason"].(string)
	if !strings.HasPrefix(reason, "idp: access_denied") {
		t.Fatalf("reason = %q, want the access_denied prefix kept", reason)
	}
	body := strings.TrimPrefix(reason, "idp: ")
	if len(body) > 64 {
		t.Fatalf("reason not truncated to 64 bytes: %d bytes (%q)", len(body), body)
	}
	if strings.ContainsAny(body, "\"\\\x00\x07") {
		t.Fatalf("reason kept a disallowed byte: %q", body)
	}
}

func TestConsoleInvalidTokenIs401AndAudited(t *testing.T) {
	for _, tamper := range []oidctest.Tamper{oidctest.WrongNonce, oidctest.WrongAudience, oidctest.Expired, oidctest.BadSignature} {
		e := newConsole(t, admins)
		e.idp.Set(func(p *oidctest.Provider) { p.Tamper = tamper })
		if cb := e.login(t, "alice@example.com"); cb.StatusCode != http.StatusUnauthorized {
			t.Fatalf("tamper %d: status %d", tamper, cb.StatusCode)
		}
		if ev := e.auditActions(t); ev[0].Action != model.AuditLoginDenied || ev[0].Actor != "anonymous" {
			t.Fatalf("tamper %d: audit %+v", tamper, ev[0])
		}
	}
}

func TestConsoleUnverifiedEmailIs403(t *testing.T) {
	e := newConsole(t, admins)
	e.idp.Set(func(p *oidctest.Provider) { p.Claims = map[string]any{"email_verified": false} })
	if cb := e.login(t, "alice@example.com"); cb.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", cb.StatusCode)
	}
	if ev := e.auditActions(t); ev[0].Action != model.AuditLoginDenied || ev[0].Actor != "anonymous" {
		t.Fatalf("audit = %+v", ev[0])
	}
}

func TestConsoleMissingUserClaimIs403(t *testing.T) {
	e := newConsole(t, admins)
	e.idp.Set(func(p *oidctest.Provider) { p.Claims = map[string]any{"email": nil} })
	if cb := e.login(t, "alice@example.com"); cb.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", cb.StatusCode)
	}
	if ev := e.auditActions(t); ev[0].Action != model.AuditLoginDenied || ev[0].Actor != "anonymous" {
		t.Fatalf("audit = %+v", ev[0])
	}
}

func TestConsoleNoGroupDataIs503(t *testing.T) {
	e := newConsole(t, nil)
	cb := e.login(t, "alice@example.com")
	body, _ := io.ReadAll(cb.Body)
	if cb.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), "console needs group data") {
		t.Fatalf("login = %d %s", cb.StatusCode, body)
	}
}

// TestConsoleNoGroupDataMidSessionIs503AndKeepsSession covers the
// per-request half of the "no group data" failure-mode row: a user who
// logged in while group data existed keeps hitting 503 on every request
// once it disappears (no snapshot and zero SCIM groups), and — unlike a
// user who is simply no longer in the admin group — the session itself
// survives: it comes back to life once group data is restored.
func TestConsoleNoGroupDataMidSessionIs503AndKeepsSession(t *testing.T) {
	mem := store.NewMemory()
	var noGroupData atomic.Bool
	fs := failingStore{
		Store: mem,
		currentGroupSnapshot: func(ctx context.Context) (model.GroupSnapshot, error) {
			if noGroupData.Load() {
				return model.GroupSnapshot{}, model.ErrNotFound
			}
			return mem.CurrentGroupSnapshot(ctx)
		},
		scimCounts: func(ctx context.Context) (model.SCIMCounts, error) {
			if noGroupData.Load() {
				return model.SCIMCounts{}, nil
			}
			return mem.SCIMCounts(ctx)
		},
	}
	e := newConsoleWithStore(t, fs, admins)
	e.login(t, "alice@example.com")

	noGroupData.Store(true)

	me := e.do(t, "GET", "/console/api/me", nil)
	body, _ := io.ReadAll(me.Body)
	if me.StatusCode != http.StatusServiceUnavailable || !strings.Contains(string(body), "console needs group data") {
		t.Fatalf("me = %d %s", me.StatusCode, body)
	}
	if resp := e.do(t, "GET", "/v1/machines", nil); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("machines = %d, want 503", resp.StatusCode)
	}

	noGroupData.Store(false)
	if me := e.do(t, "GET", "/console/api/me", nil); me.StatusCode != http.StatusOK {
		t.Fatalf("session was deleted while group data was missing: %d", me.StatusCode)
	}
}

func TestConsoleNonAdminIs403AtLogin(t *testing.T) {
	e := newConsole(t, admins)
	cb := e.login(t, "bob@example.com")
	if cb.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", cb.StatusCode)
	}
	if ev := e.auditActions(t); ev[0].Action != model.AuditLoginDenied || ev[0].Actor != "bob@example.com" ||
		ev[0].Detail["reason"] != "not in console-admins" {
		t.Fatalf("audit = %+v", ev[0])
	}
}

// Review Focus 1.
func TestConsoleLoginCaseMismatchIsDenied(t *testing.T) {
	e := newConsole(t, admins)
	if cb := e.login(t, "Alice@example.com"); cb.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", cb.StatusCode)
	}
	if ev := e.auditActions(t); ev[0].Actor != "Alice@example.com" {
		t.Fatalf("audit actor = %q", ev[0].Actor)
	}
}

func TestConsoleAdminRemovedMidSessionIs403AndSessionsDeleted(t *testing.T) {
	e := newConsole(t, admins)
	e.login(t, "alice@example.com")
	_ = e.store.PutGroupSnapshot(context.Background(), model.GroupSnapshot{Source: "t", SyncedAt: *e.now,
		Members: map[string][]string{"bob@example.com": {"console-admins"}}}, model.AuditEvent{Actor: "token:x", Action: model.AuditGroupsApply})
	if me := e.do(t, "GET", "/v1/machines", nil); me.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", me.StatusCode)
	}
	// Restoring membership does not revive the session: it was deleted.
	_ = e.store.PutGroupSnapshot(context.Background(), model.GroupSnapshot{Source: "t", SyncedAt: *e.now,
		Members: admins}, model.AuditEvent{Actor: "token:x", Action: model.AuditGroupsApply})
	if me := e.do(t, "GET", "/console/api/me", nil); me.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session survived: %d", me.StatusCode)
	}
}

func TestConsoleExpiredSessionIs401(t *testing.T) {
	e := newConsole(t, admins)
	e.login(t, "alice@example.com")
	*e.now = e.now.Add(time.Hour + time.Second)
	if me := e.do(t, "GET", "/console/api/me", nil); me.StatusCode != http.StatusUnauthorized {
		t.Fatalf("idle: %d", me.StatusCode)
	}
}

func TestConsoleWriteNeedsOrigin(t *testing.T) {
	e := newConsole(t, admins)
	e.login(t, "alice@example.com")
	for _, origin := range []string{"", "https://evil.example"} {
		h := http.Header{}
		if origin != "" {
			h.Set("Origin", origin)
		}
		if resp := e.do(t, "POST", "/v1/enrollment-tokens", h); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("origin %q: %d", origin, resp.StatusCode)
		}
	}
	if resp := e.do(t, "POST", "/console/auth/logout", nil); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("logout without origin: %d", resp.StatusCode)
	}
	for _, ev := range e.auditActions(t) {
		if ev.Action == model.AuditTokenCreate {
			t.Fatal("token created despite bad origin")
		}
	}
}

// Final review finding 2: sameOrigin must run before consoleUser, so a
// refused non-GET request has no side effect — even for a user who has
// since been removed from the admin group. Before the fix, consoleUser ran
// first and would touch last_seen_at or delete the removed admin's
// sessions before the Origin check ever got a chance to refuse the request.
func TestConsoleForeignOriginHasNoSideEffectOnARemovedAdmin(t *testing.T) {
	e := newConsole(t, admins)
	e.login(t, "alice@example.com")

	u, err := url.Parse(e.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	var token string
	for _, c := range e.browser.Jar.Cookies(u) {
		if c.Name == "aw_session" {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatal("no session cookie after login")
	}
	hash := session.Hash(token)

	// alice is removed from the admin group; her session row is untouched
	// by that alone.
	if err := e.store.PutGroupSnapshot(context.Background(), model.GroupSnapshot{Source: "t", SyncedAt: *e.now,
		Members: map[string][]string{"bob@example.com": {"console-admins"}}},
		model.AuditEvent{Actor: "token:x", Action: model.AuditGroupsApply}); err != nil {
		t.Fatal(err)
	}

	before, err := e.store.SessionByHash(context.Background(), hash)
	if err != nil {
		t.Fatal(err)
	}
	*e.now = e.now.Add(2 * time.Minute) // past the once-a-minute touch throttle

	resp := e.do(t, "POST", "/v1/enrollment-tokens", http.Header{"Origin": {"https://evil.example"}})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", resp.StatusCode)
	}

	after, err := e.store.SessionByHash(context.Background(), hash)
	if err != nil {
		t.Fatalf("session deleted despite refused origin: %v", err)
	}
	if !after.LastSeenAt.Equal(before.LastSeenAt) {
		t.Fatalf("last_seen_at touched despite refused origin: %v -> %v", before.LastSeenAt, after.LastSeenAt)
	}
}

// Final review finding 5: the CLI's own confusion (a bearer-less request to
// a console-enabled awd) should name both ways in.
func TestConsoleNoCookieMessageNamesBothPaths(t *testing.T) {
	e := newConsole(t, admins)
	resp := e.do(t, "GET", "/v1/machines", nil)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(string(body), "sign in to the console or present the admin token") {
		t.Fatalf("status %d body %s", resp.StatusCode, body)
	}
}

func TestConsoleWrongBearerWithValidCookieIs401(t *testing.T) {
	e := newConsole(t, admins)
	e.login(t, "alice@example.com")
	if resp := e.do(t, "GET", "/v1/machines", http.Header{"Authorization": {"Bearer wrong"}}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestConsoleStoreDownIs503(t *testing.T) {
	fs := failingStore{Store: store.NewMemory(), sessionByHash: func(context.Context, string) (model.ConsoleSession, error) {
		return model.ConsoleSession{}, errors.New("db down")
	}}
	e := newConsoleWithStore(t, fs, admins)
	if me := e.do(t, "GET", "/console/api/me", http.Header{"Cookie": {"aw_session=anything"}}); me.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d", me.StatusCode)
	}
}

func TestConsoleLoginAuditFailureIs500AndNoSession(t *testing.T) {
	fs := failingStore{Store: store.NewMemory(), recordAudit: func(context.Context, model.AuditEvent) error {
		return errors.New("disk full")
	}}
	e := newConsoleWithStore(t, fs, admins)
	if cb := e.login(t, "alice@example.com"); cb.StatusCode != http.StatusInternalServerError {
		t.Fatalf("callback status %d", cb.StatusCode)
	}
	if me := e.do(t, "GET", "/console/api/me", nil); me.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session survived: %d", me.StatusCode)
	}
}

func TestConsoleLogout(t *testing.T) {
	e := newConsole(t, admins)
	e.login(t, "alice@example.com")
	if resp := e.do(t, "POST", "/console/auth/logout", e.origin()); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout %d", resp.StatusCode)
	}
	if me := e.do(t, "GET", "/console/api/me", nil); me.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after logout %d", me.StatusCode)
	}
	if ev := e.auditActions(t); ev[0].Action != model.AuditLogout {
		t.Fatalf("audit %+v", ev[0])
	}
}

func TestConsoleStaticFiles(t *testing.T) {
	e := newConsole(t, admins)
	idx := e.do(t, "GET", "/console/policy/revisions/3", nil)
	body, _ := io.ReadAll(idx.Body)
	if idx.StatusCode != 200 || !strings.Contains(string(body), "id=root") || idx.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("deep link = %d %q %q", idx.StatusCode, idx.Header.Get("Cache-Control"), body)
	}
	const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
	if idx.Header.Get("Content-Security-Policy") != csp || idx.Header.Get("X-Content-Type-Options") != "nosniff" ||
		idx.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("headers = %v", idx.Header)
	}
	asset := e.do(t, "GET", "/console/assets/app-abc.js", nil)
	if asset.StatusCode != 200 || asset.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset = %d %q", asset.StatusCode, asset.Header.Get("Cache-Control"))
	}
	if missing := e.do(t, "GET", "/console/assets/nope.js", nil); missing.StatusCode != 404 {
		t.Fatalf("missing asset = %d", missing.StatusCode)
	}
	if api := e.do(t, "GET", "/console/api/nope", nil); api.StatusCode != 404 {
		t.Fatalf("unknown api = %d", api.StatusCode)
	}
	if bare := e.do(t, "GET", "/console", nil); bare.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("/console = %d", bare.StatusCode)
	}
}
