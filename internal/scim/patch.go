package scim

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/acme/agent-wrapper/internal/model"
)

// patchRequest is a PatchOp body. Go's decoder matches "Operations" and
// "operations" alike, which covers both IdPs.
type patchRequest struct {
	Schemas    []string    `json:"schemas"`
	Operations []operation `json:"Operations"`
}

type operation struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

// readPatch decodes the envelope and normalizes each op to lower case,
// since Entra sends "Replace" and "Add".
func readPatch(r io.Reader) ([]operation, error) {
	var req patchRequest
	if err := json.NewDecoder(r).Decode(&req); err != nil {
		return nil, InvalidSyntax("the body is not a SCIM PatchOp: " + err.Error())
	}
	if !slices.Contains(req.Schemas, SchemaPatchOp) {
		return nil, InvalidSyntax("the body does not declare the " + SchemaPatchOp + " schema")
	}
	if len(req.Operations) == 0 {
		return nil, InvalidSyntax("the PatchOp has no Operations")
	}
	for i := range req.Operations {
		req.Operations[i].Op = strings.ToLower(req.Operations[i].Op)
		switch req.Operations[i].Op {
		case "add", "replace", "remove":
		default:
			return nil, InvalidSyntax(fmt.Sprintf("unsupported op %q", req.Operations[i].Op))
		}
		req.Operations[i].Path = stripCoreURN(strings.TrimSpace(req.Operations[i].Path))
	}
	return req.Operations, nil
}

// noPathValue splits the object a path-less add or replace carries into
// its attributes, keyed by lower-cased name.
func noPathValue(op operation) (map[string]json.RawMessage, error) {
	var attrs map[string]json.RawMessage
	if err := json.Unmarshal(op.Value, &attrs); err != nil {
		return nil, InvalidValue("an operation without a path needs an object value")
	}
	out := make(map[string]json.RawMessage, len(attrs))
	for k, v := range attrs {
		out[strings.ToLower(stripCoreURN(k))] = v
	}
	return out, nil
}

func stringValue(raw json.RawMessage, attr string, allowEmpty bool) (*string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, InvalidValue(attr + " must be a string")
	}
	if s == "" && !allowEmpty {
		return nil, InvalidValue(attr + " must not be empty")
	}
	return &s, nil
}

// boolValue accepts a JSON boolean or, as Entra sends, the strings "True"
// and "False" in any case.
func boolValue(raw json.RawMessage, attr string) (*bool, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return &b, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch strings.ToLower(s) {
		case "true":
			b = true
			return &b, nil
		case "false":
			return &b, nil
		}
	}
	return nil, InvalidValue(attr + " must be a boolean")
}

// UserPatch folds a user PatchOp into the fields it sets. Paths the server
// does not hold (name.*, emails[…], enterprise extension attributes) are
// accepted as no-ops, so an IdP does not retry them forever.
func UserPatch(r io.Reader) (model.SCIMUserChange, error) {
	ops, err := readPatch(r)
	if err != nil {
		return model.SCIMUserChange{}, err
	}
	var c model.SCIMUserChange
	set := func(attr string, raw json.RawMessage, remove bool) error {
		switch attr {
		case "username":
			if remove {
				return InvalidValue("userName cannot be removed")
			}
			v, err := stringValue(raw, "userName", false)
			c.UserName = v
			return err
		case "externalid":
			if remove {
				c.ExternalID = new(string)
				return nil
			}
			v, err := stringValue(raw, "externalId", true)
			c.ExternalID = v
			return err
		case "active":
			if remove {
				return InvalidValue("active cannot be removed")
			}
			v, err := boolValue(raw, "active")
			c.Active = v
			return err
		}
		return nil
	}
	for _, op := range ops {
		remove := op.Op == "remove"
		if op.Path == "" {
			if remove {
				return model.SCIMUserChange{}, InvalidValue("remove needs a path")
			}
			attrs, err := noPathValue(op)
			if err != nil {
				return model.SCIMUserChange{}, err
			}
			for attr, raw := range attrs {
				if err := set(attr, raw, false); err != nil {
					return model.SCIMUserChange{}, err
				}
			}
			continue
		}
		if err := set(strings.ToLower(op.Path), op.Value, remove); err != nil {
			return model.SCIMUserChange{}, err
		}
	}
	return c, nil
}

