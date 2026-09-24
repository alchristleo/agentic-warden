package timer_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/acme/agent-wrapper/internal/sync/timer"
)

// recorder stands in for the OS tools: it records every command and fails
// the ones listed in fail, printing output[cmd] if set and "tool said no"
// otherwise.
type recorder struct {
	calls  []string
	fail   map[string]bool
	output map[string]string
	seen   map[string][]byte // a copy of any file named by schtasks /XML
}

func (r *recorder) run(_ context.Context, name string, args ...string) ([]byte, error) {
	cmd := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, cmd)
	if name == "schtasks" && len(args) > 4 && args[0] == "/Create" {
		raw, _ := os.ReadFile(args[4])
		if r.seen == nil {
			r.seen = map[string][]byte{}
		}
		r.seen["xml"] = raw
	}
	if r.fail[cmd] {
		if out, ok := r.output[cmd]; ok {
			return []byte(out), errors.New("exit status 1")
		}
		return []byte("tool said no"), errors.New("exit status 1")
	}
	return nil, nil
}

func installer(t *testing.T, goos string, r *recorder) timer.Installer {
	t.Helper()
	return timer.Installer{GOOS: goos, Run: r.run, Root: t.TempDir(), TempDir: t.TempDir()}
}

var params = timer.Params{Binary: "/usr/local/bin/aw-sync", Interval: 5 * time.Minute}

func equalCalls(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("commands:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestInstallLinux(t *testing.T) {
	r := &recorder{}
	in := installer(t, "linux", r)
	if err := in.Install(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, r.calls, []string{"systemctl daemon-reload", "systemctl enable --now aw-sync.timer"})
	for _, p := range []string{timer.ServicePath, timer.TimerPath} {
		info, err := os.Stat(filepath.Join(in.Root, p))
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("%s mode %v, want 0644", p, info.Mode().Perm())
		}
	}
	// Re-installing is how the interval changes: same commands, new file.
	r.calls = nil
	if err := in.Install(context.Background(), timer.Params{Binary: params.Binary, Interval: 10 * time.Minute}); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, r.calls, []string{"systemctl daemon-reload", "systemctl enable --now aw-sync.timer"})
	raw, _ := os.ReadFile(filepath.Join(in.Root, timer.TimerPath))
	if !strings.Contains(string(raw), "OnUnitActiveSec=600s") {
		t.Errorf("re-install did not rewrite the timer:\n%s", raw)
	}
}

func TestInstallLinuxReportsAFailingToolAndKeepsTheUnits(t *testing.T) {
	r := &recorder{fail: map[string]bool{"systemctl enable --now aw-sync.timer": true}}
	in := installer(t, "linux", r)
	err := in.Install(context.Background(), params)
	var cmdErr *timer.CommandError
	if !errors.As(err, &cmdErr) || !strings.Contains(err.Error(), "systemctl enable --now aw-sync.timer") || !strings.Contains(err.Error(), "tool said no") {
		t.Fatalf("err = %v; want the command and its output", err)
	}
	if _, err := os.Stat(filepath.Join(in.Root, timer.ServicePath)); err != nil {
		t.Errorf("unit files must stay after a tool failure so a re-run is safe: %v", err)
	}
}

func TestInstallDarwinNotLoaded(t *testing.T) {
	r := &recorder{fail: map[string]bool{"launchctl print system/com.agent-wrapper.aw-sync": true}}
	in := installer(t, "darwin", r)
	if err := in.Install(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, r.calls, []string{
		"launchctl print system/com.agent-wrapper.aw-sync",
		"launchctl bootstrap system " + filepath.Join(in.Root, timer.PlistPath),
	})
	if info, err := os.Stat(filepath.Join(in.Root, timer.LogDir)); err != nil || !info.IsDir() {
		t.Errorf("log dir: %v", err)
	}
}

func TestInstallDarwinLoadedBootsOutFirst(t *testing.T) {
	r := &recorder{}
	in := installer(t, "darwin", r)
	if err := in.Install(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, r.calls, []string{
		"launchctl print system/com.agent-wrapper.aw-sync",
		"launchctl bootout system/com.agent-wrapper.aw-sync",
		"launchctl bootstrap system " + filepath.Join(in.Root, timer.PlistPath),
	})
}

