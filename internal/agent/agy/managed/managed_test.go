package managed_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/alchristleo/agentic-warden/internal/agent/agy/managed"
)

const inlineGolden = `{
  "gateway": {
    "enabled": true,
    "url": "https://gateway.example.com",
    "wireProtocol": "genai",
    "customHeaders": {"X-Cost-Center": "platform"}
  },
  "caCertPath": "/etc/ssl/certs/example-ca.pem",
  "models": {
    "defaultModelId": "gemini-3.8-flash",
    "list": [{"modelId": "gemini-3.8-flash"}, {"modelId": "gemini-3.8-pro"}]
  }
}`

func decode(t *testing.T, raw string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestValidateAcceptsTheGoldenFixture(t *testing.T) {
	raw := inlineGolden
	if b, err := os.ReadFile("../testdata/admin_settings.golden.json"); err == nil {
		raw = string(b)
	}

	if err := managed.Validate(decode(t, raw)); err != nil {
		t.Fatalf("Validate = %v, want nil", err)
	}
}

func TestValidateAcceptsNilAndEmpty(t *testing.T) {
	if err := managed.Validate(nil); err != nil {
		t.Errorf("nil: %v", err)
	}
	if err := managed.Validate(map[string]any{}); err != nil {
		t.Errorf("empty: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{"unknown top-level", `{"foo": 1}`, `"foo"`},
		{"unknown gateway key", `{"gateway": {"foo": 1}}`, `"foo"`},
		{"unknown models key", `{"models": {"foo": 1}}`, `"foo"`},
		{"unknown model entry key", `{"models": {"list": [{"modelId": "a", "foo": 1}]}}`, `"foo"`},
		{"gateway apiKey", `{"gateway": {"apiKey": "s"}}`, "AGY_LLM_GATEWAY_API_KEY"},
		{"Authorization header", `{"gateway": {"customHeaders": {"Authorization": "x"}}}`, "Authorization"},
		{"X-Api-Key header", `{"gateway": {"customHeaders": {"X-Api-Key": "x"}}}`, "X-Api-Key"},
		{"Cookie header", `{"gateway": {"customHeaders": {"Cookie": "x"}}}`, "Cookie"},
		{"wireProtocol grpc", `{"gateway": {"wireProtocol": "grpc"}}`, "wireProtocol"},
		{"http url", `{"gateway": {"url": "http://gw.example.com"}}`, "https"},
		{"enabled without url", `{"gateway": {"enabled": true}}`, "url"},
		{"relative caCertPath", `{"caCertPath": "ca.pem"}`, "caCertPath"},
		{"default not in list", `{"models": {"defaultModelId": "c", "list": [{"modelId": "a"}]}}`, "defaultModelId"},
		{"empty modelId", `{"models": {"list": [{"modelId": ""}]}}`, "modelId"},
		{"list not array", `{"models": {"list": "a"}}`, "list"},
		{"header value not string", `{"gateway": {"customHeaders": {"X-A": 1}}}`, "X-A"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := managed.Validate(decode(t, tt.doc))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestForAgentIgnoresOtherAgents(t *testing.T) {
	bad := map[string]any{"foo": 1}

	if err := managed.ForAgent("claude", bad); err != nil {
		t.Errorf("claude: %v", err)
	}
	if err := managed.ForAgent("agy", bad); err == nil {
		t.Error("agy: want an error for an unknown key")
	}
}
