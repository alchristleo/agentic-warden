package timer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/acme/agent-wrapper/internal/cache"
)

// Runner runs one OS tool and returns its combined output. Tests replace
// it so no test ever calls systemctl, launchctl or schtasks.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// ExecRunner runs the real tool.
func ExecRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// ErrNotInstalled means there is no timer to remove.
var ErrNotInstalled = errors.New("no timer installed")

// CommandError is an OS tool that failed. It carries the command line and
// what the tool printed, which is what an MDM log needs to diagnose it.
type CommandError struct {
	// Command is the command line that failed, space-joined.
	Command string
	// Output is what the tool printed, stdout and stderr combined.
	Output string
	// Err is how it failed, usually an *exec.ExitError.
	Err error
}

// Error names the command, how it failed and what it printed.
func (e *CommandError) Error() string {
	msg := fmt.Sprintf("running `%s`: %v", e.Command, e.Err)
	if out := strings.TrimSpace(e.Output); out != "" {
		msg += ": " + out
	}
	return msg
}

// Unwrap returns Err, so errors.Is and errors.As see the underlying failure.
func (e *CommandError) Unwrap() error { return e.Err }

// Installer installs and removes the timer on one OS.
type Installer struct {
	// GOOS picks the scheduler: linux, darwin or windows.
	GOOS string
	// Run runs the OS tools; NewInstaller sets ExecRunner.
	Run Runner
	// Root prefixes every file path written or removed; empty means the
	// real filesystem root. Tests point it at a temporary directory.
	Root string
	// TempDir is where Windows stages the task XML; empty means os.TempDir().
	TempDir string
}

// NewInstaller is the Installer for goos that runs the real tools.
func NewInstaller(goos string) Installer {
	return Installer{GOOS: goos, Run: ExecRunner}
}

func (in Installer) path(p string) string {
	if in.Root == "" {
		return p
	}
	return filepath.Join(in.Root, filepath.FromSlash(p))
}

func (in Installer) run(ctx context.Context, name string, args ...string) error {
	out, err := in.Run(ctx, name, args...)
	if err != nil {
		return &CommandError{Command: strings.Join(append([]string{name}, args...), " "), Output: string(out), Err: err}
	}
	return nil
}

// loaded asks launchd whether the job is loaded: `launchctl print` exits 0
// only for a job it knows.
func (in Installer) loaded(ctx context.Context) bool {
	_, err := in.Run(ctx, "launchctl", "print", "system/"+Label)
	return err == nil
}

// Install writes goos's units and loads them. Re-running it overwrites and
// reloads, which is how the interval or binary path changes. When a tool
// fails after the units are written, the units stay: running Install again
// is safe.
func (in Installer) Install(ctx context.Context, p Params) error {
	units, err := Render(in.GOOS, p)
	if err != nil {
		return err
	}
	switch in.GOOS {
	case "linux":
		if err := in.write(units); err != nil {
			return err
		}
		if err := in.run(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
		return in.run(ctx, "systemctl", "enable", "--now", TimerUnit)
	case "darwin":
		// The plist first: without root, writing /Library/LaunchDaemons
		// fails before /Library/Logs/agent-wrapper can be created under
		// the wrong owner.
		if err := in.write(units); err != nil {
			return err
		}
		if err := os.MkdirAll(in.path(LogDir), 0o755); err != nil {
			return fmt.Errorf("timer: creating %s: %w", in.path(LogDir), err)
		}
		if in.loaded(ctx) {
			if err := in.run(ctx, "launchctl", "bootout", "system/"+Label); err != nil {
				return err
			}
		}
		// A label someone once ran `launchctl disable` on stays disabled
		// across bootstrap; enable clears that, so the job actually runs.
		if err := in.run(ctx, "launchctl", "enable", "system/"+Label); err != nil {
			return err
		}
		return in.run(ctx, "launchctl", "bootstrap", "system", in.path(PlistPath))
	default: // windows; Render has already refused anything else
		return in.register(ctx, units[0].Content)
	}
}

func (in Installer) write(units []Unit) error {
	for _, u := range units {
		path := in.path(u.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("timer: creating %s: %w", filepath.Dir(path), err)
		}
		if err := cache.ReplaceMode(path, u.Content, 0o644); err != nil {
			return fmt.Errorf("timer: writing %s: %w", path, err)
		}
	}
	return nil
}

// register stages the task XML as UTF-16LE with a BOM — the encoding Task
// Scheduler itself exports and schtasks reliably reads — then registers it
// and removes the staged file.
func (in Installer) register(ctx context.Context, content []byte) error {
	f, err := os.CreateTemp(in.TempDir, "aw-sync-task-*.xml")
	if err != nil {
		return fmt.Errorf("timer: staging the task XML: %w", err)
	}
	name := f.Name()
	defer os.Remove(name)
	_, werr := f.Write(utf16LE(content))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("timer: staging the task XML: %w", werr)
	}
	return in.run(ctx, "schtasks", "/Create", "/TN", TaskName, "/XML", name, "/F")
}

func utf16LE(content []byte) []byte {
	text := strings.Replace(string(content), `encoding="UTF-8"`, `encoding="UTF-16"`, 1)
	units := utf16.Encode([]rune(text))
	var buf bytes.Buffer
	buf.Write([]byte{0xFF, 0xFE})
	for _, u := range units {
		buf.WriteByte(byte(u))
		buf.WriteByte(byte(u >> 8))
	}
	return buf.Bytes()
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Uninstall stops and removes the timer. It never touches enrollment,
// state or rendered files. With nothing installed it returns
// ErrNotInstalled; when a tool fails part-way it still removes every file
// it can, so running it again is safe.
func (in Installer) Uninstall(ctx context.Context) error {
	if err := Supported(in.GOOS); err != nil {
		return err
	}
	switch in.GOOS {
	case "linux":
		service, timerFile := in.path(ServicePath), in.path(TimerPath)
		if !exists(service) && !exists(timerFile) {
			return ErrNotInstalled
		}
		var errs []error
		if err := in.run(ctx, "systemctl", "disable", "--now", TimerUnit); err != nil {
			errs = append(errs, err)
		}
		errs = append(errs, remove(timerFile), remove(service))
		if err := in.run(ctx, "systemctl", "daemon-reload"); err != nil {
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	case "darwin":
		plist := in.path(PlistPath)
		loaded := in.loaded(ctx)
		if !loaded && !exists(plist) {
			return ErrNotInstalled
		}
		var errs []error
		if loaded {
			if err := in.run(ctx, "launchctl", "bootout", "system/"+Label); err != nil {
				errs = append(errs, err)
			}
		}
		errs = append(errs, remove(plist))
		return errors.Join(errs...)
	default: // windows
		// A non-elevated /Query of a task an administrator created can fail
		// for access, not absence; only "cannot find" means nothing is
		// installed, and anything else is reported so the caller can say
		// to elevate.
		if err := in.run(ctx, "schtasks", "/Query", "/TN", TaskName); err != nil {
			var cmdErr *CommandError
			if errors.As(err, &cmdErr) && strings.Contains(strings.ToLower(cmdErr.Output), "cannot find") {
				return ErrNotInstalled
			}
			return err
		}
		return in.run(ctx, "schtasks", "/Delete", "/TN", TaskName, "/F")
	}
}

func remove(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("timer: removing %s: %w", path, err)
	}
	return nil
}
