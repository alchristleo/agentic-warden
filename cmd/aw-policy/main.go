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

// configEnv names the configuration file explicitly. It exists for tests and
// for trying the helper by hand; a deployment relies on the system path.
const configEnv = "AW_POLICY_CONFIG"

// bundleEnv names the bundle file explicitly, for the same reasons.
const bundleEnv = "AW_POLICY_BUNDLE"

// maxStderr bounds what is written to stderr. Claude Code fails the run past
// 1 MiB, and it shows stderr as the reason when the helper exits non-zero,
// so short is also more useful.
const maxStderr = 16 << 10

// fileConfig is the deployed configuration file.
type fileConfig struct {
	RequireBundle bool `json:"requireBundle"`
}

func main() {
	os.Exit(run(os.Stdout, os.Stderr, os.Getenv))
}

// run does everything main would, with the process boundary as parameters.
// It never panics out: a defect here must still let Claude Code start.
func run(stdout, stderr io.Writer, getenv func(string) string) (code int) {
	var notes []string
	defer func() {
		if r := recover(); r != nil {
			notes = append(notes, fmt.Sprintf("internal error: %v", r))
			fmt.Fprint(stdout, "{}\n")
			code = 0
		}
		writeNotes(stderr, notes)
	}()

	cfg, notes := loadConfig(getenv)

	result := policyhelper.Run(cfg)
	notes = append(notes, result.Notes...)
	if _, err := stdout.Write(result.Output); err != nil {
		notes = append(notes, "writing stdout: "+err.Error())
	}
	return result.ExitCode
}

// loadConfig locates the bundle and reads the optional configuration file.
// Problems are notes, not errors: the helper runs on with the defaults.
func loadConfig(getenv func(string) string) (policyhelper.Config, []string) {
	var notes []string
	systemDir := claude.SystemDir(runtime.GOOS)
	cfg := policyhelper.Config{BundlePath: getenv(bundleEnv)}
	if cfg.BundlePath == "" {
		cfg.BundlePath = filepath.Join(systemDir, claude.BundleFile)
	}

	path := getenv(configEnv)
	if path == "" {
		path = filepath.Join(systemDir, "aw-policy.json")
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
