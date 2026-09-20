package typesafe

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// config holds the resolved, immutable client configuration.
type config struct {
	apiKey         string
	baseURL        string
	defaultModel   string
	timeout        time.Duration
	defaultHeaders http.Header
}

// resolveEnv returns the first of: an explicit non-empty value, the trimmed
// non-empty environment variable, or the default. Whitespace-only environment
// values count as unset.
func resolveEnv(value, env, fallback string) string {
	if value != "" {
		return value
	}
	if fromEnv := strings.TrimSpace(os.Getenv(env)); fromEnv != "" {
		return fromEnv
	}
	return fallback
}

// resolveTimeout validates a per-attempt timeout: it must be positive and
// finite (a time.Duration is always finite).
func resolveTimeout(timeout time.Duration) error {
	if timeout <= 0 {
		return &TypeSafeError{msg: "timeout must be a positive, finite number of seconds."}
	}
	return nil
}

// resolveConfig applies the arg > env > default precedence for every setting.
// Empty explicit values mean "unset" and fall through to the environment
// (deliberate Go zero-value ergonomics: Python uses even empty explicit
// values verbatim). A non-blank explicit API key is used verbatim; only env
// values are trimmed, matching the Python SDK.
func resolveConfig(apiKey, baseURL, defaultModel string, timeout time.Duration, defaultHeaders http.Header) (*config, error) {
	explicitKey := apiKey
	if strings.TrimSpace(explicitKey) == "" {
		// Blank explicit keys mean "unset" and fall through to the env.
		explicitKey = ""
	}
	key := resolveEnv(explicitKey, EnvAPIKey, "")
	if key == "" {
		return nil, &TypeSafeError{
			msg:     fmt.Sprintf("No API key was provided. Pass WithAPIKey or set the %s environment variable.", EnvAPIKey),
			wrapped: ErrMissingAPIKey,
		}
	}
	resolvedBaseURL := strings.TrimRight(resolveEnv(baseURL, EnvBaseURL, DefaultBaseURL), "/")
	resolvedModel := resolveEnv(defaultModel, EnvDefaultModel, DefaultModel)
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if err := resolveTimeout(timeout); err != nil {
		return nil, err
	}
	if defaultHeaders == nil {
		defaultHeaders = http.Header{}
	}
	return &config{
		apiKey:         key,
		baseURL:        resolvedBaseURL,
		defaultModel:   resolvedModel,
		timeout:        timeout,
		defaultHeaders: defaultHeaders,
	}, nil
}

// runtimeHeaderValue identifies the Go runtime on every request.
func runtimeHeaderValue() string {
	return fmt.Sprintf("go/%s (%s; %s)", goRuntimeVersion, goOS, goArch)
}
