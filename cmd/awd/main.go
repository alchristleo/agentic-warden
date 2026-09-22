// Command awd is the control plane: it stores the organization's authored
// policy and serves each client the slice of it that applies to them.
//
//	awd serve            run the API server
//	awd apply <file>     store a new policy revision
//
// Policy is append-only. Applying stores a new revision rather than editing
// the last one, so the current policy is a lookup, a rollback is a re-apply,
// and an auditor can see what was in force at any point.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/acme/agent-wrapper/internal/agent/claude/schema"
	"github.com/acme/agent-wrapper/internal/agent/codex/requirements"
	"github.com/acme/agent-wrapper/internal/agent/gemini/managed"
	"github.com/acme/agent-wrapper/internal/config"
	"github.com/acme/agent-wrapper/internal/handler"
	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/store"
)

const usage = `awd is the agent-wrapper control plane.

Usage:
  awd serve                        run the API server
  awd apply <file> [--url URL]     store a new policy revision
  awd enroll-token <user> [--url URL] [--ttl 24h]   mint a single-use token that enrolls one machine
  awd machines [--url URL]                          list enrolled machines
  awd revoke <id> [--url URL]                       revoke a machine's credential
  awd help

Environment:
  AWD_ADDR              listen address (default :8080)
  AWD_DATABASE_URL      Postgres URL; unset means in-memory, for evaluation only
  AWD_LOG_LEVEL         debug, info, warn, error (default info)
  AWD_READ_TIMEOUT      per-request read timeout (default 10s)
  AWD_WRITE_TIMEOUT     per-request write timeout (default 30s)
  AWD_IDLE_TIMEOUT      keep-alive idle timeout (default 120s)
  AWD_SHUTDOWN_TIMEOUT  how long in-flight requests may drain (default 30s)
  AWD_URL               control plane URL used by apply (default http://localhost:8080)
  AWD_ADMIN_TOKEN       bearer token for apply and machine administration; unset disables them
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "awd: "+err.Error())
		os.Exit(1)
	}
}

func run(argv []string) error {
	if len(argv) == 0 {
		fmt.Print(usage)
		return nil
	}
	switch argv[0] {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	case "serve":
		return serve()
	case "apply":
		return apply(argv[1:])
	case "enroll-token":
		return enrollToken(argv[1:])
	case "machines":
		return machines(argv[1:])
	case "revoke":
		return revoke(argv[1:])
	default:
		return fmt.Errorf("unknown command %q; run `awd help`", argv[0])
	}
}

func serve() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))

	backing, err := openStore(cfg, log)
	if err != nil {
		return err
	}

	h := handler.New(backing, log)
	h.ManagedValidator = managedValidator
	h.AdminToken = cfg.AdminToken
	if cfg.AdminToken == "" {
		log.Warn("AWD_ADMIN_TOKEN is unset; apply and machine administration are disabled")
	}
	srv := &http.Server{
		Handler:      h.Routes(),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	// Listening before announcing means the address printed is the one bound,
	// which is the only way a caller learns the port when the configured one
	// is zero.
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", cfg.Addr, err)
	}
	fmt.Printf("listening on %s\n", listener.Addr().String())
	os.Stdout.Sync()

	serverError := make(chan error, 1)
	go func() {
		if err := srv.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
			serverError <- err
			return
		}
		serverError <- nil
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverError:
		return err
	case sig := <-quit:
		log.Info("shutting down", "signal", sig.String())
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutting down: %w", err)
	}
	return nil
}

// openStore picks the backing store. An unset database URL is a deliberate
// evaluation mode, and it says so loudly rather than quietly losing an
// organization's policy on restart.
func openStore(cfg config.Config, log *slog.Logger) (store.Store, error) {
	if cfg.DatabaseURL == "" {
		log.Warn("AWD_DATABASE_URL is unset; storing policy in memory, which does not survive a restart")
		return store.NewMemory(), nil
	}
	// Connecting and migrating happen before the listener opens, so a bad
	// database URL fails the process rather than every request.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pg, err := store.OpenPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	log.Info("connected to Postgres")
	return pg, nil
}

func apply(argv []string) error {
	var path, url string
	for i := 0; i < len(argv); i++ {
		switch arg := argv[i]; {
		case arg == "--url":
			if i+1 >= len(argv) {
				return errors.New("--url needs a value")
			}
			i++
			url = argv[i]
		case strings.HasPrefix(arg, "--url="):
			url = strings.TrimPrefix(arg, "--url=")
		case strings.HasPrefix(arg, "-"):
			return fmt.Errorf("unknown flag %q", arg)
		case path == "":
			path = arg
		default:
			return fmt.Errorf("unexpected argument %q", arg)
		}
	}
	if path == "" {
		return errors.New("apply needs a policy file; run `awd help`")
	}
	if url == "" {
		url = envOr("AWD_URL", "http://localhost:8080")
	}

	// Validating locally first means an author sees the problem with their
	// file rather than a status code from a server.
	ruleSet, err := policy.LoadRuleSet(path, managedValidator)
	if err != nil {
		return err
	}

	body, err := json.Marshal(ruleSet)
	if err != nil {
		return fmt.Errorf("encoding the rule set: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost,
		strings.TrimSuffix(url, "/")+"/v1/policy/revisions", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := os.Getenv("AWD_ADMIN_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if who := appliedBy(); who != "" {
		req.Header.Set("X-Applied-By", who)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("reaching the control plane at %s: %w", url, err)
	}
	defer resp.Body.Close()

	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("control plane refused revision %q: %s: %s",
			ruleSet.Version, resp.Status, strings.TrimSpace(string(payload)))
	}
	fmt.Printf("applied revision %s (%d rules)\n", ruleSet.Version, len(ruleSet.Rules))
	return nil
}

// managedValidator runs every agent's own check over a rule's managed
// settings: Claude's settings schema, Codex's requirements allowlist and
// Gemini's managed-document rules. Each ignores the agents it does not
// know, so adding an agent is adding a line here. The parameter is doc,
// not managed, so the Gemini package name is not shadowed.
func managedValidator(agentName string, doc map[string]any) error {
	if err := schema.ForAgent(agentName, doc); err != nil {
		return err
	}
	if err := requirements.ForAgent(agentName, doc); err != nil {
		return err
	}
	return managed.ForAgent(agentName, doc)
}

func appliedBy() string {
	for _, key := range []string{"AWD_APPLIED_BY", "USER", "USERNAME"} {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// adminArgs is what every administrative command accepts: positional
// arguments, --url, and for enroll-token, --ttl.
type adminArgs struct {
	positional []string
	url        string
	ttl        string
}

func parseAdminArgs(argv []string) (adminArgs, error) {
	var a adminArgs
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		switch {
		case !strings.HasPrefix(arg, "--"):
			a.positional = append(a.positional, arg)
			continue
		case name != "url" && name != "ttl":
			return a, fmt.Errorf("unknown flag %q", arg)
		}
		if !hasValue {
			if i+1 >= len(argv) {
				return a, fmt.Errorf("--%s needs a value", name)
			}
			i++
			value = argv[i]
		}
		if name == "url" {
			a.url = value
		} else {
			a.ttl = value
		}
	}
	if a.url == "" {
		a.url = envOr("AWD_URL", "http://localhost:8080")
	}
	return a, nil
}

// adminRequest sends one authenticated request and returns the decoded
// JSON body, or an error naming the status and the server's message.
func adminRequest(method, url, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, strings.TrimSuffix(url, "/")+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := os.Getenv("AWD_ADMIN_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("reaching the control plane at %s: %w", url, err)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("control plane refused: %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}
	if out != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, out); err != nil {
			return fmt.Errorf("decoding the control plane's response: %w", err)
		}
	}
	return nil
}

// enrollToken mints a single-use enrollment token for one user and prints
// it alone on stdout, so a caller can pipe it straight into a machine's
// enrollment step without scraping surrounding text.
func enrollToken(argv []string) error {
	a, err := parseAdminArgs(argv)
	if err != nil {
		return err
	}
	if len(a.positional) != 1 {
		return errors.New("enroll-token needs exactly one user; run `awd help`")
	}
	body := map[string]string{"user": a.positional[0]}
	if a.ttl != "" {
		body["ttl"] = a.ttl
	}
	var minted struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expiresAt"`
	}
	if err := adminRequest(http.MethodPost, a.url, "/v1/enrollment-tokens", body, &minted); err != nil {
		return err
	}
	// The token alone on stdout, so `aw-sync enroll --token $(awd enroll-token ...)` works.
	fmt.Println(minted.Token)
	fmt.Fprintf(os.Stderr, "token for %s expires %s\n", a.positional[0], minted.ExpiresAt.Format(time.RFC3339))
	return nil
}

