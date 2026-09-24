package scim

// ServiceProviderConfig tells the IdP what this server supports. Both
// Okta and Entra read it; neither depends on anything beyond patch and
// filter being true.
func ServiceProviderConfig() any {
	return map[string]any{
		"schemas":        []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"patch":          map[string]bool{"supported": true},
		"bulk":           map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":         map[string]any{"supported": true, "maxResults": 1000},
		"changePassword": map[string]bool{"supported": false},
		"sort":           map[string]bool{"supported": false},
		"etag":           map[string]bool{"supported": false},
		"authenticationSchemes": []map[string]any{{
			"type": "oauthbearertoken", "name": "Bearer token",
			"description": "The AWD_SCIM_TOKEN shared secret in an Authorization: Bearer header.",
		}},
	}
}

// ResourceTypes lists User and Group.
func ResourceTypes(base string) ListResponse {
	types := []map[string]any{
		{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"}, "id": "User", "name": "User",
			"endpoint": "/Users", "schema": SchemaUser, "meta": map[string]string{"resourceType": "ResourceType", "location": base + "/ResourceTypes/User"}},
		{"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"}, "id": "Group", "name": "Group",
			"endpoint": "/Groups", "schema": SchemaGroup, "meta": map[string]string{"resourceType": "ResourceType", "location": base + "/ResourceTypes/Group"}},
	}
	return NewListResponse(types, len(types), 1, len(types))
}

func attribute(name, typ string, required, caseExact bool, uniqueness string) map[string]any {
	return map[string]any{"name": name, "type": typ, "multiValued": false, "required": required,
		"caseExact": caseExact, "mutability": "readWrite", "returned": "default", "uniqueness": uniqueness}
}

// Schemas describes the attributes the server keeps; everything else an
// IdP sends is accepted and dropped.
func Schemas() ListResponse {
	members := map[string]any{"name": "members", "type": "complex", "multiValued": true, "required": false,
		"mutability": "readWrite", "returned": "default",
		"subAttributes": []map[string]any{attribute("value", "string", true, true, "none")}}
	schemas := []map[string]any{
		{"id": SchemaUser, "name": "User", "attributes": []map[string]any{
			attribute("userName", "string", true, false, "server"),
			attribute("externalId", "string", false, true, "none"),
			attribute("active", "boolean", false, false, "none"),
		}},
		{"id": SchemaGroup, "name": "Group", "attributes": []map[string]any{
			attribute("displayName", "string", true, false, "server"),
			attribute("externalId", "string", false, true, "none"),
			members,
		}},
	}
	return NewListResponse(schemas, len(schemas), 1, len(schemas))
}
