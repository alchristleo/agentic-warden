package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alchristleo/agentic-warden/internal/config"
)

const (
	hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	// sha256 of "legacy".
	hashLegacy = "c49fea7425fa7f8699897a97c159c6690267d9003bb78c53fafa8fc15c325d84"
)

func TestAdminTokensFileAcceptsCommentsAndBlanks(t *testing.T) {
	in := "# header\n\nalice " + hashA + "\n  \nbob@x.y-z " + hashB + "\n"
	got, err := config.ParseAdminTokens(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseAdminTokens: %v", err)
	}
	if len(got) != 2 || got["alice"] != hashA || got["bob@x.y-z"] != hashB {
		t.Errorf("got %v", got)
	}
}

func TestAdminTokensFileErrorsCarryLineNotText(t *testing.T) {
	secret := "SECRETTOKENTEXT"
	cases := map[string]struct{ in, line string }{
		"bad name":      {"alice " + hashA + "\nBad " + hashB + "\n", "line 2"},
		"slash":         {"a/b " + hashA + "\n", "line 1"},
		"name too long": {"alice " + hashA + "\n" + strings.Repeat("a", 65) + " " + hashB + "\n", "line 2"},
		"short hash":    {"# c\nalice abc" + secret + "\n", "line 2"},
		"upper hash":    {"alice " + strings.ToUpper(hashA) + "\n", "line 1"},
		"non hex hash":  {"alice " + strings.Repeat("g", 64) + "\n", "line 1"},
		"missing hash":  {"\n\nalice" + secret + "\n", "line 3"},
		"extra field":   {"alice " + hashA + " " + secret + "\n", "line 1"},
		"duplicate":     {"alice " + hashA + "\n\nalice " + hashB + "\n", "line 3"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := config.ParseAdminTokens(strings.NewReader(c.in))
			if err == nil {
				t.Fatal("want an error")
			}
			msg := err.Error()
			if !strings.Contains(msg, c.line) {
				t.Errorf("error %q lacks %q", msg, c.line)
			}
			for _, leak := range []string{secret, hashA, hashB, strings.ToUpper(hashA)} {
				if strings.Contains(msg, leak) {
					t.Errorf("error %q leaks %q", msg, leak)
				}
			}
		})
	}
}

