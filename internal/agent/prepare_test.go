package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/acme/agent-wrapper/internal/agent"
)

func TestPrepareReturnsTheLaunchTheAdapterComputed(t *testing.T) {
	var reg agent.Registry
	if err := reg.Register(stubAdapter{name: "claude"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	launch, err := agent.Prepare(context.Background(), &reg, agent.Options{Agent: "claude"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if launch.Binary != "/usr/bin/claude" {
		t.Errorf("Binary = %q, want %q", launch.Binary, "/usr/bin/claude")
	}
}

func TestPrepareStampsTheAgentNameOnTheLaunch(t *testing.T) {
	var reg agent.Registry
	if err := reg.Register(stubAdapter{name: "claude"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	launch, err := agent.Prepare(context.Background(), &reg, agent.Options{Agent: "claude"})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	if launch.Agent != "claude" {
		t.Errorf("Agent = %q, want %q", launch.Agent, "claude")
	}
}

func TestPrepareWithoutAnAgentNameIsAnError(t *testing.T) {
	var reg agent.Registry

	if _, err := agent.Prepare(context.Background(), &reg, agent.Options{}); err == nil {
		t.Error("Prepare() error = nil, want an error when no agent is named")
	}
}

func TestPrepareOfAnUnregisteredAgentReportsThat(t *testing.T) {
	var reg agent.Registry

	_, err := agent.Prepare(context.Background(), &reg, agent.Options{Agent: "codex"})

	if !errors.Is(err, agent.ErrNoAdapter) {
		t.Errorf("Prepare() error = %v, want it to wrap ErrNoAdapter", err)
	}
}

// failingAdapter reports that the agent is not installed.
type failingAdapter struct{ stubAdapter }

var errNotInstalled = errors.New("not installed")

func (failingAdapter) Build(context.Context, agent.BuildOptions) (*agent.Launch, error) {
	return nil, errNotInstalled
}

func TestPrepareSurfacesTheAdaptersError(t *testing.T) {
	var reg agent.Registry
	if err := reg.Register(failingAdapter{stubAdapter{name: "claude"}}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err := agent.Prepare(context.Background(), &reg, agent.Options{Agent: "claude"})

	if !errors.Is(err, errNotInstalled) {
		t.Errorf("Prepare() error = %v, want it to wrap the adapter's error", err)
	}
}
