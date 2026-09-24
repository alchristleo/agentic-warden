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
	// ServicePath is where install-timer writes the systemd service unit.
	ServicePath = "/etc/systemd/system/aw-sync.service"
	// TimerPath is where install-timer writes the systemd timer unit.
	TimerPath = "/etc/systemd/system/aw-sync.timer"
	// TimerUnit is the systemd unit name install-timer enables and starts,
	// and uninstall-timer disables and stops.
	TimerUnit = "aw-sync.timer"
	// PlistPath is where install-timer writes the launchd daemon's plist.
	PlistPath = "/Library/LaunchDaemons/com.agent-wrapper.aw-sync.plist"
	// Label is this job's identity to launchd: the plist's own Label key
	// and what bootout/bootstrap and print address it by.
	Label = "com.agent-wrapper.aw-sync"
	// LogDir is where launchd redirects the job's stdout and stderr.
	LogDir = "/Library/Logs/agent-wrapper"
	// TaskName is the Task Scheduler task's full path, used to create,
	// query and delete it.
	TaskName = `agent-wrapper\aw-sync`
)

// Interval bounds. Task Scheduler repeats in whole minutes, so every OS
// takes the same rule rather than Windows alone rejecting 90s.
const (
	// DefaultInterval is how often the deploy files and a bare
	// install-timer (no --interval) run aw-sync.
	DefaultInterval = 5 * time.Minute
	// MinInterval is the shortest interval ValidateInterval accepts.
	MinInterval = time.Minute
	// MaxInterval is the longest interval ValidateInterval accepts.
	MaxInterval = 24 * time.Hour
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

// windowsArgs joins arguments into a Task Scheduler <Arguments> value using
// the same escaping CommandLineToArgvW expects, so schtasks reconstructs
// exactly the words given.
func windowsArgs(args []string) string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, escapeWindowsArg(a))
	}
	return strings.Join(out, " ")
}

// escapeWindowsArg quotes and escapes one argument the way
// CommandLineToArgvW parses it back apart: a run of backslashes is literal
// unless it immediately precedes a double quote, in which case the run is
// doubled and the quote itself is escaped with one more backslash. Naively
// quoting on space/tab/quote alone, without this rule, corrupts a value
// like `D:\Agent Wrapper\`: its trailing backslash would merge with the
// closing quote this function adds, so the parser reads it as an escaped
// literal quote and never sees the argument end. This mirrors
// syscall.EscapeArg, which is Windows-only in the standard library and so
// cannot be called from code that must also build for linux and darwin.
func escapeWindowsArg(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"\\") {
		return s
	}
	needsBackslash := strings.ContainsAny(s, `"\`)
	hasSpace := strings.ContainsAny(s, " \t")
	if !needsBackslash {
		// Only a space or tab: no embedded quote or backslash to escape.
		return `"` + s + `"`
	}
	var b strings.Builder
	if hasSpace {
		b.WriteByte('"')
	}
	slashes := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			slashes++
		case '"':
			for ; slashes > 0; slashes-- {
				b.WriteByte('\\')
			}
			b.WriteByte('\\')
		default:
			slashes = 0
		}
		b.WriteByte(s[i])
	}
	if hasSpace {
		for ; slashes > 0; slashes-- {
			b.WriteByte('\\')
		}
		b.WriteByte('"')
	}
	return b.String()
}
