// Command aw-policy is the policyHelper executable: Claude Code runs it at
// startup with no arguments and applies the managed settings it prints.
//
// It is offline. aw-sync enrolls the machine and keeps the user's bundle in
// Claude Code's system directory as aw-bundle.json, beside the drop-in that
// names this helper:
//
//	{"policyHelper": {"path": "/usr/local/bin/aw-policy", "timeoutMs": 5000}}
//
// The helper reads the bundle, resolves the repository the session runs in
// and prints the settings that apply. Its own configuration, aw-policy.json
// in the same directory, is optional and has one key:
//
//	{"requireBundle": false}
//
// The contract with Claude Code is strict: a non-zero exit, a timeout, or a
// schema violation in the output refuses the launch. This program therefore
// always exits 0 with a valid envelope, printing an empty one when there is
// no usable bundle, unless the organization opts into failing closed with
// "requireBundle". See internal/policyhelper for the rules.
//
// The one argument it recognises is "build-info", which prints whether the
// AW_POLICY_BUNDLE and AW_POLICY_CONFIG overrides are compiled in; they are
// only under -tags awtest, for tests. Any other argument runs the helper as
// usual, so a future Claude Code that passes one cannot break a launch.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/acme/agent-wrapper/internal/agent/claude"
	"github.com/acme/agent-wrapper/internal/policyhelper"
)

// configEnv names the configuration file explicitly. It is honoured only
// when envOverrides is true, for tests; a deployment reads the system path.
const configEnv = "AW_POLICY_CONFIG"

// bundleEnv names the bundle file explicitly, under the same rule.
const bundleEnv = "AW_POLICY_BUNDLE"

// buildInfoArg is the one argument the helper answers instead of running.
const buildInfoArg = "build-info"

// maxStderr bounds what is written to stderr. Claude Code fails the run past
// 1 MiB, and it shows stderr as the reason when the helper exits non-zero,
// so short is also more useful.
const maxStderr = 16 << 10

// fileConfig is the deployed configuration file.
type fileConfig struct {
	// RequireBundle makes the helper exit non-zero, so Claude Code refuses
	// to start, when no usable bundle is on the machine. Off by default:
	// failing safe keeps developers working.
	RequireBundle bool `json:"requireBundle"`
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == buildInfoArg {
		fmt.Fprintln(os.Stdout, buildInfo(envOverrides))
		os.Exit(0)
	}
	os.Exit(run(os.Stdout, os.Stderr, os.Getenv, envOverrides))
}

// buildInfo is what `aw-policy build-info` prints: one line `aw doctor`
// parses to tell a test build from a release build.
func buildInfo(overrides bool) string {
	if overrides {
		return "env-overrides=on"
	}
	return "env-overrides=off"
}

// run does everything main would, with the process boundary as parameters.
// It never panics out: a defect here must still let Claude Code start.
func run(stdout, stderr io.Writer, getenv func(string) string, allowEnv bool) (code int) {
	var notes []string
	defer func() {
		if r := recover(); r != nil {
			notes = append(notes, fmt.Sprintf("internal error: %v", r))
			fmt.Fprint(stdout, "{}\n")
			code = 0
		}
		writeNotes(stderr, notes)
	}()

	cfg, notes := loadConfig(getenv, allowEnv)

	result := policyhelper.Run(cfg)
	notes = append(notes, result.Notes...)
	if _, err := stdout.Write(result.Output); err != nil {
		notes = append(notes, "writing stdout: "+err.Error())
	}
	return result.ExitCode
}

// loadConfig locates the bundle and reads the optional configuration file.
// Problems are notes, not errors: the helper runs on with the defaults.
// allowEnv is false in a release build: the two environment variables are
// then not consulted at all, and a set one earns a note so the developer
// who set it learns it did nothing rather than wondering why.
func loadConfig(getenv func(string) string, allowEnv bool) (policyhelper.Config, []string) {
	var notes []string
	systemDir := claude.SystemDir(runtime.GOOS)
	cfg := policyhelper.Config{BundlePath: filepath.Join(systemDir, claude.BundleFile)}
	path := filepath.Join(systemDir, "aw-policy.json")
	if allowEnv {
		if p := getenv(bundleEnv); p != "" {
			cfg.BundlePath = p
		}
		if p := getenv(configEnv); p != "" {
			path = p
		}
	} else {
		for _, name := range []string{bundleEnv, configEnv} {
			if getenv(name) != "" {
				notes = append(notes, fmt.Sprintf("%s is set but this build ignores it; the bundle and configuration are read from %s only", name, systemDir))
			}
		}
	}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		var file fileConfig
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&file); err != nil {
			notes = append(notes, fmt.Sprintf("configuration %s is invalid: %v", path, err))
			break
		}
		cfg.RequireBundle = file.RequireBundle
	case errors.Is(err, os.ErrNotExist):
		// The file is optional: its one key defaults to failing safe.
	default:
		notes = append(notes, fmt.Sprintf("configuration %s: %v", path, err))
	}

	if dir, err := os.UserCacheDir(); err == nil {
		cfg.AuditDir = filepath.Join(dir, "agent-wrapper", "aw-policy")
	} else {
		notes = append(notes, "no audit directory: "+err.Error())
	}
	return cfg, notes
}

func writeNotes(stderr io.Writer, notes []string) {
	if len(notes) == 0 {
		return
	}
	text := "aw-policy: " + strings.Join(notes, "\naw-policy: ") + "\n"
	if len(text) > maxStderr {
		text = text[:maxStderr]
	}
	_, _ = io.WriteString(stderr, text)
}
