package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alchristleo/agentic-warden/internal/sync"
)

func statusWithProblems(t *testing.T, on bool) string {
	t.Helper()
	dir := t.TempDir()
	err := sync.SaveState(dir, sync.State{
		Server: "http://x", MachineID: "m1", Agents: []string{"claude"},
		SyncedAt: time.Unix(0, 0).UTC(), Error: "boom", Notes: []string{"heads up"},
		Files: map[string]string{dir + "/missing.json": "00"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sync.MachineFile), []byte(`{"server":"http://x","machineId":"m1","credential":"c"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := run([]string{"status", "--state-dir", dir}, &out, on); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestStatusColorOn(t *testing.T) {
	got := statusWithProblems(t, true)
	for _, want := range []string{
		"\x1b[31mlast error:\x1b[0m boom",
		"\x1b[2mnote: heads up\x1b[0m",
		"\x1b[33mdrift:\x1b[0m the next",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("colored status lacks %q:\n%q", want, got)
		}
	}
}

func TestStatusColorOff(t *testing.T) {
	got := statusWithProblems(t, false)
	if strings.Contains(got, "\x1b") {
		t.Errorf("status with color off has an escape byte:\n%q", got)
	}
	for _, want := range []string{"last error: boom\n", "note: heads up\n", "drift: the next"} {
		if !strings.Contains(got, want) {
			t.Errorf("plain status lacks %q:\n%q", want, got)
		}
	}
}
