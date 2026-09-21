// Command aw-policy is the policyHelper executable: Claude Code runs it at
// startup with no arguments and applies the managed settings it prints.
//
// It is deployed alongside a managed-settings file that names it:
//
//	{"policyHelper": {"path": "/usr/local/bin/aw-policy", "timeoutMs": 5000}}
//
// and reads its own configuration from aw-policy.json in the same system
// directory as that file, so both are delivered by the same MDM push:
//
//	{"serverUrl": "https://awd.example.com", "groups": ["platform"]}
//
// The contract with Claude Code is strict: a non-zero exit, a timeout, or a
// schema violation in the output refuses the launch. This program therefore
// always exits 0 with a valid envelope, serving its cache when the control
// plane cannot be reached, unless the organization opts into failing closed
// with "requireFresh". See internal/policyhelper for the rules.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/acme/agent-wrapper/internal/policyhelper"
)

// defaultServerURL can be baked in at build time so that a configuration
// file is optional:
//
//	go build -ldflags "-X main.defaultServerURL=https://awd.example.com" ./cmd/aw-policy
var defaultServerURL = ""

// configEnv names the configuration file explicitly. It exists for tests and
// for trying the helper by hand; a deployment relies on the system path.
const configEnv = "AW_POLICY_CONFIG"

// maxStderr bounds what is written to stderr. Claude Code fails the run past
// 1 MiB, and it shows stderr as the reason when the helper exits non-zero,
// so short is also more useful.
const maxStderr = 16 << 10

// fileConfig is the deployed configuration file.
type fileConfig struct {
	ServerURL    string   `json:"serverUrl"`
	Groups       []string `json:"groups"`
	TimeoutMs    int      `json:"timeoutMs"`
	RequireFresh bool     `json:"requireFresh"`
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
	cfg.ClaudeCodeVersion = getenv("CLAUDE_CODE_VERSION")

	result := policyhelper.Run(context.Background(), cfg)
	notes = append(notes, result.Notes...)
	if _, err := stdout.Write(result.Output); err != nil {
		notes = append(notes, "writing stdout: "+err.Error())
	}
	return result.ExitCode
}

// loadConfig reads the deployed file and turns it into a helper Config.
// Problems are notes, not errors: the helper runs on to serve its cache.
func loadConfig(getenv func(string) string) (policyhelper.Config, []string) {
	var notes []string
	cfg := policyhelper.Config{ServerURL: defaultServerURL}

	path := getenv(configEnv)
	if path == "" {
		path = filepath.Join(systemDir(), "aw-policy.json")
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
		if file.ServerURL != "" {
			cfg.ServerURL = file.ServerURL
		}
		cfg.Groups = file.Groups
		cfg.Timeout = time.Duration(file.TimeoutMs) * time.Millisecond
		cfg.RequireFresh = file.RequireFresh
	case errors.Is(err, os.ErrNotExist) && cfg.ServerURL != "":
		// A baked-in URL makes the file optional.
	default:
		notes = append(notes, fmt.Sprintf("configuration %s: %v", path, err))
	}

	if dir, err := os.UserCacheDir(); err == nil {
		cfg.CacheDir = filepath.Join(dir, "agent-wrapper", "aw-policy")
	} else {
		notes = append(notes, "no cache directory: "+err.Error())
	}
	return cfg, notes
}

// systemDir is where Claude Code reads its managed settings file on this
// OS, and so where the helper's configuration is deployed beside it.
func systemDir() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/ClaudeCode"
	case "windows":
		return `C:\Program Files\ClaudeCode`
	default:
		return "/etc/claude-code"
	}
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
