package agent

import "strings"

// RevisionFromHeader reads the policy revision a renderer put in a comment on
// the first line of a file it wrote, or "" when the file has no such header.
// It lives here, rather than duplicated inside each adapter, because more
// than one renderer (Codex's requirements.toml, and later Gemini's managed
// settings) has no sidecar to carry the revision and so writes it into the
// file's own first line; `aw doctor` reads it back through this one function
// regardless of which adapter's file it is looking at.
func RevisionFromHeader(content []byte) string {
	const prefix = "# Managed by aw-sync from policy revision "
	first, _, _ := strings.Cut(string(content), "\n")
	if !strings.HasPrefix(first, prefix) {
		return ""
	}
	version, _, _ := strings.Cut(strings.TrimPrefix(first, prefix), ". ")
	return version
}
