// Package managed validates the document aw-sync renders for agy: the
// admin_settings.json object itself, {gateway, caCertPath, models}. The
// checks are a dated allowlist of keys at every level, no secret in the
// policy, and the value rules agy would otherwise fail on at startup.
package managed

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// secretHeader matches a customHeaders name that probably carries a
// credential. It is best effort: a policy author can still name a secret
// header innocently, and the docs say so.
var secretHeader = regexp.MustCompile(`(?i)auth|key|token|secret|cookie|password`)

// Validate reports whether doc can be rendered as agy's admin_settings.json.
// A nil or empty document is valid and renders to {}.
func Validate(doc map[string]any) error {
	if err := checkKeys(doc, topKeys, "document"); err != nil {
		return err
	}
	if raw, ok := doc["caCertPath"]; ok {
		p, isStr := raw.(string)
		if !isStr || !(strings.HasPrefix(p, "/") || isWindowsAbs(p)) {
			return fmt.Errorf("managed: caCertPath must be an absolute path string, not %v", raw)
		}
	}
	if raw, ok := doc["gateway"]; ok {
		g, isObj := raw.(map[string]any)
		if !isObj {
			return fmt.Errorf("managed: gateway must be an object, not %T", raw)
		}
		if err := validateGateway(g); err != nil {
			return err
		}
	}
	if raw, ok := doc["models"]; ok {
		m, isObj := raw.(map[string]any)
		if !isObj {
			return fmt.Errorf("managed: models must be an object, not %T", raw)
		}
		return validateModels(m)
	}
	return nil
}

// ForAgent is a policy.ManagedValidator: it validates the managed document of
// rules aimed at agy and ignores every other agent, whose format it does not
// know.
func ForAgent(agentName string, doc map[string]any) error {
	if agentName != "agy" || doc == nil {
		return nil
	}
	return Validate(doc)
}

func validateGateway(g map[string]any) error {
	if _, ok := g["apiKey"]; ok {
		return fmt.Errorf("managed: gateway.apiKey is a secret and does not belong in policy; set AGY_LLM_GATEWAY_API_KEY in each developer's environment")
	}
	if err := checkKeys(g, gatewayKeys, "gateway"); err != nil {
		return err
	}
	if raw, ok := g["enabled"]; ok {
		if _, isBool := raw.(bool); !isBool {
			return fmt.Errorf("managed: gateway.enabled must be true or false, not %T", raw)
		}
	}
	rawURL, hasURL := g["url"]
	if hasURL {
		s, isStr := rawURL.(string)
		u, err := url.Parse(s)
		if !isStr || err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("managed: gateway.url must be an https URL, not %v", rawURL)
		}
	}
	if enabled, _ := g["enabled"].(bool); enabled && !hasURL {
		return fmt.Errorf("managed: gateway.enabled is true but gateway.url is missing")
	}
	if raw, ok := g["wireProtocol"]; ok {
		s, _ := raw.(string)
		if i := sort.SearchStrings(WireProtocols, s); i == len(WireProtocols) || WireProtocols[i] != s {
			return fmt.Errorf("managed: gateway.wireProtocol must be one of %s, not %v", strings.Join(WireProtocols, ", "), raw)
		}
	}
	if raw, ok := g["customHeaders"]; ok {
		headers, isObj := raw.(map[string]any)
		if !isObj {
			return fmt.Errorf("managed: gateway.customHeaders must be an object, not %T", raw)
		}
		for name, v := range headers {
			if secretHeader.MatchString(name) {
				return fmt.Errorf("managed: gateway.customHeaders %q looks like a credential and does not belong in policy; set AGY_LLM_GATEWAY_API_KEY in each developer's environment", name)
			}
			if _, isStr := v.(string); !isStr {
				return fmt.Errorf("managed: gateway.customHeaders %q must be a string, not %T", name, v)
			}
		}
	}
	return nil
}

func validateModels(m map[string]any) error {
	if err := checkKeys(m, modelsKeys, "models"); err != nil {
		return err
	}
	ids := map[string]bool{}
	hasList := false
	if raw, ok := m["list"]; ok {
		list, isList := raw.([]any)
		if !isList {
			return fmt.Errorf("managed: models.list must be a list, not %T", raw)
		}
		hasList = true
		for i, entry := range list {
			where := fmt.Sprintf("models.list[%d]", i)
			e, isObj := entry.(map[string]any)
			if !isObj {
				return fmt.Errorf("managed: %s must be an object, not %T", where, entry)
			}
			if err := checkKeys(e, modelKeys, where); err != nil {
				return err
			}
			id, _ := e["modelId"].(string)
			if id == "" {
				return fmt.Errorf("managed: %s.modelId must be a non-empty string", where)
			}
			ids[id] = true
		}
	}
	if raw, ok := m["defaultModelId"]; ok {
		id, _ := raw.(string)
		if id == "" {
			return fmt.Errorf("managed: models.defaultModelId must be a non-empty string")
		}
		if hasList && !ids[id] {
			return fmt.Errorf("managed: models.defaultModelId %q is not in models.list", id)
		}
	}
	return nil
}

// checkKeys rejects every key of obj that is not in allowed (sorted). where
// names the object in the error.
func checkKeys(obj map[string]any, allowed []string, where string) error {
	var unknown []string
	for key := range obj {
		if i := sort.SearchStrings(allowed, key); i == len(allowed) || allowed[i] != key {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	q := make([]string, len(unknown))
	for i, k := range unknown {
		q[i] = fmt.Sprintf("%q", k)
	}
	return fmt.Errorf("managed: unknown key(s) %s in %s; the allowlist is from Google's reference as of %s", strings.Join(q, ", "), where, KeysAsOf)
}

// isWindowsAbs accepts a drive-letter path, which the Windows admin file
// location (%ProgramData%) makes a plausible caCertPath.
func isWindowsAbs(p string) bool {
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') &&
		(p[0] >= 'A' && p[0] <= 'Z' || p[0] >= 'a' && p[0] <= 'z')
}
