// Package scim is the SCIM 2.0 protocol (RFC 7643, RFC 7644) as the control
// plane speaks it: the wire shapes of users, groups, lists and errors, the
// filter and PATCH forms Okta and Entra ID send, and nothing about HTTP
// routing or storage. Vendor quirks are normalized here and nowhere else.
package scim

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
)

// Schema URNs.
const (
	SchemaUser           = "urn:ietf:params:scim:schemas:core:2.0:User"
	SchemaGroup          = "urn:ietf:params:scim:schemas:core:2.0:Group"
	SchemaEnterpriseUser = "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"
	SchemaListResponse   = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	SchemaPatchOp        = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	SchemaError          = "urn:ietf:params:scim:api:messages:2.0:Error"
)

// Error is a SCIM error: an HTTP status, the RFC's scimType keyword when
// one applies, and a detail for the IdP's provisioning log.
type Error struct {
	Status   int
	ScimType string
	Detail   string
}

func (e *Error) Error() string { return fmt.Sprintf("scim %d %s: %s", e.Status, e.ScimType, e.Detail) }

// ErrorBody is the wire form. The RFC makes status a string.
type ErrorBody struct {
	Schemas  []string `json:"schemas"`
	Status   string   `json:"status"`
	ScimType string   `json:"scimType,omitempty"`
	Detail   string   `json:"detail,omitempty"`
}

// Body renders the error for the wire.
func (e *Error) Body() ErrorBody {
	return ErrorBody{Schemas: []string{SchemaError}, Status: strconv.Itoa(e.Status), ScimType: e.ScimType, Detail: e.Detail}
}

// InvalidSyntax is a body the server cannot parse.
func InvalidSyntax(detail string) *Error {
	return &Error{Status: 400, ScimType: "invalidSyntax", Detail: detail}
}

// InvalidFilter is a filter outside the supported forms.
func InvalidFilter(detail string) *Error {
	return &Error{Status: 400, ScimType: "invalidFilter", Detail: detail}
}

// InvalidValue is a well-formed request with a value the server refuses.
func InvalidValue(detail string) *Error {
	return &Error{Status: 400, ScimType: "invalidValue", Detail: detail}
}

// Uniqueness is a name or id already taken.
func Uniqueness(detail string) *Error {
	return &Error{Status: 409, ScimType: "uniqueness", Detail: detail}
}

// NotFound is an unknown resource.
func NotFound(detail string) *Error { return &Error{Status: 404, Detail: detail} }

// Meta is a resource's metadata.
type Meta struct {
	ResourceType string    `json:"resourceType"`
	Created      time.Time `json:"created"`
	LastModified time.Time `json:"lastModified"`
	Location     string    `json:"location,omitempty"`
}

// Member is one group member reference.
type Member struct {
	Value string `json:"value"`
}

// User is a user on the wire.
type User struct {
	Schemas    []string `json:"schemas"`
	ID         string   `json:"id"`
	ExternalID string   `json:"externalId,omitempty"`
	UserName   string   `json:"userName"`
	Active     bool     `json:"active"`
	Meta       Meta     `json:"meta"`
}

// Group is a group on the wire. Members is omitted when the IdP asked for
// excludedAttributes=members, which it does for large groups.
type Group struct {
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id"`
	ExternalID  string   `json:"externalId,omitempty"`
	DisplayName string   `json:"displayName"`
	Members     []Member `json:"members,omitempty"`
	Meta        Meta     `json:"meta"`
}

// ListResponse is a page of resources.
type ListResponse struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    any      `json:"Resources"`
}

// NewListResponse wraps one page.
func NewListResponse(resources any, total, startIndex, count int) ListResponse {
	return ListResponse{Schemas: []string{SchemaListResponse}, TotalResults: total, StartIndex: startIndex, ItemsPerPage: count, Resources: resources}
}

// incomingUser is what the server reads from a user body. Every other
// attribute an IdP sends is accepted and dropped.
type incomingUser struct {
	Schemas    []string `json:"schemas"`
	UserName   string   `json:"userName"`
	ExternalID string   `json:"externalId"`
	Active     *bool    `json:"active"`
}

// DecodeUser reads a POST or PUT user body. It leaves ID and timestamps
// for the caller. An omitted active means active, as both IdPs assume.
func DecodeUser(r io.Reader) (model.SCIMUser, error) {
	var in incomingUser
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return model.SCIMUser{}, InvalidSyntax("the body is not a SCIM user: " + err.Error())
	}
	if !slices.Contains(in.Schemas, SchemaUser) {
		return model.SCIMUser{}, InvalidSyntax("the body does not declare the " + SchemaUser + " schema")
	}
	if in.UserName == "" {
		return model.SCIMUser{}, InvalidValue("userName is required")
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	return model.SCIMUser{UserName: in.UserName, ExternalID: in.ExternalID, Active: active}, nil
}

type incomingGroup struct {
	Schemas     []string `json:"schemas"`
	DisplayName string   `json:"displayName"`
	ExternalID  string   `json:"externalId"`
	Members     []Member `json:"members"`
}

// DecodeGroup reads a POST or PUT group body.
func DecodeGroup(r io.Reader) (model.SCIMGroup, error) {
	var in incomingGroup
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return model.SCIMGroup{}, InvalidSyntax("the body is not a SCIM group: " + err.Error())
	}
	if !slices.Contains(in.Schemas, SchemaGroup) {
		return model.SCIMGroup{}, InvalidSyntax("the body does not declare the " + SchemaGroup + " schema")
	}
	if in.DisplayName == "" {
		return model.SCIMGroup{}, InvalidValue("displayName is required")
	}
	members := make([]string, 0, len(in.Members))
	for _, m := range in.Members {
		if m.Value == "" {
			return model.SCIMGroup{}, InvalidValue("a member has no value")
		}
		members = append(members, m.Value)
	}
	return model.SCIMGroup{DisplayName: in.DisplayName, ExternalID: in.ExternalID, Members: members}, nil
}

// EncodeUser renders a stored user.
func EncodeUser(u model.SCIMUser, location string) User {
	return User{
		Schemas: []string{SchemaUser}, ID: u.ID, ExternalID: u.ExternalID, UserName: u.UserName, Active: u.Active,
		Meta: Meta{ResourceType: "User", Created: u.Created, LastModified: u.Modified, Location: location},
	}
}

// EncodeGroup renders a stored group, with or without its members.
func EncodeGroup(g model.SCIMGroup, location string, withMembers bool) Group {
	out := Group{
		Schemas: []string{SchemaGroup}, ID: g.ID, ExternalID: g.ExternalID, DisplayName: g.DisplayName,
		Meta: Meta{ResourceType: "Group", Created: g.Created, LastModified: g.Modified, Location: location},
	}
	if withMembers {
		out.Members = make([]Member, 0, len(g.Members))
		for _, m := range g.Members {
			out.Members = append(out.Members, Member{Value: m})
		}
	}
	return out
}
