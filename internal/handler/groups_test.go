package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// putGroups posts a snapshot with the given bearer; empty sends none.
func putGroups(t *testing.T, srv *httptest.Server, body, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, srv.URL+"/v1/groups", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func getGroups(t *testing.T, srv *httptest.Server, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/groups", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

type groupSummary struct {
	Source    string              `json:"source"`
	AppliedBy string              `json:"appliedBy"`
	SyncedAt  string              `json:"syncedAt"`
	Users     int                 `json:"users"`
	Groups    int                 `json:"groups"`
	Members   map[string][]string `json:"members"`
}

func decodeSummary(t *testing.T, resp *http.Response) groupSummary {
	t.Helper()
	var s groupSummary
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestGroupsRoutesRequireTheAdminToken(t *testing.T) {
	srv := newServer(t)
	if resp := putGroups(t, srv, `{"members":{}}`, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("PUT without a token: %d, want 401", resp.StatusCode)
	}
	if resp := putGroups(t, srv, `{"members":{}}`, "wrong"); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("PUT with a wrong token: %d, want 401", resp.StatusCode)
	}
	if resp := getGroups(t, srv, ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET without a token: %d, want 401", resp.StatusCode)
	}
}

func TestPutGroupsStoresTheSnapshotAndSummarises(t *testing.T) {
	srv := newServer(t)

	resp := putGroups(t, srv, `{"source":"okta-export","members":{"alice@acme.com":["platform","oncall"],"bob@acme.com":["oncall"]}}`, adminToken)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	s := decodeSummary(t, resp)
	if s.Source != "okta-export" || s.Users != 2 || s.Groups != 2 || s.SyncedAt == "" {
		t.Errorf("summary = %+v; want 2 users and 2 distinct groups", s)
	}

	got := getGroups(t, srv, adminToken)
	if got.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d", got.StatusCode)
	}
	if full := decodeSummary(t, got); len(full.Members["alice@acme.com"]) != 2 {
		t.Errorf("GET members = %v", full.Members)
	}
}

func TestGetGroupsIs404BeforeAnySnapshot(t *testing.T) {
	srv := newServer(t)
	if resp := getGroups(t, srv, adminToken); resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestPutGroupsRejectsMalformedSnapshots(t *testing.T) {
	srv := newServer(t)
	for name, body := range map[string]string{
		"no members":         `{"source":"x"}`,
		"members not object": `{"members":[]}`,
		"empty user":         `{"members":{"":["platform"]}}`,
		"value not list":     `{"members":{"alice@acme.com":"platform"}}`,
		"empty group":        `{"members":{"alice@acme.com":["platform",""]}}`,
		"unknown field":      `{"members":{},"extra":1}`,
	} {
		resp := putGroups(t, srv, body, adminToken)
		if resp.StatusCode != http.StatusUnprocessableEntity && resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 422 or 400", name, resp.StatusCode)
		}
	}
	// The previous snapshot survives a bad one.
	putGroups(t, srv, `{"members":{"alice@acme.com":["platform"]}}`, adminToken)
	putGroups(t, srv, `{"members":{"":["x"]}}`, adminToken)
	if s := decodeSummary(t, getGroups(t, srv, adminToken)); s.Users != 1 {
		t.Errorf("a rejected snapshot replaced the good one: %+v", s)
	}
}

func TestPutGroupsRejectsAnOversizedBody(t *testing.T) {
	srv := newServer(t)
	huge := `{"members":{"alice@acme.com":["` + strings.Repeat("g", 9<<20) + `"]}}`
	if resp := putGroups(t, srv, huge, adminToken); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.StatusCode)
	}
}

func TestAnEmptyMembersObjectIsAValidSnapshot(t *testing.T) {
	srv := newServer(t)
	if resp := putGroups(t, srv, `{"members":{}}`, adminToken); resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200: the IdP may say nobody is in anything", resp.StatusCode)
	}
}