func TestInstallWindowsStagesUTF16AndCleansUp(t *testing.T) {
	r := &recorder{}
	in := installer(t, "windows", r)
	if err := in.Install(context.Background(), timer.Params{Binary: `C:\aw\aw-sync.exe`, Interval: 5 * time.Minute}); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) != 1 || !strings.HasPrefix(r.calls[0], `schtasks /Create /TN agent-wrapper\aw-sync /XML `) || !strings.HasSuffix(r.calls[0], " /F") {
		t.Fatalf("commands: %v", r.calls)
	}
	raw := r.seen["xml"]
	if len(raw) < 2 || raw[0] != 0xFF || raw[1] != 0xFE {
		t.Fatalf("staged task XML must start with a UTF-16LE BOM, got % x", raw[:min(4, len(raw))])
	}
	units := make([]uint16, 0, len(raw)/2)
	for i := 2; i+1 < len(raw); i += 2 {
		units = append(units, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	text := string(utf16.Decode(units))
	if !strings.HasPrefix(text, `<?xml version="1.0" encoding="UTF-16"?>`) || !strings.Contains(text, `<Command>C:\aw\aw-sync.exe</Command>`) {
		t.Errorf("decoded task XML:\n%s", text)
	}
	left, _ := os.ReadDir(in.TempDir)
	if len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestUninstallLinux(t *testing.T) {
	r := &recorder{}
	in := installer(t, "linux", r)
	if err := in.Install(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	r.calls = nil
	if err := in.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, r.calls, []string{"systemctl disable --now aw-sync.timer", "systemctl daemon-reload"})
	for _, p := range []string{timer.ServicePath, timer.TimerPath} {
		if _, err := os.Stat(filepath.Join(in.Root, p)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still there: %v", p, err)
		}
	}
}

func TestUninstallLinuxRemovesFilesEvenWhenDisableFails(t *testing.T) {
	r := &recorder{fail: map[string]bool{"systemctl disable --now aw-sync.timer": true}}
	in := installer(t, "linux", r)
	if err := in.Install(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	err := in.Uninstall(context.Background())
	if err == nil || !strings.Contains(err.Error(), "systemctl disable --now aw-sync.timer") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(in.Root, timer.ServicePath)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("service file kept after a partial failure: %v", err)
	}
}

func TestUninstallWithNothingInstalled(t *testing.T) {
	for goos, r := range map[string]*recorder{
		"linux":  {},
		"darwin": {fail: map[string]bool{"launchctl print system/com.agent-wrapper.aw-sync": true}},
		"windows": {
			fail:   map[string]bool{queryTask: true},
			output: map[string]string{queryTask: "ERROR: The system cannot find the file specified.\r\n"},
		},
	} {
		in := installer(t, goos, r)
		if err := in.Uninstall(context.Background()); !errors.Is(err, timer.ErrNotInstalled) {
			t.Errorf("%s: err = %v, want ErrNotInstalled", goos, err)
		}
		for _, c := range r.calls {
			if !strings.Contains(c, "print") && !strings.Contains(c, "/Query") {
				t.Errorf("%s: ran %q with nothing installed", goos, c)
			}
		}
	}
}

func TestUninstallDarwinAndWindows(t *testing.T) {
	r := &recorder{}
	in := installer(t, "darwin", r)
	if err := in.Install(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	r.calls = nil
	if err := in.Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, r.calls, []string{"launchctl print system/com.agent-wrapper.aw-sync", "launchctl bootout system/com.agent-wrapper.aw-sync"})
	if _, err := os.Stat(filepath.Join(in.Root, timer.PlistPath)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("plist kept: %v", err)
	}

	w := &recorder{}
	if err := installer(t, "windows", w).Uninstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	equalCalls(t, w.calls, []string{`schtasks /Query /TN agent-wrapper\aw-sync`, `schtasks /Delete /TN agent-wrapper\aw-sync /F`})
}

func TestInstallRejectsBadParamsBeforeWriting(t *testing.T) {
	r := &recorder{}
	in := installer(t, "linux", r)
	if err := in.Install(context.Background(), timer.Params{Binary: "/b", Interval: 30 * time.Second}); err == nil {
		t.Fatal("30s accepted")
	}
	if len(r.calls) != 0 {
		t.Errorf("ran %v", r.calls)
	}
	if entries, _ := os.ReadDir(in.Root); len(entries) != 0 {
		t.Errorf("wrote %v", entries)
	}
}

const queryTask = `schtasks /Query /TN agent-wrapper\aw-sync`

// A non-elevated query of a task an administrator created fails for access,
// not absence; saying "no timer installed" then would leave the task
// running while claiming it is gone.
func TestUninstallWindowsReportsAQueryItCannotRun(t *testing.T) {
	r := &recorder{
		fail:   map[string]bool{queryTask: true},
		output: map[string]string{queryTask: "ERROR: Access is denied.\r\n"},
	}
	err := installer(t, "windows", r).Uninstall(context.Background())
	var cmdErr *timer.CommandError
	if errors.Is(err, timer.ErrNotInstalled) || !errors.As(err, &cmdErr) || !strings.Contains(err.Error(), queryTask) || !strings.Contains(err.Error(), "Access is denied") {
		t.Fatalf("err = %v; want a CommandError naming the query and its output", err)
	}
	equalCalls(t, r.calls, []string{queryTask})
}
