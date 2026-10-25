// Package managed validates the document aw-sync renders for agy: the
// admin_settings.json object itself, {gateway, caCertPath, models}. The
// checks are a dated, typo-catching allowlist of keys at every level, no
// secret in the policy, and per-field value rules.
package managed

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// secretHeader matches a customHeaders name that probably carries a
// credential. It is best effort: a policy author can still name a secret
// header innocently, and the docs say so.
var secretHeader = regexp.MustCompile(`(?i)auth|key|token|secret|cookie|password`)

// secretValuePrefixes start a customHeaders value that is a credential: an
// auth scheme or a Google API key.
var secretValuePrefixes = []string{"Bearer ", "Basic ", "AIza"}

// Validate reports whether doc can be rendered as agy's admin_settings.json.
// A nil or empty document is valid and renders to {}. Every check looks at one
// field alone, so rules that are valid one at a time stay valid once merged.
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
	if agentName != "agy" {
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
	if rawURL, ok := g["url"]; ok {
		s, isStr := rawURL.(string)
		u, err := url.Parse(s)
		if !isStr || err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("managed: gateway.url must be an https URL, not %v", rawURL)
		}
		if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("managed: gateway.url must not carry userinfo, a query or a fragment: they hold secrets")
		}
	}
	if raw, ok := g["wireProtocol"]; ok {
		s, _ := raw.(string)
		if !slices.Contains(wireProtocols, s) {
			return fmt.Errorf("managed: gateway.wireProtocol must be one of %s, not %v", strings.Join(wireProtocols, ", "), raw)
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
			val, isStr := v.(string)
			if !isStr {
				return fmt.Errorf("managed: gateway.customHeaders %q must be a string, not %T", name, v)
			}
			for _, prefix := range secretValuePrefixes {
				if len(val) >= len(prefix) && strings.EqualFold(val[:len(prefix)], prefix) {
					return fmt.Errorf("managed: gateway.customHeaders %q value looks like a credential and does not belong in policy; set AGY_LLM_GATEWAY_API_KEY in each developer's environment", name)
				}
			}
		}
	}
	return nil
}

func validateModels(m map[string]any) error {
	if err := checkKeys(m, modelsKeys, "models"); err != nil {
		return err
	}
	if raw, ok := m["list"]; ok {
		list, isList := raw.([]any)
		if !isList {
			return fmt.Errorf("managed: models.list must be a list, not %T", raw)
		}
		if len(list) == 0 {
			return fmt.Errorf("managed: models.list must not be empty")
		}
		for i, entry := range list {
			where := fmt.Sprintf("models.list[%d]", i)
			e, isObj := entry.(map[string]any)
			if !isObj {
				return fmt.Errorf("managed: %s must be an object, not %T", where, entry)
			}
			if err := checkKeys(e, modelKeys, where); err != nil {
				return err
			}
			if id, _ := e["modelId"].(string); id == "" {
				return fmt.Errorf("managed: %s.modelId must be a non-empty string", where)
			}
		}
	}
	if raw, ok := m["defaultModelId"]; ok {
		id, _ := raw.(string)
		if id == "" {
			return fmt.Errorf("managed: models.defaultModelId must be a non-empty string")
		}
	}
	return nil
}

// checkKeys rejects every key of obj that is not in allowed. where
// names the object in the error.
func checkKeys(obj map[string]any, allowed []string, where string) error {
	var unknown []string
	for key := range obj {
		if !slices.Contains(allowed, key) {
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
	return fmt.Errorf("managed: unknown key(s) %s in %s; aw rejects unknown keys to catch typos (agy would ignore them silently), and the allowlist is from Google's reference as of %s", strings.Join(q, ", "), where, KeysAsOf)
}

// isWindowsAbs accepts a drive-letter path, which the Windows admin file
// location (%ProgramData%) makes a plausible caCertPath.
func isWindowsAbs(p string) bool {
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') &&
		(p[0] >= 'A' && p[0] <= 'Z' || p[0] >= 'a' && p[0] <= 'z')
}
