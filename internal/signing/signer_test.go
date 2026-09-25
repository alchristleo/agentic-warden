package signing

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBundleStatementBytes(t *testing.T) {
	got := string(BundleStatement("3f9a1c22b0d41e77", []byte("{}")))
	want := "aw-bundle-v2\n3f9a1c22b0d41e77\n44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a\n"
	if got != want {
		t.Fatalf("statement:\n%q\nwant\n%q", got, want)
	}
}

func TestSeedSignerRoundTripsBothFormats(t *testing.T) {
	key, _ := Generate()
	s := NewSeedSigner(key)
	if !reflect.DeepEqual(s.Formats(), []string{FormatV2, FormatV1}) {
		t.Fatalf("formats %v", s.Formats())
	}
	body := []byte(`{"version":"v1"}`)
	for _, format := range []string{FormatV1, FormatV2} {
		msg := body
		if format == FormatV2 {
			msg = BundleStatement(KeyID(s.Public()), body)
		}
		raw, err := s.Sign(context.Background(), msg)
		if err != nil {
			t.Fatal(err)
		}
		sig := base64.StdEncoding.EncodeToString(raw)
		if !VerifyBundle(format, s.Public(), body, sig) {
			t.Fatalf("%s did not verify", format)
		}
		if VerifyBundle(format, s.Public(), []byte(`{"version":"v2"}`), sig) {
			t.Fatalf("%s verified a changed body", format)
		}
	}
	if VerifyBundle("v3", s.Public(), body, "x") {
		t.Fatal("unknown format verified")
	}
}

// A v2 signature is over the statement, not the body: it must not verify
// as v1, or a downgrade could replay it.
func TestV2SignatureDoesNotVerifyAsV1(t *testing.T) {
	key, _ := Generate()
	s := NewSeedSigner(key)
	body := []byte("{}")
	raw, _ := s.Sign(context.Background(), BundleStatement(KeyID(s.Public()), body))
	if VerifyBundle(FormatV1, s.Public(), body, base64.StdEncoding.EncodeToString(raw)) {
		t.Fatal("v2 signature accepted as v1")
	}
}

func TestParseFormats(t *testing.T) {
	cases := map[string][]string{
		"":          {"v1"},
		"v1, v2":    {"v1", "v2"},
		" V2 ,v1 ":  {"v2", "v1"},
		"v2,v2,v1":  {"v2", "v1"},
		"v9, bogus": {"v1"},
		"v9, v2":    {"v2"},
		",,":        {"v1"},
	}
	for in, want := range cases {
		if got := ParseFormats(in); !reflect.DeepEqual(got, want) {
			t.Errorf("ParseFormats(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestChoose(t *testing.T) {
	if f, ok := Choose([]string{"v1", "v2"}, []string{"v2", "v1"}); !ok || f != "v2" {
		t.Fatalf("seed+new client = %q %v", f, ok)
	}
	if f, ok := Choose([]string{"v1"}, []string{"v2", "v1"}); !ok || f != "v1" {
		t.Fatalf("seed+old client = %q %v", f, ok)
	}
	if _, ok := Choose([]string{"v1"}, []string{"v2"}); ok {
		t.Fatal("kms+old client chose something")
	}
}

func FuzzParseFormats(f *testing.F) {
	for _, s := range []string{"", "v1, v2", " V2 ,v1 ", "v9", ",,,"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := ParseFormats(s)
		if len(got) == 0 {
			t.Fatal("empty result")
		}
		for _, g := range got {
			if g != FormatV1 && g != FormatV2 {
				t.Fatalf("unknown format %q in %v", g, got)
			}
		}
	})
}

func TestSignatureLine(t *testing.T) {
	if got := SignatureLine(FormatV1, "k", "s"); got != "aw-ed25519 k s\n" {
		t.Fatalf("v1 line %q", got)
	}
	if got := SignatureLine(FormatV2, "k", "s"); got != "aw-ed25519-v2 k s\n" {
		t.Fatalf("v2 line %q", got)
	}
}

func writeSigned(t *testing.T, format string, tamperToken string) (dir string, s Signer) {
	t.Helper()
	dir = t.TempDir()
	key, _ := Generate()
	s = NewSeedSigner(key)
	body := []byte(`{"version":"v1"}`)
	msg := body
	if format == FormatV2 {
		msg = BundleStatement(KeyID(s.Public()), body)
	}
	raw, _ := s.Sign(context.Background(), msg)
	line := SignatureLine(format, KeyID(s.Public()), base64.StdEncoding.EncodeToString(raw))
	if tamperToken != "" {
		line = tamperToken + line[strings.Index(line, " "):]
	}
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(dir, "trust"), []byte(FormatPublic(s.Public())+"\n"), 0o644))
	must(os.WriteFile(filepath.Join(dir, "bundle"), body, 0o644))
	must(os.WriteFile(filepath.Join(dir, "sig"), []byte(line), 0o644))
	return dir, s
}

func TestVerifyFilesBothFormats(t *testing.T) {
	for _, format := range []string{FormatV1, FormatV2} {
		dir, s := writeSigned(t, format, "")
		body, gotFormat, keyID, missing, err := VerifyFiles(filepath.Join(dir, "trust"), filepath.Join(dir, "bundle"), filepath.Join(dir, "sig"))
		if err != nil || missing || gotFormat != format || keyID != KeyID(s.Public()) || string(body) != `{"version":"v1"}` {
			t.Fatalf("%s: body=%q format=%q key=%q missing=%v err=%v", format, body, gotFormat, keyID, missing, err)
		}
	}
}

func TestVerifyFilesUnknownTokenIsNamed(t *testing.T) {
	dir, _ := writeSigned(t, FormatV2, "aw-ed25519-v9")
	_, _, _, _, err := VerifyFiles(filepath.Join(dir, "trust"), filepath.Join(dir, "bundle"), filepath.Join(dir, "sig"))
	if err == nil || !strings.Contains(err.Error(), "aw-ed25519-v9") {
		t.Fatalf("err = %v, want it to name the token", err)
	}
}

func TestVerifyFilesV2RejectsAChangedBody(t *testing.T) {
	dir, _ := writeSigned(t, FormatV2, "")
	if err := os.WriteFile(filepath.Join(dir, "bundle"), []byte(`{"version":"evil"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := VerifyFiles(filepath.Join(dir, "trust"), filepath.Join(dir, "bundle"), filepath.Join(dir, "sig")); err == nil {
		t.Fatal("changed body verified")
	}
}

// A v2 line claiming the right key ID but signed by another key fails.
func TestVerifyFilesV2WrongKey(t *testing.T) {
	dir, s := writeSigned(t, FormatV2, "")
	other, _ := Generate()
	if err := os.WriteFile(filepath.Join(dir, "trust"), []byte(FormatPublic(other.Public().(ed25519.PublicKey))+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = s
	if _, _, _, _, err := VerifyFiles(filepath.Join(dir, "trust"), filepath.Join(dir, "bundle"), filepath.Join(dir, "sig")); err == nil {
		t.Fatal("wrong key verified")
	}
}

func TestSignRolloverThroughASigner(t *testing.T) {
	prev, _ := Generate()
	next, _ := Generate()
	header, err := SignRollover(context.Background(), NewSeedSigner(prev), next.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	r, err := VerifyRollover(prev.Public().(ed25519.PublicKey), header)
	if err != nil || r.KeyID != KeyID(next.Public().(ed25519.PublicKey)) {
		t.Fatalf("rollover = %+v, %v", r, err)
	}
}
