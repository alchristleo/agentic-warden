package scim_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/acme/agent-wrapper/internal/model"
	"github.com/acme/agent-wrapper/internal/scim"
)

func TestDecodeUserKeepsWhatResolutionNeedsAndDropsTheRest(t *testing.T) {
	body := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User","urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"],
	  "userName":"alice@acme.com","externalId":"00u1","active":false,
	  "name":{"givenName":"Alice"},"emails":[{"value":"alice@acme.com","primary":true}],"password":"x"}`
	u, err := scim.DecodeUser(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if u.UserName != "alice@acme.com" || u.ExternalID != "00u1" || u.Active {
		t.Errorf("user = %+v", u)
	}
}

func TestDecodeUserDefaultsActiveToTrue(t *testing.T) {
	u, err := scim.DecodeUser(strings.NewReader(`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"a@acme.com"}`))
	if err != nil || !u.Active {
		t.Errorf("user = %+v, err = %v; an omitted active means active", u, err)
	}
}

func TestDecodeUserRejects(t *testing.T) {
	for name, body := range map[string]string{
		"not json":     `{`,
		"no schema":    `{"userName":"a@acme.com"}`,
		"no userName":  `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"]}`,
		"group schema": `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:Group"],"userName":"a"}`,
	} {
		_, err := scim.DecodeUser(strings.NewReader(body))
		var scimErr *scim.Error
		if !errors.As(err, &scimErr) || scimErr.Status != 400 {
			t.Errorf("%s: err = %v, want a 400 scim error", name, err)
		}
	}
}

func TestDecodeGroupReadsMemberValues(t *testing.T) {
	body := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:Group"],"displayName":"platform","externalId":"g-ext",
	  "members":[{"value":"u1","display":"alice@acme.com"},{"value":"u2"}]}`
	g, err := scim.DecodeGroup(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if g.DisplayName != "platform" || g.ExternalID != "g-ext" || strings.Join(g.Members, ",") != "u1,u2" {
		t.Errorf("group = %+v", g)
	}
}

func TestEncodeGroupCanOmitMembers(t *testing.T) {
	at := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	g := model.SCIMGroup{ID: "g1", DisplayName: "platform", Members: []string{"u1"}, Created: at, Modified: at}
	raw, _ := json.Marshal(scim.EncodeGroup(g, "https://awd/scim/v2/Groups/g1", false))
	if strings.Contains(string(raw), "members") {
		t.Errorf("excluded members still encoded: %s", raw)
	}
	raw, _ = json.Marshal(scim.EncodeGroup(g, "https://awd/scim/v2/Groups/g1", true))
	if !strings.Contains(string(raw), `"members":[{"value":"u1"}]`) || !strings.Contains(string(raw), `"resourceType":"Group"`) {
		t.Errorf("encoded = %s", raw)
	}
}

func TestErrorBodyCarriesTheStatusAsAString(t *testing.T) {
	raw, _ := json.Marshal(scim.Uniqueness("userName taken").Body())
	want := `{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"status":"409","scimType":"uniqueness","detail":"userName taken"}`
	if string(raw) != want {
		t.Errorf("body = %s\nwant   %s", raw, want)
	}
}
