package typesafe

import "time"

// Public environment-variable names and client defaults.
const (
	// EnvAPIKey is the environment variable holding the API key.
	EnvAPIKey = "TYPESAFE_API_KEY"
	// EnvBaseURL is the environment variable overriding the API base URL.
	EnvBaseURL = "TYPESAFE_BASE_URL"
	// EnvDefaultModel is the environment variable overriding the default model.
	EnvDefaultModel = "TYPESAFE_DEFAULT_MODEL"
	// EnvLogLevel is the environment variable selecting the SDK log level
	// (debug | info | warn | warning | error | off).
	EnvLogLevel = "TYPESAFE_LOG_LEVEL"
	// EnvLogBody is the environment variable selecting wire-body logging
	// (off | redacted | full).
	EnvLogBody = "TYPESAFE_LOG_BODY"

	// DefaultBaseURL is the default API base URL.
	DefaultBaseURL = "https://api.typesafe.ai"
	// DefaultModel is the default model name.
	DefaultModel = "jev-latest"
	// DefaultTimeout is the default timeout for each HTTP attempt.
	DefaultTimeout = 10 * time.Second
	// DefaultMaxResponseBodySize is the maximum response body buffered by the
	// SDK and standalone response parsers.
	DefaultMaxResponseBodySize int64 = 16 << 20
)

// Internal protocol and logging constants.
const (
	systemOnePath   = "/v1/systemone"
	modelsPath      = "/v1/models"
	sdkName         = "typesafe-sdk"
	jsonContentType = "application/json"

	authorizationHeader = "Authorization"
	acceptHeader        = "Accept"
	contentTypeHeader   = "Content-Type"
	userAgentHeader     = "User-Agent"
	sdkHeader           = "X-TypeSafe-SDK"
	runtimeHeader       = "X-TypeSafe-Runtime"
	retryCountHeader    = "X-TypeSafe-Retry-Count"
	requestIDHeader     = "x-typesafe-request-id"
	retryAfterHeader    = "retry-after"
	retryAfterMSHeader  = "retry-after-ms"

	// maxErrorBodyLength caps the raw body text embedded in an APIError message.
	maxErrorBodyLength = 200
	// maxLoggedBodyBytes caps the body text embedded in a DEBUG wire log.
	maxLoggedBodyBytes = 16 * 1024
)

// secretHeaders are redacted from DEBUG wire dumps by name (case-insensitive);
// names containing "token" or "secret" are redacted as well.
var secretHeaders = map[string]struct{}{
	"authorization":       {},
	"proxy-authorization": {},
	"x-api-key":           {},
	"api-key":             {},
	"cookie":              {},
	"set-cookie":          {},
}
