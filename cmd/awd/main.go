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
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
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
	"github.com/acme/agent-wrapper/internal/console"
	"github.com/acme/agent-wrapper/internal/console/authz"
	"github.com/acme/agent-wrapper/internal/console/session"
	"github.com/acme/agent-wrapper/internal/console/sso"
	"github.com/acme/agent-wrapper/internal/handler"
	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/signing"
	"github.com/acme/agent-wrapper/internal/store"
	"sigs.k8s.io/yaml"
)

const usage = `awd is the agent-wrapper control plane.

Usage:
  awd serve                        run the API server
  awd keygen --out FILE            write a new signing key
  awd apply <file> [--url URL]     store a new policy revision
  awd enroll-token <user> [--url URL] [--ttl 24h]   mint a single-use token that enrolls one machine
  awd machines [--url URL]                          list enrolled machines
  awd revoke <id> [--url URL]                       revoke a machine's credential
  awd groups apply FILE [--url URL]   push an identity-provider membership snapshot
  awd groups [--url URL]              show the current membership snapshot
  awd groups resolve USER [--url URL] show one user's groups by source
  awd help

Environment:
  AWD_ADDR                   listen address (default :8080)
  AWD_DATABASE_URL           Postgres URL; unset means in-memory, for evaluation only
  AWD_LOG_LEVEL              debug, info, warn, error (default info)
  AWD_READ_TIMEOUT           per-request read timeout (default 10s)
  AWD_WRITE_TIMEOUT          per-request write timeout (default 30s)
  AWD_IDLE_TIMEOUT           keep-alive idle timeout (default 120s)
  AWD_SHUTDOWN_TIMEOUT       how long in-flight requests may drain (default 30s)
  AWD_URL                    control plane URL used by apply (default http://localhost:8080)
  AWD_ADMIN_TOKEN            bearer token for apply and machine administration; unset disables them
  AWD_SCIM_TOKEN             bearer token the identity provider presents on /scim/v2; unset disables SCIM
  AWD_SIGNING_KEY            path to the signing key written by keygen; unset means bundles are not signed
  AWD_SIGNING_KEY_PREVIOUS   path to the key being rotated out, signed over during a rotation
  AWD_PUBLIC_URL                   external https origin of awd; enables the console with AWD_CONSOLE_*
  AWD_CONSOLE_ISSUER               OIDC issuer URL
  AWD_CONSOLE_CLIENT_ID            OIDC client id
  AWD_CONSOLE_CLIENT_SECRET_FILE   file holding the OIDC client secret
  AWD_CONSOLE_ADMIN_GROUP          IdP group whose members are console admins
  AWD_CONSOLE_USER_CLAIM           ID token claim naming the user (default email)
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
	case "keygen":
		return keygen(argv[1:])
	case "apply":
		return apply(argv[1:])
	case "enroll-token":
		return enrollToken(argv[1:])
	case "machines":
		return machines(argv[1:])
	case "revoke":
		return revoke(argv[1:])
	case "groups":
		if len(argv) > 1 && argv[1] == "apply" {
			return groupsApply(argv[2:])
		}
		if len(argv) > 1 && argv[1] == "resolve" {
			return groupsResolve(argv[2:])
		}
		return groups(argv[1:])
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

	sweepCtx, stopSweep := context.WithCancel(context.Background())
	defer stopSweep()

	h := handler.New(backing, log)
	h.ManagedValidator = managedValidator
	h.AdminToken = cfg.AdminToken
	if cfg.AdminToken == "" {
		log.Warn("AWD_ADMIN_TOKEN is unset; apply and machine administration are disabled")
	}
	h.SCIMToken = cfg.SCIMToken
	if cfg.SCIMToken != "" {
		log.Info("SCIM provisioning is enabled at /scim/v2")
		// The IdP's token lives in its own configuration, separate from
		// operators; sharing it with AdminToken means anyone who can
		// provision users can also administer the control plane, and vice
		// versa. Never log either token's value.
		if cfg.SCIMToken == cfg.AdminToken {
			log.Warn("AWD_SCIM_TOKEN equals AWD_ADMIN_TOKEN; they should be distinct")
		}
	}

	// Signing is opt-in: a deployment with no key keeps serving exactly as
	// it did, and every client keeps accepting what it serves.
	if value := os.Getenv("AWD_SIGNING_KEY"); value != "" {
		current, err := loadSigner(context.Background(), value)
		if err != nil {
			return fmt.Errorf("awd: AWD_SIGNING_KEY: %w", err)
		}
		s := &handler.Signer{Current: current}
		if previous := os.Getenv("AWD_SIGNING_KEY_PREVIOUS"); previous != "" {
			// A rotation that cannot vouch for its new key leaves every
			// machine pinned to a key nothing signs with any more.
			old, err := loadSigner(context.Background(), previous)
			if err != nil {
				return fmt.Errorf("awd: AWD_SIGNING_KEY_PREVIOUS: %w", err)
			}
			s.Previous = old
		}
		h.Signer = s
		log.Info("signing bundles", "keyId", s.KeyID(), "formats", strings.Join(current.Formats(), ","))
	} else {
		log.Warn("bundles are not signed; set AWD_SIGNING_KEY to sign them")
	}

	if cfg.ConsoleEnabled() {
		secret, err := os.ReadFile(cfg.Console.ClientSecretFile)
		if err != nil {
			return fmt.Errorf("awd: AWD_CONSOLE_CLIENT_SECRET_FILE: %w", err)
		}
		clientSecret := strings.TrimSpace(string(secret))
		if clientSecret == "" {
			return fmt.Errorf("awd: AWD_CONSOLE_CLIENT_SECRET_FILE %s is empty", cfg.Console.ClientSecretFile)
		}
		client := sso.New(sso.Config{
			Issuer: cfg.Console.Issuer, ClientID: cfg.Console.ClientID, ClientSecret: clientSecret,
			RedirectURL: cfg.PublicURL.String() + "/console/auth/callback", UserClaim: cfg.Console.UserClaim,
		})
		dctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := client.Discover(dctx); err != nil {
			log.Warn("console: identity provider discovery failed; retrying on the next sign-in", "err", err)
		}
		cancel()
		sessions := session.NewManager(backing)
		h.Console = &handler.Console{
			PublicURL: cfg.PublicURL, SSO: client, Sessions: sessions,
			Authz:  &authz.Checker{Store: backing, Group: cfg.Console.AdminGroup},
			Assets: console.Assets(),
		}
		go sweepSessions(sweepCtx, sessions, log)
		log.Info("admin console enabled", "url", cfg.PublicURL.String()+"/console/")
	} else {
		log.Info("admin console disabled; set AWD_PUBLIC_URL and AWD_CONSOLE_* to enable it")
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

// sweepSessions deletes expired console sessions hourly. Lookup already
// refuses them; this only keeps the table from growing.
func sweepSessions(ctx context.Context, m *session.Manager, log *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := m.Sweep(ctx); err != nil {
				log.Error("sweeping console sessions", "err", err)
			} else if n > 0 {
				log.Debug("swept console sessions", "count", n)
			}
		}
	}
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

// loadSigner reads AWD_SIGNING_KEY's value: a path to a seed file written
// by keygen.
func loadSigner(_ context.Context, value string) (signing.Signer, error) {
	key, err := signing.LoadSeed(value)
	if err != nil {
		return nil, err
	}
	return signing.NewSeedSigner(key), nil
}

// keygen writes a new signing key and prints the public half. It never
// replaces an existing key: a control plane that quietly started signing
// with a different key would strand every machine that pinned the old one.
func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	out := fs.String("out", "", "where to write the signing key (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("awd keygen: --out names the file to write")
	}
	key, err := signing.Generate()
	if err != nil {
		return err
	}
	if err := signing.WriteSeed(*out, key); err != nil {
		return err
	}
	pub := key.Public().(ed25519.PublicKey)
	fmt.Printf("wrote %s\npublic key %s\nkey id     %s\n", *out, signing.FormatPublic(pub), signing.KeyID(pub))
	return nil
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
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &controlPlaneError{Status: resp.StatusCode, Message: strings.TrimSpace(string(payload))}
	}
	if out != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, out); err != nil {
			return fmt.Errorf("decoding the control plane's response: %w", err)
		}
	}
	return nil
}

// controlPlaneError is a non-2xx answer from awd, kept as a type so a
// caller can act on the status without parsing the message.
type controlPlaneError struct {
	// Status is the HTTP status code the control plane answered with.
	Status int
	// Message is the response body, trimmed of surrounding whitespace.
	Message string
}

// Error renders the same text adminRequest has always produced, so a
// caller that only prints err.Error() sees no change.
func (e *controlPlaneError) Error() string {
	return fmt.Sprintf("control plane refused: %d %s: %s", e.Status, http.StatusText(e.Status), e.Message)
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
	fmt.Fprintln(w, "ID\tUSER\tNAME\tOS\tENROLLED\tLAST SEEN\tVERSION\tKEY")
	for _, m := range list {
		lastSeen := "never"
		if !m.LastSeenAt.IsZero() {
			lastSeen = m.LastSeenAt.Format(time.RFC3339)
		}
		keyID := m.LastKeyID
		if keyID == "" {
			keyID = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			m.ID, m.User, m.Name, m.OS, m.EnrolledAt.Format(time.RFC3339), lastSeen, m.LastBundleVersion, keyID)
	}
	return w.Flush()
}

// groupSummary is what /v1/groups answers with.
type groupSummary struct {
	// Source names the exporter that produced the current snapshot.
	Source string `json:"source"`
	// AppliedBy is who posted the current snapshot, from the client's environment.
	AppliedBy string `json:"appliedBy"`
	// SyncedAt is when the server stored the current snapshot.
	SyncedAt time.Time `json:"syncedAt"`
	// Users is the number of distinct member keys in the snapshot.
	Users int `json:"users"`
	// Groups is the number of distinct group names across all members.
	Groups int `json:"groups"`
	// HasSnapshot is false when only SCIM data exists. A server from
	// before SCIM never sends it, and always had a snapshot on a 200.
	HasSnapshot *bool `json:"hasSnapshot"`
	// SCIM is present when the server has SCIM enabled.
	SCIM *struct {
		Users       int `json:"users"`
		ActiveUsers int `json:"activeUsers"`
		Groups      int `json:"groups"`
	} `json:"scim"`
}

// groupsApply posts a membership snapshot from a JSON or YAML file. The
// file is decoded locally first so a malformed export is reported with its
// path rather than as a status code.
func groupsApply(argv []string) error {
	a, err := parseAdminArgs(argv)
	if err != nil {
		return err
	}
	if len(a.positional) != 1 {
		return errors.New("groups apply takes one argument: the snapshot file")
	}
	raw, err := os.ReadFile(a.positional[0])
	if err != nil {
		return err
	}
	var body struct {
		Source  string              `json:"source"`
		Members map[string][]string `json:"members"`
	}
	if err := yaml.UnmarshalStrict(raw, &body); err != nil {
		return fmt.Errorf("%s: %w", a.positional[0], err)
	}
	if body.Members == nil {
		return fmt.Errorf("%s: no members map", a.positional[0])
	}
	var summary groupSummary
	if err := adminRequest(http.MethodPut, a.url, "/v1/groups", body, &summary); err != nil {
		return err
	}
	fmt.Printf("applied group snapshot from %s: %d users, %d groups\n", orUnknownSource(summary.Source), summary.Users, summary.Groups)
	return nil
}

// groups prints the current snapshot's summary; none is a fact, not an error.
func groups(argv []string) error {
	a, err := parseAdminArgs(argv)
	if err != nil {
		return err
	}
	if len(a.positional) != 0 {
		return errors.New("groups takes no arguments; did you mean `groups apply FILE`?")
	}
	var summary groupSummary
	err = adminRequest(http.MethodGet, a.url, "/v1/groups", nil, &summary)
	if err != nil {
		var cpErr *controlPlaneError
		if errors.As(err, &cpErr) && cpErr.Status == http.StatusNotFound {
			fmt.Println("no group snapshot has been applied; bundles resolve from the policy's groups map alone")
			return nil
		}
		return err
	}
	if summary.HasSnapshot == nil || *summary.HasSnapshot {
		fmt.Printf("source: %s\nsynced: %s\nby: %s\nusers: %d\ngroups: %d\n",
			orUnknownSource(summary.Source), summary.SyncedAt.Format(time.RFC3339), orNoneString(summary.AppliedBy), summary.Users, summary.Groups)
	} else {
		fmt.Println("no group snapshot has been applied")
	}
	if summary.SCIM != nil {
		fmt.Printf("scim users: %d (%d active)\nscim groups: %d\n", summary.SCIM.Users, summary.SCIM.ActiveUsers, summary.SCIM.Groups)
	}
	return nil
}

// groupsResolve prints one user's groups by source, and warns when SCIM
// holds the user under a different case — the mapping mistake resolution
// deliberately does not paper over.
func groupsResolve(argv []string) error {
	a, err := parseAdminArgs(argv)
	if err != nil {
		return err
	}
	if len(a.positional) != 1 {
		return errors.New("groups resolve takes one argument: the enrolled user")
	}
	var s struct {
		Authored      []string `json:"authored"`
		Snapshot      []string `json:"snapshot"`
		SCIM          []string `json:"scim"`
		Effective     []string `json:"effective"`
		SCIMNearMatch *string  `json:"scimNearMatch"`
	}
	if err := adminRequest(http.MethodGet, a.url, "/v1/groups/resolve?user="+url.QueryEscape(a.positional[0]), nil, &s); err != nil {
		return err
	}
	list := func(groups []string) string {
		if len(groups) == 0 {
			return "(none)"
		}
		return strings.Join(groups, ", ")
	}
	fmt.Printf("authored: %s\nsnapshot: %s\nscim: %s\neffective: %s\n", list(s.Authored), list(s.Snapshot), list(s.SCIM), list(s.Effective))
	if s.SCIMNearMatch != nil {
		fmt.Printf("warning: SCIM has %q, which differs from %q only in case; resolution matches exactly, so its SCIM groups do not apply. Fix the IdP's userName mapping.\n",
			*s.SCIMNearMatch, a.positional[0])
	}
	return nil
}

// orUnknownSource reports a snapshot's source, or a placeholder when the
// IdP export left it blank, so the CLI's output is never an empty field.
func orUnknownSource(s string) string {
	if s == "" {
		return "(unnamed source)"
	}
	return s
}

// orNoneString reports who applied a snapshot, or a placeholder when the
// server has none recorded, so the CLI's output is never an empty field.
func orNoneString(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
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
