// Package requirements validates the document aw-sync writes to Codex's
// requirements.toml. Codex has no published schema for the file, so the
// check is the one the spec names: a vendored, dated allowlist of top-level
// keys, and a TOML round trip so nothing the policy carries is silently
// lost or mangled on the way to disk.
package requirements

import (
	"fmt"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// Validate reports whether managed can be written as requirements.toml: every
// top-level key is documented, and the document survives a TOML encode and
// decode. A nil or empty document is valid; it renders to an empty file,
// which is a policy that requires nothing.
func Validate(managed map[string]any) error {
	var unknown []string
	for key := range managed {
		if !known(key) {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		quoted := make([]string, len(unknown))
		for i, k := range unknown {
			quoted[i] = fmt.Sprintf("%q", k)
		}
		return fmt.Errorf("requirements: unknown top-level key(s) %s; the allowlist is from the reference as of %s",
			strings.Join(quoted, ", "), KeysAsOf)
	}
	if len(managed) == 0 {
		return nil
	}
	if path := firstNull(managed, ""); path != "" {
		return fmt.Errorf("requirements: %s is null, which TOML cannot represent", path)
	}
	encoded, err := toml.Marshal(managed)
	if err != nil {
		return fmt.Errorf("requirements: the document cannot be written as TOML: %w", err)
	}
	var back map[string]any
	if err := toml.Unmarshal(encoded, &back); err != nil {
		return fmt.Errorf("requirements: the document does not read back as TOML: %w", err)
	}
	return nil
}

// ForAgent is a policy.ManagedValidator: it validates the managed document of
// rules aimed at Codex and ignores every other agent, whose format it does
// not know.
func ForAgent(agentName string, managed map[string]any) error {
	if agentName != "codex" || managed == nil {
		return nil
	}
	return Validate(managed)
}

// firstNull returns the path of the first nil value in v, or "" when there
// is none. TOML has no null: the encoder drops a nil map value silently and
// refuses a nil slice element, and neither is what an author meant.
func firstNull(v any, path string) string {
	switch t := v.(type) {
	case nil:
		return path
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := k
			if path != "" {
				p = path + "." + k
			}
			if found := firstNull(t[k], p); found != "" {
				return found
			}
		}
	case []any:
		for i, e := range t {
			if found := firstNull(e, fmt.Sprintf("%s[%d]", path, i)); found != "" {
				return found
			}
		}
	}
	return ""
}

func known(key string) bool {
	i := sort.SearchStrings(Keys, key)
	return i < len(Keys) && Keys[i] == key
}
