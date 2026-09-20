// Package glob matches the simple wildcard patterns used in permission rules
// and repository targeting.
//
// A '*' stands for any run of characters, path separators included. That is
// looser than path.Match on purpose: "./secrets/*" should cover everything
// under the directory, and a deny rule that matches too much fails safe.
package glob

import "strings"

// Match reports whether value matches pattern.
func Match(pattern, value string) bool {
	segments := strings.Split(pattern, "*")
	if len(segments) == 1 {
		return pattern == value
	}

	prefix, suffix := segments[0], segments[len(segments)-1]
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	value = value[len(prefix):]

	// The suffix is consumed last. Reserving it here stops a middle segment
	// from eating the characters the suffix still needs, so "a*a" does not
	// match "a".
	if len(value) < len(suffix) || !strings.HasSuffix(value, suffix) {
		return false
	}
	value = value[:len(value)-len(suffix)]

	for _, segment := range segments[1 : len(segments)-1] {
		i := strings.Index(value, segment)
		if i < 0 {
			return false
		}
		value = value[i+len(segment):]
	}
	return true
}

// MatchAny returns the first pattern in patterns that matches value.
func MatchAny(patterns []string, value string) (string, bool) {
	for _, pattern := range patterns {
		if Match(pattern, value) {
			return pattern, true
		}
	}
	return "", false
}
