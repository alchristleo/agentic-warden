package schema

import (
	"fmt"
	"strings"
)

// KeysAsOf is the date Overlay was read from the Claude Code settings
// reference (https://code.claude.com/docs/en/settings-reference.md).
const KeysAsOf = "2026-10-23"

// Extra is one settings key the vendored schema has not learned yet.
type Extra struct {
	// Path is the dotted settings path, e.g. "sandbox.allowNestedUserNamespaces".
	Path string
	// Schema is the JSON schema of the value. It is a real type, never "any":
	// a value Claude Code cannot repair makes it refuse to start.
	Schema map[string]any
}

// Overlay lists the keys under the strict blocks (the ones that reject
// unknown keys) that the settings reference documents and the vendored
// schema lacks. An entry the vendored schema later gains makes load fail,
// which is the signal to delete it.
var Overlay = []Extra{
	{"hooks.PostModelSwitch", hookEvent},
	{"hooks.PreModelSwitch", hookEvent},
	{"permissions.blockReadsOutsideWorkingDirectories", map[string]any{"type": "boolean"}},
	{"sandbox.allowNestedUserNamespaces", map[string]any{"type": "boolean"}},
	{"sandbox.credentials.awsPairs", map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"accessKeyIdVar":     map[string]any{"type": "string"},
				"secretAccessKeyVar": map[string]any{"type": "string"},
				"sessionTokenVar":    map[string]any{"type": "string"},
			},
			"required": []any{"accessKeyIdVar", "secretAccessKeyVar"},
		},
	}},
	{"sandbox.credentials.sigv4", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"streaming": sigv4Mode,
			"presigned": sigv4Mode,
			"sigv4a":    sigv4Mode,
		},
	}},
	{"worktree.location", map[string]any{"type": "string"}},
}

var (
	hookEvent = map[string]any{
		"type":  "array",
		"items": map[string]any{"$ref": "#/$defs/hookMatcher"},
	}
	sigv4Mode = map[string]any{"type": "string", "enum": []any{"deny", "passthrough"}}
)

// apply adds each extra's schema to doc, the parsed vendored schema, under the
// properties of its parent block. Local "#/..." references are followed, so a
// block defined in $defs gains the key there. It fails when the parent does
// not resolve to a schema with properties, or when the key is already
// defined.
func apply(doc map[string]any, extras []Extra) error {
	for _, e := range extras {
		i := strings.LastIndex(e.Path, ".")
		if i < 0 {
			return fmt.Errorf("schema: overlay %q: a path needs a parent block", e.Path)
		}
		parent, key := e.Path[:i], e.Path[i+1:]
		props, err := propertiesOf(doc, parent)
		if err != nil {
			return fmt.Errorf("schema: overlay %q: %w", e.Path, err)
		}
		if _, ok := props[key]; ok {
			return fmt.Errorf("schema: overlay %q: the vendored schema already defines it; delete the entry", e.Path)
		}
		props[key] = e.Schema
	}
	return nil
}

// propertiesOf returns the properties map of the schema at the dotted path.
func propertiesOf(doc map[string]any, path string) (map[string]any, error) {
	node := doc
	for _, name := range strings.Split(path, ".") {
		props, err := ownProperties(doc, node)
		if err != nil {
			return nil, err
		}
		next, ok := props[name].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("parent block %q does not resolve", path)
		}
		node = next
	}
	return ownProperties(doc, node)
}

// ownProperties follows node's local $ref chain and returns its properties.
func ownProperties(doc, node map[string]any) (map[string]any, error) {
	for hops := 0; ; hops++ {
		ref, ok := node["$ref"].(string)
		if !ok {
			break
		}
		if hops > 16 || !strings.HasPrefix(ref, "#/") {
			return nil, fmt.Errorf("cannot follow reference %q", ref)
		}
		var cur any = doc
		for _, part := range strings.Split(ref[2:], "/") {
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("reference %q does not resolve", ref)
			}
			cur = m[part]
		}
		target, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("reference %q does not resolve", ref)
		}
		node = target
	}
	props, ok := node["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("parent block has no properties")
	}
	return props, nil
}
