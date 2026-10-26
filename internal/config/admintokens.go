package config

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var (
	adminNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._@-]{0,63}$`)
	sha256HexRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// ValidAdminName reports whether name is a legal admin name.
func ValidAdminName(name string) bool { return adminNameRE.MatchString(name) }

// ParseAdminTokens reads an admin tokens file: one "<name> <sha256-hex>" per
// line, with "#" comments and blank lines allowed. It returns name to hash.
// Errors carry the line number and never the line's text, since a line holds
// credential material.
func ParseAdminTokens(r io.Reader) (map[string]string, error) {
	return parseAdminTokens(r, "")
}

// parseAdminTokens is ParseAdminTokens; a non-empty envHash is the hash of
// AWD_ADMIN_TOKEN, which owns the name "admin" and may not repeat in the file.
func parseAdminTokens(r io.Reader, envHash string) (map[string]string, error) {
	tokens := map[string]string{}
	seen := map[string]bool{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return nil, fmt.Errorf("config: admin tokens file line %d: want \"<name> <sha256-hex>\"", n)
		}
		name, hash := fields[0], fields[1]
		switch {
		case !adminNameRE.MatchString(name):
			return nil, fmt.Errorf("config: admin tokens file line %d: name must match %s", n, adminNameRE)
		case !sha256HexRE.MatchString(hash):
			return nil, fmt.Errorf("config: admin tokens file line %d: hash must be 64 lowercase hex characters", n)
		case envHash != "" && name == "admin":
			return nil, fmt.Errorf("config: admin tokens file line %d: the name admin belongs to AWD_ADMIN_TOKEN", n)
		}
		if _, dup := tokens[name]; dup {
			return nil, fmt.Errorf("config: admin tokens file line %d: duplicate name", n)
		}
		if seen[hash] || hash == envHash {
			return nil, fmt.Errorf("config: admin tokens file line %d: duplicate token hash", n)
		}
		seen[hash] = true
		tokens[name] = hash
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("config: reading admin tokens file: %w", err)
	}
	return tokens, nil
}
