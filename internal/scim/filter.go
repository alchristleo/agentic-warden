package scim

import (
	"encoding/json"
	"strings"

	"github.com/acme/agent-wrapper/internal/model"
)

// ParseFilter reads the one filter form the target IdPs send,
// `<attribute> eq "<value>"`, for an attribute in allowed. The attribute
// name and the operator are case-insensitive, as RFC 7644 §3.4.2.2 says;
// a core-schema URN prefix on the attribute is accepted. Anything else is
// an invalidFilter error rather than an empty list, so an IdP that sends
// a filter the server does not understand finds out.
func ParseFilter(expr string, allowed ...string) (model.SCIMFilter, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return model.SCIMFilter{}, nil
	}
	attr, rest, ok := strings.Cut(expr, " ")
	if !ok {
		return model.SCIMFilter{}, InvalidFilter("expected `attribute eq \"value\"`, got " + expr)
	}
	rest = strings.TrimSpace(rest)
	op, value, ok := strings.Cut(rest, " ")
	if !ok || !strings.EqualFold(op, "eq") {
		return model.SCIMFilter{}, InvalidFilter("only the eq operator is supported: " + expr)
	}
	value = strings.TrimSpace(value)
	var unquoted string
	if !strings.HasPrefix(value, `"`) || json.Unmarshal([]byte(value), &unquoted) != nil {
		return model.SCIMFilter{}, InvalidFilter("the value must be one quoted string: " + expr)
	}
	attr = stripCoreURN(attr)
	for _, a := range allowed {
		if strings.EqualFold(attr, a) {
			return model.SCIMFilter{Attribute: a, Value: unquoted}, nil
		}
	}
	return model.SCIMFilter{}, InvalidFilter("cannot filter on " + attr)
}

// stripCoreURN removes a core user or group schema prefix from an
// attribute path: "urn:…:core:2.0:User:userName" is "userName".
func stripCoreURN(path string) string {
	for _, urn := range []string{SchemaUser, SchemaGroup} {
		if len(path) > len(urn) && strings.EqualFold(path[:len(urn)+1], urn+":") {
			return path[len(urn)+1:]
		}
	}
	return path
}
