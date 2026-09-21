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
	"time"

	"github.com/acme/agent-wrapper/internal/agent/claude/schema"
	"github.com/acme/agent-wrapper/internal/config"
	"github.com/acme/agent-wrapper/internal/handler"
	"github.com/acme/agent-wrapper/internal/policy"
	"github.com/acme/agent-wrapper/internal/store"
)

const usage = `awd is the agent-wrapper control plane.

Usage:
  awd serve                        run the API server
  awd apply <file> [--url URL]     store a new policy revision
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
	h.ManagedValidator = schema.ForAgent
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
	ruleSet, err := policy.LoadRuleSet(path, schema.ForAgent)
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
