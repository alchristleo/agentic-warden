package policyhelper

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/acme/agent-wrapper/internal/policy"
)

// auditRotateAt is the log size past which the current log is moved aside.
// One line per launch stays far below it for a long time.
const auditRotateAt = 1 << 20

// auditEntry is one line of the local audit log: enough to reconstruct what
// a session was governed by and why, without the control plane.
type auditEntry struct {
	Time     time.Time `json:"time"`
	Source   Source    `json:"source"`
	Version  string    `json:"version,omitempty"`
	Groups   []string  `json:"groups,omitempty"`
	Repo     string    `json:"repo,omitempty"`
	ExitCode int       `json:"exitCode"`
	Notes    []string  `json:"notes,omitempty"`
}

// audit appends one line to the log in the audit directory. It is best
// effort: a log that cannot be written must not affect the launch.
func audit(cfg Config, subject policy.Subject, r Result) {
	if cfg.AuditDir == "" {
		return
	}
	path := filepath.Join(cfg.AuditDir, "aw-policy.log")
	if info, err := os.Stat(path); err == nil && info.Size() > auditRotateAt {
		_ = os.Rename(path, path+".1")
	}
	if err := os.MkdirAll(cfg.AuditDir, 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	line, err := json.Marshal(auditEntry{
		Time:     cfg.Now().UTC(),
		Source:   r.Source,
		Version:  r.Version,
		Groups:   subject.Groups,
		Repo:     subject.Repo,
		ExitCode: r.ExitCode,
		Notes:    r.Notes,
	})
	if err != nil {
		return
	}
	_, _ = f.Write(append(line, '\n'))
}
