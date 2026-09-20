package typesafe

import (
	"context"
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

func init() {
	loggerPtr.Store(newLogger())
	setupLogging()
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
