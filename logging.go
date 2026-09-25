package typesafe

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
)

// levelOff sits above every standard slog level so that "off" disables all
// logging.
const levelOff = slog.LevelError + 4

// logLevels maps TYPESAFE_LOG_LEVEL values to slog levels.
var logLevels = map[string]slog.Level{
	"debug":   slog.LevelDebug,
	"info":    slog.LevelInfo,
	"warn":    slog.LevelWarn,
	"warning": slog.LevelWarn,
	"error":   slog.LevelError,
	"off":     levelOff,
}

// nopHandler keeps the library silent unless the user configures logging or
// sets TYPESAFE_LOG_LEVEL; it is the slog equivalent of a NullHandler.
type nopHandler struct{}

func (nopHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (nopHandler) Handle(context.Context, slog.Record) error { return nil }
func (nopHandler) WithAttrs([]slog.Attr) slog.Handler        { return nopHandler{} }
func (nopHandler) WithGroup(string) slog.Handler             { return nopHandler{} }

// loggerPtr holds the SDK's logger, tagged "typesafe_sdk" so records carry
// the same attribution as the Python SDK's logger name. It is an atomic
// pointer so SetLogger stays safe even with requests in flight.
var loggerPtr atomic.Pointer[slog.Logger]

// LogBodyMode controls whether request and response bodies appear in DEBUG
// wire logs.
type LogBodyMode string

const (
	// LogBodyOff prevents wire bodies from appearing in logs.
	LogBodyOff LogBodyMode = "off"
	// LogBodyRedacted replaces string values, but exposes keys and nonstring
	// scalars. Use LogBodyStrict for bodies containing sensitive identifiers.
	LogBodyRedacted LogBodyMode = "redacted"
	// LogBodyStrict logs only the body size, concealing all keys and values.
	LogBodyStrict LogBodyMode = "strict"
	// LogBodyFull logs raw wire bodies, subject to the logging size limit.
	LogBodyFull LogBodyMode = "full"
)

var logBodyModeValue atomic.Value
var sensitiveHeaderNames atomic.Pointer[map[string]struct{}]

// SetSensitiveHeaders replaces the additional case-insensitive header names
// concealed in request and response wire logs. Built-in redaction always applies.
// Names are copied; pass no names to reset. Safe to call with requests in flight.
func SetSensitiveHeaders(names ...string) {
	next := make(map[string]struct{}, len(names))
	for _, name := range names {
		next[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	sensitiveHeaderNames.Store(&next)
}

func init() {
	loggerPtr.Store(newLogger())
	setupLogging()
	setupLogBodyMode()
}

// sdkLogger returns the active SDK logger.
func sdkLogger() *slog.Logger { return loggerPtr.Load() }

func newLogger() *slog.Logger {
	return slog.New(nopHandler{}).With(slog.String("logger", "typesafe_sdk"))
}

// setupLogging applies TYPESAFE_LOG_LEVEL to the SDK logger when it names a
// known level. Unknown values and "off" leave the logger silent.
func setupLogging() {
	level := strings.ToLower(strings.TrimSpace(os.Getenv(EnvLogLevel)))
	if level == "" || level == "off" {
		return
	}
	lvl, ok := logLevels[level]
	if !ok {
		return
	}
	loggerPtr.Store(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})).
		With(slog.String("logger", "typesafe_sdk")))
}

// SetLogBodyMode configures the body policy used by DEBUG wire logs.
func SetLogBodyMode(mode LogBodyMode) error {
	switch mode {
	case LogBodyOff, LogBodyRedacted, LogBodyStrict, LogBodyFull:
		logBodyModeValue.Store(mode)
		return nil
	default:
		return newTypeSafeError("unknown log body mode %q.", mode)
	}
}

// setupLogBodyMode applies TYPESAFE_LOG_BODY. Invalid and empty values keep
// the safe default of LogBodyOff.
func setupLogBodyMode() {
	mode := LogBodyMode(strings.ToLower(strings.TrimSpace(os.Getenv(EnvLogBody))))
	if mode != LogBodyRedacted && mode != LogBodyStrict && mode != LogBodyFull {
		mode = LogBodyOff
	}
	_ = SetLogBodyMode(mode)
}

func logBodyMode() LogBodyMode {
	return logBodyModeValue.Load().(LogBodyMode)
}

// formatLoggedBody applies the configured body policy and the wire-log size
// limit. Empty bodies stay empty in every mode.
func formatLoggedBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var formatted string
	switch logBodyMode() {
	case LogBodyStrict:
		formatted = fmt.Sprintf("<redacted %d bytes>", len(body))
	case LogBodyFull:
		formatted = string(body)
	case LogBodyRedacted:
		var value any
		if err := json.Unmarshal(body, &value); err != nil {
			formatted = fmt.Sprintf("<redacted %d bytes>", len(body))
		} else {
			if _, ok := value.(string); ok {
				value = "***"
			}
			redactJSONStrings(value)
			encoded, err := marshalJSONCompact(value)
			if err != nil {
				formatted = fmt.Sprintf("<redacted %d bytes>", len(body))
			} else {
				formatted = string(encoded)
			}
		}
	default:
		formatted = "<redacted>"
	}
	if len(formatted) > maxLoggedBodyBytes {
		return formatted[:maxLoggedBodyBytes] + "<truncated>"
	}
	return formatted
}

func redactJSONStrings(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if _, ok := nested.(string); ok {
				typed[key] = "***"
				continue
			}
			redactJSONStrings(nested)
		}
	case []any:
		for index, nested := range typed {
			if _, ok := nested.(string); ok {
				typed[index] = "***"
				continue
			}
			redactJSONStrings(nested)
		}
	}
}

// SetLogger replaces the SDK's logger, giving full control over output
// destination, format, and level — the slog equivalent of configuring the
// "typesafe_sdk" logger in the Python SDK. The logger is tagged with a
// "logger"="typesafe_sdk" attribute so SDK records stay identifiable; pass
// nil to restore the silent default. Safe to call at any time.
func SetLogger(l *slog.Logger) {
	if l == nil {
		loggerPtr.Store(newLogger())
		return
	}
	loggerPtr.Store(l.With(slog.String("logger", "typesafe_sdk")))
}

// isSecretHeader reports whether a header name is redacted from DEBUG wire
// dumps: the known secret names, or any name containing "token" or "secret"
// (all case-insensitive).
func isSecretHeader(name string) bool {
	lowered := strings.ToLower(name)
	if names := sensitiveHeaderNames.Load(); names != nil {
		if _, ok := (*names)[lowered]; ok {
			return true
		}
	}
	compact := strings.NewReplacer("-", "", "_", "").Replace(lowered)
	if strings.Contains(compact, "apikey") {
		return true
	}
	if _, ok := secretHeaders[lowered]; ok {
		return true
	}
	return strings.Contains(lowered, "token") || strings.Contains(lowered, "secret")
}

// redactHeaders is the single redaction choke point for header dumps: every
// header value whose name marks it as secret is replaced with "***".
func redactHeaders(headers http.Header) map[string]string {
	redacted := make(map[string]string, len(headers))
	for name, values := range headers {
		value := ""
		if len(values) > 0 {
			value = strings.Join(values, ", ")
		}
		if isSecretHeader(name) {
			value = "***"
		}
		redacted[name] = value
	}
	return redacted
}
