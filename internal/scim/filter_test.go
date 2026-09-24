package scim_test

import (
	"errors"
	"testing"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/scim"
)

func TestParseFilterAcceptsTheFormsTheIdPsSend(t *testing.T) {
	users := []string{model.SCIMAttrUserName, model.SCIMAttrExternalID}
	cases := []struct {
		expr string
		want model.SCIMFilter
	}{
		{``, model.SCIMFilter{}},
		{`userName eq "alice@acme.com"`, model.SCIMFilter{Attribute: "userName", Value: "alice@acme.com"}},
		{`username EQ "alice@acme.com"`, model.SCIMFilter{Attribute: "userName", Value: "alice@acme.com"}},
		{`  userName   eq   "a b"  `, model.SCIMFilter{Attribute: "userName", Value: "a b"}},
		{`externalId eq "00u1"`, model.SCIMFilter{Attribute: "externalId", Value: "00u1"}},
		{`userName eq "quote\"d"`, model.SCIMFilter{Attribute: "userName", Value: `quote"d`}},
		{`urn:ietf:params:scim:schemas:core:2.0:User:userName eq "x"`, model.SCIMFilter{Attribute: "userName", Value: "x"}},
	}
	for _, tc := range cases {
		got, err := scim.ParseFilter(tc.expr, users...)
		if err != nil || got != tc.want {
			t.Errorf("ParseFilter(%q) = %+v, %v; want %+v", tc.expr, got, err, tc.want)
		}
	}
}

func TestParseFilterRejectsEverythingElse(t *testing.T) {
	for _, expr := range []string{
		`userName co "alice"`,
		`userName eq alice`,
		`userName eq "a" and active eq true`,
		`displayName eq "platform"`, // not allowed for users
		`userName eq "unterminated`,
		`userName`,
	} {
		_, err := scim.ParseFilter(expr, model.SCIMAttrUserName, model.SCIMAttrExternalID)
		var scimErr *scim.Error
		if !errors.As(err, &scimErr) || scimErr.Status != 400 || scimErr.ScimType != "invalidFilter" {
			t.Errorf("ParseFilter(%q) err = %v, want 400 invalidFilter", expr, err)
		}
	}
}
