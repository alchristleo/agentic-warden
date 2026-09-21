package sync_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/sync"
)

func TestMachineRoundTripsAndIsPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	want := sync.Machine{Server: "http://awd", MachineID: "m1", Credential: "secret", Agents: []string{"claude"}}
	if err := sync.SaveMachine(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := sync.LoadMachine(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, sync.MachineFile))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("machine.json mode = %o, want 0600: it carries the credential", info.Mode().Perm())
		}
	}
}

func TestSaveMachineLeavesTheStateDirDeveloperReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix file modes")
	}
	dir := filepath.Join(t.TempDir(), "state")
	if err := sync.SaveMachine(dir, sync.Machine{Server: "http://awd", MachineID: "m1", Credential: "secret", Agents: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("state dir mode = %o, want 0755: state.json and the audit log inside it are world-readable even though machine.json is not", info.Mode().Perm())
	}
}

func TestLoadMachineWithoutEnrollmentIsErrNotEnrolled(t *testing.T) {
	_, err := sync.LoadMachine(t.TempDir())
	if !errors.Is(err, sync.ErrNotEnrolled) {
		t.Errorf("err = %v, want ErrNotEnrolled", err)
	}
}

func TestLoadMachineRejectsAnIncompleteFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, sync.MachineFile), []byte(`{"server":"http://awd"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := sync.LoadMachine(dir)
	if err == nil || errors.Is(err, sync.ErrNotEnrolled) {
		t.Errorf("err = %v, want a validation error that is not ErrNotEnrolled", err)
	}
}

func TestStateRoundTripsAndIsWorldReadable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	want := sync.State{
		Server:    "http://awd",
		MachineID: "m1",
		Agents:    []string{"claude"},
		ETag:      `"abc"`,
		Version:   "v1",
		SyncedAt:  time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC),
		Files:     map[string]string{"/etc/claude-code/aw-bundle.json": "deadbeef"},
		Error:     "",
		Notes:     []string{"a note"},
	}
	if err := sync.SaveState(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := sync.LoadState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, sync.StateFile))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("state.json mode = %o, want 0644: aw doctor reads it as the developer", info.Mode().Perm())
		}
	}
}

func TestLoadStateWithoutAFileIsTheZeroState(t *testing.T) {
	got, err := sync.LoadState(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, sync.State{}) {
		t.Errorf("got %+v, want the zero State", got)
	}
}

func TestLoadStateReportsACorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, sync.StateFile), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sync.LoadState(dir); err == nil {
		t.Error("want an error for a corrupt state file")
	}
}

func TestSaveStateWritesEmptyCollectionsAsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := sync.SaveState(dir, sync.State{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, sync.StateFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"files": {}`, `"notes": []`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("state.json lacks %s:\n%s", want, raw)
		}
	}
}
