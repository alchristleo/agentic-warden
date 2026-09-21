package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/sync"
)

func TestSyncSectionReadsAwSyncState(t *testing.T) {
	dir := t.TempDir()
	if err := sync.SaveMachine(dir, sync.Machine{Server: "http://awd", MachineID: "m1", Credential: "secret", Agents: []string{"claude"}}); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(t.TempDir(), "aw-bundle.json")
	if err := os.WriteFile(bundle, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := sync.State{
		Server: "http://awd", MachineID: "m1", Agents: []string{"claude"},
		Version:  "v1",
		SyncedAt: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC),
		Files:    map[string]string{bundle: "not-the-hash-on-disk"},
		Error:    "boom",
	}
	if err := sync.SaveState(dir, state); err != nil {
		t.Fatal(err)
	}

	r, errText := syncSection(dir)

	if errText != "" {
		t.Fatalf("syncSection error = %q", errText)
	}
	if !r.Enrolled || r.MachineID != "m1" || r.Version != "v1" || r.Error != "boom" || !r.SyncedAt.Equal(state.SyncedAt) {
		t.Errorf("report = %+v; want the enrollment and last-cycle facts from state.json", r)
	}
	if !r.Drift || len(r.Files) != 1 || r.Files[0].State != "drift" {
		t.Errorf("report = %+v; want the tampered bundle reported as drift", r)
	}
}

func TestSyncSectionWithoutAStateDirectoryIsNotEnrolled(t *testing.T) {
	r, errText := syncSection(filepath.Join(t.TempDir(), "absent"))

	if errText != "" || r == nil || r.Enrolled {
		t.Errorf("report = %+v error = %q; a machine without aw-sync is not enrolled, and that is not an error", r, errText)
	}
}

func TestSyncSectionReportsAnUnreadableState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, errText := syncSection(dir)

	if r != nil || errText == "" {
		t.Errorf("report = %+v error = %q; a broken state.json is worth telling the developer about", r, errText)
	}
}

func TestAgeIsHumanReadable(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	cases := map[time.Time]string{
		now.Add(-30 * time.Second): "30s ago",
		now.Add(-5 * time.Minute):  "5m0s ago",
		now.Add(-3 * time.Hour):    "3h0m0s ago",
		now.Add(-49 * time.Hour):   "2d1h ago",
		{}:                         "never",
	}
	for at, want := range cases {
		if got := age(at, now); got != want {
			t.Errorf("age(%v) = %q, want %q", at, got, want)
		}
	}
}
