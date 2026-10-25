package managed

// KeysAsOf is the date the allowlists below were read from Google's
// Antigravity enterprise gateway page (docs.cloud.google.com, Enterprise
// Gateway) and antigravity.google/docs, and checked against the JSON tags of
// agy 1.4.7. A key that appears after that date is rejected until this file
// is refreshed; the error names the date.
const KeysAsOf = "2026-10-26"

// Documented keys of admin_settings.json, by object. Only these are valid at
// each level. The allowlist catches typos and is stricter than agy: agy
// silently ignores unknown keys (spec T1), so a typo such as "gatway" would
// drop the control on every machine with no error.
var (
	topKeys     = []string{"caCertPath", "gateway", "models"}
	gatewayKeys = []string{"apiKey", "customHeaders", "enabled", "url", "wireProtocol"}
	modelsKeys  = []string{"defaultModelId", "list"}
	modelKeys   = []string{"modelId"}
)

// wireProtocols are the documented values of gateway.wireProtocol.
var wireProtocols = []string{"genai", "openai"}
