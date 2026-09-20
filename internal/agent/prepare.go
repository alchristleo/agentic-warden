package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// Options configures one launch.
type Options struct {
	// Agent is a registered adapter name, e.g. "claude".
	Agent string
	// Args are the developer's arguments, passed through after any flags the
	// adapter injects.
	Args []string
	// Env is the base environment for the child; nil means os.Environ.
	Env []string
	// Settings is the organization configuration to apply.
	Settings Settings
	// Stdin, Stdout and Stderr are used only where the process cannot be
	// replaced in place. On UNIX the agent inherits this process's streams.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Prepare resolves the adapter and computes the launch without running
// anything. Diagnostics and tests use it directly; Run is Prepare followed by
// Launch.Exec.
func Prepare(ctx context.Context, reg *Registry, o Options) (*Launch, error) {
	if o.Agent == "" {
		return nil, errors.New("agent: no agent specified")
	}
	if reg == nil {
		return nil, errors.New("agent: no registry")
	}
	a, err := reg.Lookup(o.Agent)
	if err != nil {
		return nil, err
	}
	launch, err := a.Build(ctx, BuildOptions{
		Args:     o.Args,
		Env:      o.Env,
		Settings: o.Settings,
	})
	if err != nil {
		return nil, fmt.Errorf("agent %s: %w", o.Agent, err)
	}
	launch.Agent = a.Name()
	return launch, nil
}

// Run prepares the launch and starts the agent. On UNIX this replaces the
// current process, so on success it does not return and the agent owns signal
// handling, exit codes and terminal state.
func Run(ctx context.Context, reg *Registry, o Options) error {
	launch, err := Prepare(ctx, reg, o)
	if err != nil {
		return err
	}
	return launch.Exec(ExecOptions{
		Stdin:  o.Stdin,
		Stdout: o.Stdout,
		Stderr: o.Stderr,
	})
}

// ExecOptions carries the streams used where the process cannot be replaced.
type ExecOptions struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}
