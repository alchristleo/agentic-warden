package sync_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/sync"
)

func TestEnrollPostsTheTokenAndReturnsTheCredential(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/machines/enroll" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"machineId":"m1","credential":"c1","user":"alice@acme.com"}`))
	}))
	defer srv.Close()

	c := &sync.Client{Server: srv.URL + "/"}
	e, err := c.Enroll(context.Background(), "tok", "host1", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if got["token"] != "tok" || got["name"] != "host1" || got["os"] != "linux" {
		t.Errorf("request body = %v", got)
	}
	if e.MachineID != "m1" || e.Credential != "c1" || e.User != "alice@acme.com" {
		t.Errorf("enrollment = %+v", e)
	}
}

func TestEnrollReportsARejectedToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"conflict"}`, http.StatusConflict)
	}))
	defer srv.Close()
	_, err := (&sync.Client{Server: srv.URL}).Enroll(context.Background(), "tok", "h", "linux")
	if err == nil || !strings.Contains(err.Error(), "409") {
		t.Errorf("err = %v, want one naming the 409", err)
	}
}

func bundleServer(t *testing.T, status int, body string) (*httptest.Server, *http.Request) {
	t.Helper()
	var seen http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = *r
		if status == http.StatusOK {
			w.Header().Set("ETag", `"e2"`)
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestFetchSendsTheCredentialAndETag(t *testing.T) {
	srv, seen := bundleServer(t, http.StatusOK, `{"version":"v2","groups":[],"rules":[]}`)
	got, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", `"e1"`)
	if err != nil {
		t.Fatal(err)
	}
	if seen.URL.Path != "/v1/bundle" || seen.Header.Get("Authorization") != "Bearer cred" || seen.Header.Get("If-None-Match") != `"e1"` {
		t.Errorf("request: %s %s %v", seen.Method, seen.URL.Path, seen.Header)
	}
	if got.Unchanged || got.ETag != `"e2"` || got.Bundle == nil || got.Bundle.Version != "v2" {
		t.Errorf("fetched = %+v", got)
	}
}

func TestFetchOn304IsUnchanged(t *testing.T) {
	srv, _ := bundleServer(t, http.StatusNotModified, "")
	got, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", `"e1"`)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Unchanged || got.Bundle != nil {
		t.Errorf("fetched = %+v, want Unchanged with no bundle", got)
	}
}

func TestFetchOn401IsErrUnauthorized(t *testing.T) {
	srv, _ := bundleServer(t, http.StatusUnauthorized, `{"error":"unauthorized"}`)
	_, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "")
	if !errors.Is(err, model.ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

func TestFetchOnServerErrorFails(t *testing.T) {
	srv, _ := bundleServer(t, http.StatusInternalServerError, "boom")
	_, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "")
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v, want one naming the status", err)
	}
}

func TestFetchRejectsAMalformedBundle(t *testing.T) {
	srv, _ := bundleServer(t, http.StatusOK, `{"version":`)
	_, err := (&sync.Client{Server: srv.URL}).Fetch(context.Background(), "cred", "")
	if err == nil {
		t.Error("want an error for a malformed bundle")
	}
}

func TestFetchWhenTheServerIsDownFails(t *testing.T) {
	srv, _ := bundleServer(t, http.StatusOK, "{}")
	url := srv.URL
	srv.Close()
	_, err := (&sync.Client{Server: url}).Fetch(context.Background(), "cred", "")
	if err == nil {
		t.Error("want an error when the control plane is unreachable")
	}
}
