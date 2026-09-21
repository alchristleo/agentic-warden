package sync_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent/claude"
	"github.com/acme/agent-wrapper/internal/sync"
)

func synced(t *testing.T) (sync.Config, string) {
	t.Helper()
	f := newFakeAwd(t, testBundle())
	cfg, root := enrolled(t, f, claudeRegistry(t), "claude")
	if res := sync.Run(context.Background(), cfg); res.Err != nil {
		t.Fatal(res.Err)
	}
	return cfg, root
}

func fileStates(r sync.Report) map[string]string {
	out := make(map[string]string, len(r.Files))
	for _, f := range r.Files {
		out[filepath.Base(f.Path)] = f.State
	}
	return out
}

func TestStatusAfterACleanSyncReportsEverythingOK(t *testing.T) {
	cfg, _ := synced(t)
	r, err := sync.Status(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Enrolled || r.MachineID != "m1" || r.Version != "v1" || r.Error != "" || r.Drift {
		t.Errorf("report = %+v", r)
	}
	if !r.SyncedAt.Equal(cfg.Now()) {
		t.Errorf("syncedAt = %v", r.SyncedAt)
	}
	states := fileStates(r)
	if states["aw-bundle.json"] != "ok" || states["50-agent-wrapper.json"] != "ok" {
		t.Errorf("file states = %v", states)
	}
	paths := make([]string, 0, len(r.Files))
	for _, f := range r.Files {
		paths = append(paths, f.Path)
	}
	if !sort.StringsAreSorted(paths) {
		t.Errorf("files are not sorted by path: %v", paths)
	}
}

func TestStatusReportsDrift(t *testing.T) {
	cfg, root := synced(t)
	if err := os.WriteFile(filepath.Join(root, claude.BundleFile), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := sync.Status(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Drift || fileStates(r)["aw-bundle.json"] != "drift" {
		t.Errorf("report = %+v", r)
	}
	for _, f := range r.Files {
		if f.State == "drift" && (f.Actual == "" || f.Actual == f.Expected) {
			t.Errorf("drifted file lacks a distinct actual hash: %+v", f)
		}
	}
}

func TestStatusReportsAMissingFile(t *testing.T) {
	cfg, root := synced(t)
	if err := os.Remove(filepath.Join(root, claude.DropInFile)); err != nil {
		t.Fatal(err)
	}
	r, err := sync.Status(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Drift || fileStates(r)["50-agent-wrapper.json"] != "missing" {
		t.Errorf("report = %+v", r)
	}
}

func TestStatusWithoutEnrollmentIsNotAnError(t *testing.T) {
	r, err := sync.Status(sync.Config{StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if r.Enrolled || r.Agents == nil || r.Files == nil || r.Notes == nil {
		t.Errorf("report = %+v, want Enrolled false and empty, non-nil lists", r)
	}
}

func TestStatusCarriesTheLastError(t *testing.T) {
	cfg, _ := synced(t)
	state, _ := sync.LoadState(cfg.StateDir)
	state.Error = "the control plane answered 500"
	if err := sync.SaveState(cfg.StateDir, state); err != nil {
		t.Fatal(err)
	}
	r, err := sync.Status(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if r.Error != "the control plane answered 500" {
		t.Errorf("error = %q", r.Error)
	}
}

func TestStatusReportsAMalformedEnrollment(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, sync.MachineFile), []byte(`{"server":"http://awd"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := sync.Status(sync.Config{StateDir: dir})
	if err == nil {
		t.Fatal("want an error for a half-enrolled machine")
	}
	if errors.Is(err, sync.ErrNotEnrolled) {
		t.Errorf("err = %v, want an error other than ErrNotEnrolled", err)
	}
}
