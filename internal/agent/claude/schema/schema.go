// Package schema validates a Claude Code settings document against the
// published JSON schema.
//
// A policy helper that emits settings with a schema violation makes Claude
// Code refuse to start, so the helper validates before it prints, and the
// control plane validates when a policy is applied, where an administrator
// sees the error instead of a developer.
//
// The schema is vendored: the helper runs on every launch and must not depend
// on a network. It can lag the CLI, which is why it allows unknown top-level
// keys; a key it has not learned yet is not a violation.
package schema

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// url is where the vendored schema came from, and the name it is compiled
// under so that a violation is reported against it rather than a local path.
const url = "https://json.schemastore.org/claude-code-settings.json"

// Source is the vendored schema, from url.
//
//go:embed claude-code-settings.json
var Source []byte

var (
	compileOnce sync.Once
	compiled    *jsonschema.Schema
	compileErr  error
)

// load compiles the vendored schema once. A schema that does not compile is a
// build defect, not a runtime condition, so it surfaces as an error from
// every Validate call rather than a panic at init.
func load() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		var doc any
		if err := json.Unmarshal(Source, &doc); err != nil {
			compileErr = fmt.Errorf("schema: parsing the vendored schema: %w", err)
			return
		}
		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource(url, doc); err != nil {
			compileErr = fmt.Errorf("schema: loading the vendored schema: %w", err)
			return
		}
		compiled, compileErr = compiler.Compile(url)
	})
	return compiled, compileErr
}

// Validate reports the first way settings departs from the schema. settings
// is the decoded document, as encoding/json produces it: maps, slices,
// strings, float64, bool and nil.
func Validate(settings any) error {
	s, err := load()
	if err != nil {
		return err
	}
	if _, ok := settings.(map[string]any); !ok {
		return errors.New("schema: the settings document must be a JSON object")
	}
	err = s.Validate(settings)
	if err == nil {
		return nil
	}
	var violation *jsonschema.ValidationError
	if !errors.As(err, &violation) {
		return fmt.Errorf("schema: %w", err)
	}
	return &Error{Causes: leaves(violation)}
}

// Error lists every violation found, one per offending path.
type Error struct {
	Causes []Cause
}

// Cause is one violation.
type Cause struct {
	// Path is a JSON pointer to the offending value; "/" is the document.
	Path string
	// Message says what is wrong with it.
	Message string
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("schema: settings are invalid")
	for _, c := range e.Causes {
		fmt.Fprintf(&b, "\n  at %s: %s", c.Path, c.Message)
	}
	return b.String()
}

// leaves flattens a validation error tree to its leaf causes, which are the
// specific complaints; the interior nodes only say which branch failed.
func leaves(v *jsonschema.ValidationError) []Cause {
	if len(v.Causes) == 0 {
		return []Cause{{Path: pointer(v.InstanceLocation), Message: v.ErrorKind.LocalizedString(message.NewPrinter(language.English))}}
	}
	out := make([]Cause, 0, len(v.Causes))
	for _, c := range v.Causes {
		out = append(out, leaves(c)...)
	}
	return out
}

func pointer(location []string) string {
	if len(location) == 0 {
		return "/"
	}
	return "/" + strings.Join(location, "/")
}

// ForAgent is a policy.ManagedValidator: it validates the managed settings of
// rules aimed at Claude Code and ignores every other agent, whose schema it
// does not know.
func ForAgent(agentName string, managed map[string]any) error {
	if agentName != "claude" || managed == nil {
		return nil
	}
	return Validate(managed)
}