// machines lists every enrolled machine as one line each, for an operator
// scanning the fleet or piping the output into grep.
func machines(argv []string) error {
	a, err := parseAdminArgs(argv)
	if err != nil {
		return err
	}
	if len(a.positional) != 0 {
		return errors.New("machines takes no arguments")
	}
	var list []model.Machine
	if err := adminRequest(http.MethodGet, a.url, "/v1/machines", nil, &list); err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tUSER\tNAME\tOS\tENROLLED\tLAST SEEN\tVERSION")
	for _, m := range list {
		lastSeen := "never"
		if !m.LastSeenAt.IsZero() {
			lastSeen = m.LastSeenAt.Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			m.ID, m.User, m.Name, m.OS, m.EnrolledAt.Format(time.RFC3339), lastSeen, m.LastBundleVersion)
	}
	return w.Flush()
}

// revoke deletes one machine's credential, so a lost or decommissioned
// machine immediately loses access to the bundle.
func revoke(argv []string) error {
	a, err := parseAdminArgs(argv)
	if err != nil {
		return err
	}
	if len(a.positional) != 1 {
		return errors.New("revoke needs exactly one machine id; run `awd help`")
	}
	if err := adminRequest(http.MethodDelete, a.url, "/v1/machines/"+a.positional[0], nil, nil); err != nil {
		return err
	}
	fmt.Printf("revoked %s\n", a.positional[0])
	return nil
}
