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

func TestValidateAcceptsAWindowsCACertPath(t *testing.T) {
	for _, path := range []string{`C:\\ProgramData\\ca.pem`, `C:/ca.pem`} {
		if err := managed.Validate(decode(t, `{"caCertPath": "`+path+`"}`)); err != nil {
			t.Errorf("%s: %v", path, err)
		}
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
		{"url with userinfo", `{"gateway": {"url": "https://u:p@gw.example.com"}}`, "gateway.url"},
		{"url with query", `{"gateway": {"url": "https://gw.example.com/?k=v"}}`, "gateway.url"},
		{"url with fragment", `{"gateway": {"url": "https://gw.example.com/#f"}}`, "gateway.url"},
		{"Bearer header value", `{"gateway": {"customHeaders": {"X-A": "bearer abc"}}}`, "X-A"},
		{"Basic header value", `{"gateway": {"customHeaders": {"X-A": "Basic abc"}}}`, "X-A"},
		{"AIza header value", `{"gateway": {"customHeaders": {"X-A": "AIzaSyX"}}}`, "X-A"},
		{"leading-space Bearer value", `{"gateway": {"customHeaders": {"X-A": " Bearer x"}}}`, "X-A"},
		{"Token header value", `{"gateway": {"customHeaders": {"X-A": "Token x"}}}`, "X-A"},
		{"Digest header value", `{"gateway": {"customHeaders": {"X-A": "Digest x"}}}`, "X-A"},
		{"empty model list", `{"models": {"list": []}}`, "models.list"},
		{"relative caCertPath", `{"caCertPath": "ca.pem"}`, "caCertPath"},
		{"empty modelId", `{"models": {"list": [{"modelId": ""}]}}`, "modelId"},
		{"list not array", `{"models": {"list": "a"}}`, "list"},
		{"header value not string", `{"gateway": {"customHeaders": {"X-A": 1}}}`, "X-A"},
		{"drive-relative caCertPath", `{"caCertPath": "C:ca.pem"}`, "caCertPath"},
		{"empty defaultModelId", `{"models": {"defaultModelId": ""}}`, "defaultModelId"},
		{"defaultModelId not string", `{"models": {"defaultModelId": 5}}`, "defaultModelId"},
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

func TestValidateChecksEachFieldAlone(t *testing.T) {
	for _, raw := range []string{
		`{"gateway": {"enabled": true}}`,
		`{"models": {"defaultModelId": "c", "list": [{"modelId": "a"}]}}`,
	} {
		if err := managed.Validate(decode(t, raw)); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
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
