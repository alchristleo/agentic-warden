//go:build !unix

package agent

import (
	"io"
	"os"
	"os/exec"
)

// Exec runs the agent as a child process and waits for it. This is the
// fallback for platforms where a process cannot be replaced in place; the
// child's exit error propagates to the caller, so an exit code still reaches
// the shell.
func (l *Launch) Exec(o ExecOptions) error {
	cmd := exec.Command(l.Binary, l.Args...)
	cmd.Env = l.Env
	cmd.Stdin = stdin(o.Stdin)
	cmd.Stdout = stdout(o.Stdout, os.Stdout)
	cmd.Stderr = stdout(o.Stderr, os.Stderr)
	return cmd.Run()
}

func stdin(r io.Reader) io.Reader {
	if r != nil {
		return r
	}
	return os.Stdin
}

func stdout(w io.Writer, fallback *os.File) io.Writer {
	if w != nil {
		return w
	}
	return fallback
}
