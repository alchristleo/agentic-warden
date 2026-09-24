package agent

import "os"

// ProgramData is Windows' machine-wide data directory, where aw-sync and the
// adapters that render system-tier files put them. An installation can
// relocate it, so the environment says where; every caller on Windows shares
// this one lookup instead of copying the fallback.
func ProgramData() string {
	if dir := os.Getenv("ProgramData"); dir != "" {
		return dir
	}
	return `C:\ProgramData`
}
