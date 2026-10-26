package handler_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alchristleo/agentic-warden/internal/handler"
	"github.com/alchristleo/agentic-warden/internal/store"
)

func TestLimitHookSitsInsideLoggingAndRecovery(t *testing.T) {
	var buf bytes.Buffer
	h := handler.New(store.NewMemory(), slog.New(slog.NewTextHandler(&buf, nil)))
	h.Limit = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/boom" {
				panic("limiter bug")
			}
			http.Error(w, "limited", http.StatusTooManyRequests)
		})
	}
	srv := httptest.NewServer(h.Routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests || !strings.Contains(buf.String(), "status=429") {
		t.Errorf("status %d, log %q, want 429 logged", resp.StatusCode, buf.String())
	}

	resp, err = http.Get(srv.URL + "/boom")
	if err != nil {
		t.Fatalf("a limiter panic dropped the connection: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("panic status %d, want 500", resp.StatusCode)
	}
}
