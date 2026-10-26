package claude

import (
	"bytes"
	"encoding/json"
	"os"
)

// PolicyConfigFile is aw-policy's optional configuration in the system
// directory, beside BundleFile.
const PolicyConfigFile = "aw-policy.json"

// FileConfig is the deployed aw-policy configuration file.
type FileConfig struct {
	// RequireBundle makes the helper exit non-zero, so Claude Code refuses
	// to start, when no usable bundle is on the machine. Absent or null:
	// closed once a key is pinned, open otherwise. False opts out.
	RequireBundle *bool `json:"requireBundle"`
}

// ReadConfig reads the file at path with a strict parse: an unknown key is
// an error. A missing file returns the os error, so callers can tell it
// apart with errors.Is(err, os.ErrNotExist).
func ReadConfig(path string) (FileConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return FileConfig{}, err
	}
	var cfg FileConfig
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return FileConfig{}, err
	}
	return cfg, nil
}