func writeTokens(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAdminTokensFileMergesWithEnvToken(t *testing.T) {
	t.Setenv("AWD_ADMIN_TOKENS_FILE", writeTokens(t, "alice "+hashA+"\n"))
	t.Setenv("AWD_ADMIN_TOKEN", "legacy")
	got, err := config.FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if len(got.AdminTokens) != 2 || got.AdminTokens["alice"] != hashA || got.AdminTokens["admin"] != hashLegacy {
		t.Errorf("AdminTokens = %v", got.AdminTokens)
	}
}

func TestAdminTokensFileAdminClashesWithEnvToken(t *testing.T) {
	t.Setenv("AWD_ADMIN_TOKENS_FILE", writeTokens(t, "alice "+hashA+"\nadmin "+hashB+"\n"))
	t.Setenv("AWD_ADMIN_TOKEN", "legacy")
	_, err := config.FromEnv()
	if err == nil || !strings.Contains(err.Error(), "line 2") || strings.Contains(err.Error(), hashB) {
		t.Fatalf("err = %v, want line 2 without the hash", err)
	}
}

func TestAdminTokensFileAdminAloneIsFine(t *testing.T) {
	t.Setenv("AWD_ADMIN_TOKENS_FILE", writeTokens(t, "admin "+hashB+"\n"))
	got, err := config.FromEnv()
	if err != nil || got.AdminTokens["admin"] != hashB {
		t.Fatalf("got %v, %v", got.AdminTokens, err)
	}
}

func TestAdminTokensFileUnreadable(t *testing.T) {
	t.Setenv("AWD_ADMIN_TOKENS_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, err := config.FromEnv(); err == nil || !strings.Contains(err.Error(), "AWD_ADMIN_TOKENS_FILE") {
		t.Fatalf("err = %v", err)
	}
}

func TestAdminTokensEnvTokenAlone(t *testing.T) {
	t.Setenv("AWD_ADMIN_TOKEN", "legacy")
	got, err := config.FromEnv()
	if err != nil || len(got.AdminTokens) != 1 || got.AdminTokens["admin"] != hashLegacy {
		t.Fatalf("got %v, %v", got.AdminTokens, err)
	}
	t.Setenv("AWD_ADMIN_TOKEN", "")
	got, err = config.FromEnv()
	if err != nil || len(got.AdminTokens) != 0 {
		t.Fatalf("unset: got %v, %v", got.AdminTokens, err)
	}
}

func TestFromEnvTLSPair(t *testing.T) {
	t.Setenv("AWD_TLS_CERT_FILE", "/c.pem")
	_, err := config.FromEnv()
	if err == nil || !strings.Contains(err.Error(), "AWD_TLS_KEY_FILE") {
		t.Fatalf("cert only: err = %v, want it to name AWD_TLS_KEY_FILE", err)
	}
	t.Setenv("AWD_TLS_CERT_FILE", "")
	t.Setenv("AWD_TLS_KEY_FILE", "/k.pem")
	_, err = config.FromEnv()
	if err == nil || !strings.Contains(err.Error(), "AWD_TLS_CERT_FILE") {
		t.Fatalf("key only: err = %v, want it to name AWD_TLS_CERT_FILE", err)
	}
	t.Setenv("AWD_TLS_CERT_FILE", "/c.pem")
	got, err := config.FromEnv()
	if err != nil || got.TLSCertFile != "/c.pem" || got.TLSKeyFile != "/k.pem" {
		t.Fatalf("both: %+v, %v", got, err)
	}
}

func TestFromEnvTrustedProxies(t *testing.T) {
	t.Setenv("AWD_TRUSTED_PROXIES", "10.0.0.0/8, 2001:db8::/32")
	got, err := config.FromEnv()
	if err != nil || len(got.TrustedProxies) != 2 ||
		got.TrustedProxies[0].String() != "10.0.0.0/8" || got.TrustedProxies[1].String() != "2001:db8::/32" {
		t.Fatalf("got %v, %v", got.TrustedProxies, err)
	}
	t.Setenv("AWD_TRUSTED_PROXIES", "10.0.0.0/8,nonsense")
	if _, err := config.FromEnv(); err == nil || !strings.Contains(err.Error(), "AWD_TRUSTED_PROXIES") {
		t.Fatalf("bad CIDR: err = %v", err)
	}
	t.Setenv("AWD_TRUSTED_PROXIES", "")
	got, err = config.FromEnv()
	if err != nil || len(got.TrustedProxies) != 0 {
		t.Fatalf("unset: %v, %v", got.TrustedProxies, err)
	}
}

func TestAdminTokensFileDuplicateHashRejected(t *testing.T) {
	_, err := config.ParseAdminTokens(strings.NewReader("alice " + hashA + "\nbob " + hashA + "\n"))
	if err == nil || !strings.Contains(err.Error(), "line 2") || strings.Contains(err.Error(), hashA) {
		t.Fatalf("err = %v, want line 2 without the hash", err)
	}
}

func TestAdminTokensFileHashSameAsEnvTokenRejected(t *testing.T) {
	t.Setenv("AWD_ADMIN_TOKENS_FILE", writeTokens(t, "alice "+hashA+"\nbob "+hashLegacy+"\n"))
	t.Setenv("AWD_ADMIN_TOKEN", "legacy")
	_, err := config.FromEnv()
	if err == nil || !strings.Contains(err.Error(), "line 2") || strings.Contains(err.Error(), hashLegacy) {
		t.Fatalf("err = %v, want line 2 without the hash", err)
	}
}
