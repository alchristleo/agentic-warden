package main_test

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alchristleo/agentic-warden/internal/config"
	"github.com/alchristleo/agentic-warden/internal/credential"
)

// writeCertPair writes a self-signed certificate for 127.0.0.1 and returns
// the cert path, the key path and a pool trusting it.
func writeCertPair(t *testing.T) (certPath, keyPath string, pool *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "awd-test"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath, keyPath = filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	pool = x509.NewCertPool()
	pool.AddCert(cert)
	return certPath, keyPath, pool
}

// serveFailure runs `awd serve` to completion and returns its combined
// output. It fails the test if awd exits 0.
func serveFailure(t *testing.T, env ...string) (stderr string) {
	t.Helper()
	cmd := exec.Command(built, "serve")
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("awd serve exited 0, output %q", out)
	}
	return string(out)
}

func TestServeTLSHTTPS(t *testing.T) {
	certPath, keyPath, pool := writeCertPair(t)
	s := startServerTLS(t, certPath, keyPath)

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	resp, err := client.Get(s + "/healthz")
	if err != nil {
		t.Fatalf("https get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	old := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS11}}}
	if resp, err := old.Get(s + "/healthz"); err == nil {
		resp.Body.Close()
		t.Fatal("a TLS 1.1 client was served")
	}
}

// startServerTLS starts awd with the given pair and returns its https URL.
func startServerTLS(t *testing.T, certPath, keyPath string) string {
	t.Helper()
	cmd := exec.Command(built, "serve")
	cmd.Env = append(os.Environ(), "AWD_ADDR=127.0.0.1:0", "AWD_LOG_LEVEL=error",
		"AWD_TLS_CERT_FILE="+certPath, "AWD_TLS_KEY_FILE="+keyPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_ = cmd.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("reading the listen address: %v", err)
	}
	return "https://" + strings.TrimSpace(strings.TrimPrefix(line, "listening on "))
}

func TestServeTLSOneVarSetNamesTheOther(t *testing.T) {
	certPath, keyPath, _ := writeCertPair(t)
	if out := serveFailure(t, "AWD_ADDR=127.0.0.1:0", "AWD_TLS_CERT_FILE="+certPath); !strings.Contains(out, "AWD_TLS_KEY_FILE") {
		t.Errorf("cert only: %q", out)
	}
	if out := serveFailure(t, "AWD_ADDR=127.0.0.1:0", "AWD_TLS_KEY_FILE="+keyPath); !strings.Contains(out, "AWD_TLS_CERT_FILE") {
		t.Errorf("key only: %q", out)
	}
}

func TestServeTLSBadPairBindsNothing(t *testing.T) {
	// The port is held, so a bind attempt would fail with a listen error.
	// The pair error must win, which shows the load ran first.
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	bad := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(bad, []byte("not a pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := serveFailure(t, "AWD_ADDR="+held.Addr().String(), "AWD_TLS_CERT_FILE="+bad, "AWD_TLS_KEY_FILE="+bad)
	if !strings.Contains(out, "TLS certificate pair") || strings.Contains(out, "listening on") {
		t.Errorf("output %q, want the pair error and no listen error", out)
	}
}

func TestAdminTokenCommand(t *testing.T) {
	cmd := exec.Command(built, "admin-token", "alice")
	cmd.Env = append(os.Environ(), "AWD_LOG_LEVEL=error", "AWD_TLS_CERT_FILE=only-one-set")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v: %s", err, stderr.String())
	}
	token := strings.TrimSpace(stderr.String())
	line := strings.TrimSpace(stdout.String())
	if token == "" || strings.Contains(token, "\n") {
		t.Fatalf("stderr = %q, want the token alone", stderr.String())
	}
	if line != "alice "+credential.Hash(token) {
		t.Errorf("stdout = %q, want alice + hash of the stderr token", line)
	}

	parsed, err := config.ParseAdminTokens(strings.NewReader(line + "\n"))
	if err != nil || parsed["alice"] != credential.Hash(token) {
		t.Fatalf("round trip: %v, %v", parsed, err)
	}
	file := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(file, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := startServerEnv(t, []string{"AWD_ADMIN_TOKENS_FILE=" + file})
	req, _ := http.NewRequest(http.MethodGet, s.url+"/v1/machines", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("minted token got %d, want 200", resp.StatusCode)
	}

	for _, bad := range []string{"Bad", "a b", ""} {
		args := []string{"admin-token"}
		if bad != "" {
			args = append(args, bad)
		}
		if _, code := runAwdEnv(t, nil, args...); code == 0 {
			t.Errorf("admin-token %q succeeded", bad)
		}
	}
}

func TestLimiterAnswers429ToAServedAwd(t *testing.T) {
	s := startServer(t)
	status := func() int {
		req, _ := http.NewRequest(http.MethodGet, s.url+"/v1/machines", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for i := 1; i <= 10; i++ {
		if got := status(); got != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d, want 401", i, got)
		}
	}
	if got := status(); got != http.StatusTooManyRequests {
		t.Errorf("attempt 11: %d, want 429", got)
	}
	resp, err := http.Get(s.url + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz after the 429: %d, want 200", resp.StatusCode)
	}
}
