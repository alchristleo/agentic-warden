//go:build unix

package agent

import (
	"fmt"
	"os"
	"syscall"
)

// Exec replaces the current process with the agent. On success it does not
// return: the agent inherits this process's file descriptors and terminal, so
// signals, job control and the exit code are the agent's own rather than
// something the wrapper has to forward.
func (l *Launch) Exec(_ ExecOptions) error {
	env := l.Env
	if env == nil {
		env = os.Environ()
	}
	argv := append([]string{l.Binary}, l.Args...)
	if err := syscall.Exec(l.Binary, argv, env); err != nil {
		return fmt.Errorf("agent: executing %s: %w", l.Binary, err)
	}
	return nil // unreachable: a successful Exec never returns
}
