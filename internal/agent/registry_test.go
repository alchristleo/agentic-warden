package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
)

// stubAdapter is a minimal Adapter used to exercise the registry.
type stubAdapter struct{ name string }

func (s stubAdapter) Name() string { return s.name }

func (s stubAdapter) Locate(_ []string) (string, error) { return "/usr/bin/" + s.name, nil }

func (s stubAdapter) Build(_ context.Context, _ agent.BuildOptions) (*agent.Launch, error) {
	return &agent.Launch{Binary: "/usr/bin/" + s.name}, nil
}

func TestRegistryLookupReturnsTheRegisteredAdapter(t *testing.T) {
	var reg agent.Registry
	want := stubAdapter{name: "claude"}
	if err := reg.Register(want); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, err := reg.Lookup("claude")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.Name() != want.Name() {
		t.Errorf("Lookup() = %q, want %q", got.Name(), want.Name())
	}
}

func TestRegistryLookupOfAnUnknownAgentNamesWhatIsAvailable(t *testing.T) {
	var reg agent.Registry
	if err := reg.Register(stubAdapter{name: "claude"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err := reg.Lookup("codex")
	if !errors.Is(err, agent.ErrNoAdapter) {
		t.Fatalf("Lookup() error = %v, want it to wrap ErrNoAdapter", err)
	}
	if !strings.Contains(err.Error(), "claude") {
		t.Errorf("error %q does not name the registered agent", err)
	}
}

func TestRegistryRejectsASecondAdapterForTheSameName(t *testing.T) {
	var reg agent.Registry
	if err := reg.Register(stubAdapter{name: "claude"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := reg.Register(stubAdapter{name: "claude"}); err == nil {
		t.Error("Register() error = nil, want an error for a duplicate name")
	}
}

func TestRegistryNamesAreSorted(t *testing.T) {
	var reg agent.Registry
	for _, name := range []string{"opencode", "claude", "codex"} {
		if err := reg.Register(stubAdapter{name: name}); err != nil {
			t.Fatalf("Register(%q): %v", name, err)
		}
	}

	got := reg.Names()

	want := []string{"claude", "codex", "opencode"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Names() = %v, want %v", got, want)
	}
}

func TestRegistryNamesIsEmptyNotNilWhenNothingIsRegistered(t *testing.T) {
	var reg agent.Registry

	if got := reg.Names(); got == nil {
		t.Error("Names() = nil, want an empty slice")
	}
}
