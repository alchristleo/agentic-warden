package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/store"
)

// stringsReader is strings.NewReader, except an empty body becomes a nil
// io.Reader: http.NewRequest treats that as no body at all, which is what a
// DELETE with no payload needs.
func stringsReader(s string) io.Reader {
	if s == "" {
		return nil
	}
	return strings.NewReader(s)
}

// itoa formats an event id for a query string.
func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}

// bodyIs reports whether resp's body, trimmed, equals want.
func bodyIs(t *testing.T, resp *http.Response, want string) bool {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(body)) == want
}

// enrollMachine mints a token for user as an administrator, enrolls a
// machine named name with it, and returns the new machine's id.
func enrollMachine(t *testing.T, srv *httptest.Server, user, name string) string {
	t.Helper()
	token := mintToken(t, srv, user)
	resp := postAs(t, srv, "/v1/machines/enroll", `{"token":"`+token+`","name":"`+name+`","os":"linux"}`, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("enrolling: status = %d", resp.StatusCode)
	}
	var body struct {
		MachineID string `json:"machineId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.MachineID
}

// failingAudit wraps a store and fails PutRuleSet before it reaches the
// wrapped store, so a test can prove that an audit failure fails the write
// it would have accompanied.
type failingAudit struct {
	store.Store
}

func (failingAudit) PutRuleSet(context.Context, model.Revision, model.AuditEvent) error {
	return errors.New("disk full")
}

func auditEvents(t *testing.T, resp *http.Response) []model.AuditEvent {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var events []model.AuditEvent
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatal(err)
	}
	return events
}

func TestEachAdminWriteIsAuditedWithTheTokenActor(t *testing.T) {
	srv := newServer(t)
	req := func(method, path, body string) {
		t.Helper()
		r, _ := http.NewRequest(method, srv.URL+path, stringsReader(body))
		r.Header.Set("Authorization", "Bearer "+adminToken)
		r.Header.Set("X-Applied-By", "alice")
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			t.Fatalf("%s %s = %d", method, path, resp.StatusCode)
		}
	}
	req("POST", "/v1/policy/revisions", `{"version":"v1"}`)
	req("POST", "/v1/enrollment-tokens", `{"user":"bob@example.com"}`)
	req("PUT", "/v1/groups", `{"source":"okta","members":{"bob@example.com":["devs"]}}`)
	id := enrollMachine(t, srv, "bob@example.com", "laptop")
	req("DELETE", "/v1/machines/"+id, "")

	events := auditEvents(t, getAs(t, srv, "/v1/audit", adminToken))
	want := []struct{ action, target string }{
		{model.AuditMachineRevoke, id},
		{model.AuditTokenCreate, "bob@example.com"}, // enrollMachine's own token
		{model.AuditGroupsApply, ""},
		{model.AuditTokenCreate, "bob@example.com"},
		{model.AuditRevisionCreate, "v1"},
	}
	if len(events) != len(want) {
		t.Fatalf("got %d events: %+v", len(events), events)
	}
	for i, w := range want {
		if events[i].Action != w.action || events[i].Target != w.target {
			t.Errorf("event %d = %s %q, want %s %q", i, events[i].Action, events[i].Target, w.action, w.target)
		}
	}
	if events[0].Detail["name"] != "laptop" || events[0].Detail["user"] != "bob@example.com" {
		t.Errorf("revoke detail = %#v", events[0].Detail)
	}
	for _, e := range events[1:] {
		if e.Actor != "token:alice" && e.Actor != "token:" {
			t.Errorf("actor = %q", e.Actor)
		}
	}
	if events[4].Actor != "token:alice" {
		t.Errorf("revision actor = %q, want token:alice", events[4].Actor)
	}
	for _, e := range events {
		if _, leaked := e.Detail["token"]; leaked {
			t.Fatalf("token recorded in audit: %+v", e)
		}
	}
}

func TestAuditPagingAndLimits(t *testing.T) {
	srv := newServer(t)
	for _, v := range []string{"v1", "v2", "v3"} {
		post(t, srv, "/v1/policy/revisions", `{"version":"`+v+`"}`)
	}
	all := auditEvents(t, getAs(t, srv, "/v1/audit?limit=2", adminToken))
	if len(all) != 2 || all[0].Target != "v3" {
		t.Fatalf("limit=2 → %+v", all)
	}
	next := auditEvents(t, getAs(t, srv, "/v1/audit?before="+itoa(all[1].ID), adminToken))
	if len(next) != 1 || next[0].Target != "v1" {
		t.Fatalf("before → %+v", next)
	}
	for _, bad := range []string{"limit=0", "limit=201", "limit=x", "before=-1", "before=x"} {
		if resp := getAs(t, srv, "/v1/audit?"+bad, adminToken); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s → %d, want 400", bad, resp.StatusCode)
		}
	}
	if resp := getAs(t, srv, "/v1/audit", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token → %d", resp.StatusCode)
	}
}

// An audit write that fails fails the change with it: a 500, and nothing
// stored.
func TestAuditFailureFailsTheWrite(t *testing.T) {
	srv := newServerWithStore(t, failingAudit{Store: store.NewMemory()})
	resp := post(t, srv, "/v1/policy/revisions", `{"version":"v1"}`)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", resp.StatusCode)
	}
	if got := getAs(t, srv, "/v1/policy/revisions", adminToken); !bodyIs(t, got, "[]") {
		t.Fatal("revision stored despite audit failure")
	}
}
