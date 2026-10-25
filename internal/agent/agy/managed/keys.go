package managed

// KeysAsOf is the date the allowlists below were read from Google's
// Antigravity enterprise gateway page (docs.cloud.google.com, Enterprise
// Gateway) and antigravity.google/docs, and checked against the JSON tags of
// agy 1.4.7. A key that appears after that date is rejected until this file
// is refreshed; the error names the date.
const KeysAsOf = "2026-10-26"

// Documented keys of admin_settings.json, by object. Only these are valid at
// each level: agy reads the file with unknown fields refused, so a typo
// would otherwise fail on every developer machine instead of at apply time.
var (
	topKeys     = []string{"caCertPath", "gateway", "models"}
	gatewayKeys = []string{"apiKey", "customHeaders", "enabled", "url", "wireProtocol"}
	modelsKeys  = []string{"defaultModelId", "list"}
	modelKeys   = []string{"modelId"}
)

// WireProtocols are the documented values of gateway.wireProtocol.
var WireProtocols = []string{"genai", "openai"}
