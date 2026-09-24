// Package timer renders the units that run `aw-sync once` periodically on
// each supported OS, and installs or removes them. The templates embedded
// here are the one source for both `aw-sync install-timer` and the files
// shipped under deploy/aw-sync/, which a golden test keeps identical.
package timer

import (
	"bytes"
	"embed"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/acme/agent-wrapper/internal/sync"
)

//go:embed units/*
var unitFS embed.FS

// Where the units live and what they are called.
const (
	ServicePath = "/etc/systemd/system/aw-sync.service"
	TimerPath   = "/etc/systemd/system/aw-sync.timer"
	TimerUnit   = "aw-sync.timer"
	PlistPath   = "/Library/LaunchDaemons/com.agent-wrapper.aw-sync.plist"
	Label       = "com.agent-wrapper.aw-sync"
	LogDir      = "/Library/Logs/agent-wrapper"
	TaskName    = `agent-wrapper\aw-sync`
)

// Interval bounds. Task Scheduler repeats in whole minutes, so every OS
// takes the same rule rather than Windows alone rejecting 90s.
const (
	DefaultInterval = 5 * time.Minute
	MinInterval     = time.Minute
	MaxInterval     = 24 * time.Hour
)

// Params is what a unit needs: which binary to run, which state directory
// to pass it, and how often.
type Params struct {
	Binary   string
	StateDir string
	Interval time.Duration
}

// Unit is one rendered file. Name is its file name under
// deploy/aw-sync/<DeployDir>/; Path is where install-timer writes it, empty
// on Windows, where the task is registered from a temporary file.
type Unit struct {
	Name    string
	Path    string
	Content []byte
}

// Default is what the deploy files are rendered with: the conventional
// binary location, the OS state directory and a five-minute interval.
func Default(goos string) Params {
	binary := "/usr/local/bin/aw-sync"
	if goos == "windows" {
		binary = `C:\Program Files\AgentWrapper\aw-sync.exe`
	}
	return Params{Binary: binary, StateDir: sync.StateDir(goos), Interval: DefaultInterval}
}

// ValidateInterval reports whether d can be scheduled on every OS.
func ValidateInterval(d time.Duration) error {
	if d < MinInterval || d > MaxInterval {
		return fmt.Errorf("--interval must be between 1m and 24h, got %s", d)
	}
	if d%time.Minute != 0 {
		return fmt.Errorf("--interval must be a whole number of minutes, got %s", d)
	}
	return nil
}

// Supported reports whether install-timer knows goos's scheduler.
func Supported(goos string) error {
	switch goos {
	case "linux", "darwin", "windows":
		return nil
	}
	return fmt.Errorf("install-timer supports linux, darwin and windows, not %s", goos)
}

// DeployDir is the directory under deploy/aw-sync/ holding goos's units.
func DeployDir(goos string) string {
	switch goos {
	case "darwin":
		return "launchd"
	case "windows":
		return "windows"
	default:
		return "systemd"
	}
}

var templates = template.Must(template.New("units").Funcs(template.FuncMap{"xml": xmlEscape}).ParseFS(unitFS, "units/*"))

// Render fills goos's templates with p. It never touches the filesystem.
func Render(goos string, p Params) ([]Unit, error) {
	if err := Supported(goos); err != nil {
		return nil, err
	}
	if err := ValidateInterval(p.Interval); err != nil {
		return nil, err
	}
	if p.Binary == "" {
		return nil, errors.New("timer: no binary to run")
	}
	// The default state directory is left implicit, so the deploy files
	// read exactly as an operator would write them by hand.
	args := []string{"once"}
	if p.StateDir != "" && p.StateDir != sync.StateDir(goos) {
		args = append(args, "--state-dir", p.StateDir)
	}
	seconds := int(p.Interval / time.Second)
	switch goos {
	case "linux":
		data := map[string]any{"ExecStart": systemdCommand(append([]string{p.Binary}, args...)), "Seconds": seconds}
		service, err := execute("aw-sync.service", data)
		if err != nil {
			return nil, err
		}
		timerUnit, err := execute("aw-sync.timer", data)
		if err != nil {
			return nil, err
		}
		return []Unit{
			{Name: "aw-sync.service", Path: ServicePath, Content: service},
			{Name: "aw-sync.timer", Path: TimerPath, Content: timerUnit},
		}, nil
	case "darwin":
		data := map[string]any{"Label": Label, "Args": append([]string{p.Binary}, args...), "Seconds": seconds}
		plist, err := execute("com.agent-wrapper.aw-sync.plist", data)
		if err != nil {
			return nil, err
		}
		return []Unit{{Name: "com.agent-wrapper.aw-sync.plist", Path: PlistPath, Content: plist}}, nil
	default: // windows
		data := map[string]any{"Command": p.Binary, "Arguments": windowsArgs(args), "Minutes": seconds / 60}
		task, err := execute("aw-sync-task.xml", data)
		if err != nil {
			return nil, err
		}
		return []Unit{{Name: "aw-sync-task.xml", Content: task}}, nil
	}
}

func execute(name string, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, fmt.Errorf("timer: rendering %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// systemdCommand joins words into an ExecStart value. systemd expands %
// specifiers and $ variables even inside quotes, so both are doubled;
// words with spaces, quotes or backslashes are double-quoted.
func systemdCommand(words []string) string {
	out := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.ReplaceAll(w, "%", "%%")
		w = strings.ReplaceAll(w, "$", "$$")
		if strings.ContainsAny(w, " \t\"'\\") {
			w = `"` + strings.ReplaceAll(strings.ReplaceAll(w, `\`, `\\`), `"`, `\"`) + `"`
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}

// windowsArgs joins arguments the way Windows' command-line parser splits
// them back: an argument with a space or quote is quoted, inner quotes
// escaped.
func windowsArgs(args []string) string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.ContainsAny(a, " \t\"") {
			a = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		}
		out = append(out, a)
	}
	return strings.Join(out, " ")
}