// memberValues reads a members value list.
func memberValues(raw json.RawMessage) ([]string, error) {
	var members []Member
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, InvalidValue("members must be a list of {\"value\": id}")
	}
	out := make([]string, 0, len(members))
	for _, m := range members {
		if m.Value == "" {
			return nil, InvalidValue("a member has no value")
		}
		out = append(out, m.Value)
	}
	return out, nil
}

// memberFilterID reads the id out of Okta's `members[value eq "<id>"]`.
func memberFilterID(path string) (string, bool, error) {
	lower := strings.ToLower(path)
	if !strings.HasPrefix(lower, "members[") {
		return "", false, nil
	}
	if !strings.HasSuffix(path, "]") {
		return "", true, InvalidValue("unsupported member path " + path)
	}
	f, err := ParseFilter(path[len("members["):len(path)-1], "value")
	if err != nil {
		return "", true, InvalidValue("unsupported member path " + path)
	}
	return f.Value, true, nil
}

// GroupPatch folds a group PatchOp into a change set. Member operations
// keep their order; the store applies them in that order in one
// transaction.
func GroupPatch(r io.Reader) (model.SCIMGroupChange, error) {
	ops, err := readPatch(r)
	if err != nil {
		return model.SCIMGroupChange{}, err
	}
	var c model.SCIMGroupChange
	set := func(op, attr string, raw json.RawMessage) error {
		switch attr {
		case "displayname":
			if op == "remove" {
				return InvalidValue("displayName cannot be removed")
			}
			v, err := stringValue(raw, "displayName", false)
			c.DisplayName = v
			return err
		case "externalid":
			if op == "remove" {
				c.ExternalID = new(string)
				return nil
			}
			v, err := stringValue(raw, "externalId", true)
			c.ExternalID = v
			return err
		case "members":
			if op == "remove" && len(raw) == 0 {
				c.Members = append(c.Members, model.SCIMMemberOp{Kind: model.SCIMMembersReplace, Users: []string{}})
				return nil
			}
			ids, err := memberValues(raw)
			if err != nil {
				return err
			}
			kind := map[string]string{"add": model.SCIMMembersAdd, "remove": model.SCIMMembersRemove, "replace": model.SCIMMembersReplace}[op]
			c.Members = append(c.Members, model.SCIMMemberOp{Kind: kind, Users: ids})
		}
		return nil
	}
	for _, op := range ops {
		if op.Path == "" {
			if op.Op == "remove" {
				return model.SCIMGroupChange{}, InvalidValue("remove needs a path")
			}
			attrs, err := noPathValue(op)
			if err != nil {
				return model.SCIMGroupChange{}, err
			}
			// Map iteration order is random; apply attributes in a fixed
			// order so a body with both a rename and members is deterministic.
			for _, attr := range []string{"displayname", "externalid", "members"} {
				if raw, ok := attrs[attr]; ok {
					if err := set(op.Op, attr, raw); err != nil {
						return model.SCIMGroupChange{}, err
					}
				}
			}
			continue
		}
		id, isFilter, err := memberFilterID(op.Path)
		if err != nil {
			return model.SCIMGroupChange{}, err
		}
		if isFilter {
			if op.Op != "remove" {
				return model.SCIMGroupChange{}, InvalidValue("a member filter path is supported only for remove")
			}
			c.Members = append(c.Members, model.SCIMMemberOp{Kind: model.SCIMMembersRemove, Users: []string{id}})
			continue
		}
		if err := set(op.Op, strings.ToLower(op.Path), op.Value); err != nil {
			return model.SCIMGroupChange{}, err
		}
	}
	return c, nil
}
