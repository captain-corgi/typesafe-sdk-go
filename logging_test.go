package typesafe

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestRedactHeaders(t *testing.T) {
	tests := []struct {
		name   string
		header http.Header
	}{
		{"authorization", http.Header{"Authorization": []string{"Bearer secret"}}},
		{"proxy-authorization", http.Header{"Proxy-Authorization": []string{"Bearer secret"}}},
		{"x-api-key", http.Header{"X-Api-Key": []string{"secret"}}},
		{"api-key", http.Header{"Api-Key": []string{"secret"}}},
		{"cookie", http.Header{"Cookie": []string{"session=secret"}}},
		{"set-cookie", http.Header{"Set-Cookie": []string{"session=secret"}}},
		{"token substring", http.Header{"X-Auth-Token": []string{"secret"}}},
		{"secret substring", http.Header{"X-App-Secret": []string{"value"}}},
		{"mixed case substring", http.Header{"X-AuTh-ToKeN": []string{"value"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			redacted := redactHeaders(tt.header)
			for _, values := range redacted {
				if values != "***" {
					t.Errorf("value %q should be redacted to ***", values)
				}
			}
		})
	}

	t.Run("non-secret headers pass through", func(t *testing.T) {
		redacted := redactHeaders(http.Header{
			"Accept":                 []string{"application/json"},
			"X-TypeSafe-Retry-Count": []string{"2"},
		})
		if redacted["Accept"] != "application/json" {
			t.Errorf("Accept should pass through, got %q", redacted["Accept"])
		}
		if redacted["X-TypeSafe-Retry-Count"] != "2" {
			t.Errorf("retry count should pass through, got %q", redacted["X-TypeSafe-Retry-Count"])
		}
	})
}

func TestIsSecretHeader(t *testing.T) {
	for name, secret := range map[string]bool{
		"Authorization":       true,
		"authorization":       true,
		"AUTHORIZATION":       true,
		"Proxy-Authorization": true,
		"X-API-Key":           true,
		"api-key":             true,
		"Cookie":              true,
		"Set-Cookie":          true,
		"X-Session-Token":     true,
		"x-refresh-token":     true,
		"Client-Secret":       true,
		"SECRET-VALUE":        true,
		"Accept":              false,
		"Content-Type":        false,
		"X-TypeSafe-SDK":      false,
		"X-Team":              false,
	} {
		if got := isSecretHeader(name); got != secret {
			t.Errorf("isSecretHeader(%q) = %v, want %v", name, got, secret)
		}
	}
}

func TestDefaultLoggerIsSilent(t *testing.T) {
	// The package logger starts with a no-op handler unless
	// TYPESAFE_LOG_LEVEL is set in the ambient environment; account for a
	// level-set environment by checking against a fresh logger.
	fresh := newLogger()
	for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		if fresh.Enabled(t.Context(), level) {
			t.Errorf("fresh logger should be disabled at %v", level)
		}
	}
	// The active package logger must also be silent unless the environment
	// configured it; skip the check when TYPESAFE_LOG_LEVEL is present.
	if strings.TrimSpace(os.Getenv(EnvLogLevel)) == "" {
		if sdkLogger().Enabled(t.Context(), slog.LevelDebug) {
			t.Error("package logger should be silent by default")
		}
	}
}

// TestSetLogger: users can route SDK records through their own slog setup,
// and SetLogger(nil) restores the silent default.
func TestSetLogger(t *testing.T) {
	var buf bytes.Buffer
	injected := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	swapLogger(t, sdkLogger())

	SetLogger(injected)
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, `{"models": []}`)
	})
	client := newLoggedOutClient(t, server.URL)
	defer client.Close()
	if _, err := client.Models.List(t.Context(), nil); err != nil {
		t.Fatalf("List: %v", err)
	}

	logged := buf.String()
	if !strings.Contains(logged, "logger=typesafe_sdk") {
		t.Errorf("records should carry the logger tag: %s", logged)
	}
	if !strings.Contains(logged, "msg=response") {
		t.Errorf("expected a response record: %s", logged)
	}

	buf.Reset()
	SetLogger(nil)
	if _, err := client.Models.List(t.Context(), nil); err != nil {
		t.Fatalf("List: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("SetLogger(nil) must restore silence, got %q", buf.String())
	}
}

func TestLogLevelsMapping(t *testing.T) {
	for name, want := range map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"warn":    slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
		"off":     levelOff,
	} {
		got, ok := logLevels[name]
		if !ok {
			t.Errorf("level %q missing from logLevels", name)
			continue
		}
		if got != want {
			t.Errorf("logLevels[%q] = %v, want %v", name, got, want)
		}
	}
	if levelOff <= slog.LevelError {
		t.Error("levelOff must sit above slog.LevelError")
	}
}

func TestSetupLoggingAppliesEnvLevel(t *testing.T) {
	original := sdkLogger()
	t.Cleanup(func() { loggerPtr.Store(original) })
	reset := func() { loggerPtr.Store(original) }

	t.Setenv(EnvLogLevel, "DEBUG ")
	setupLogging()
	if !sdkLogger().Enabled(t.Context(), slog.LevelDebug) {
		t.Error("TYPESAFE_LOG_LEVEL=debug should enable debug logging")
	}

	reset()
	t.Setenv(EnvLogLevel, "off")
	setupLogging()
	if sdkLogger().Enabled(t.Context(), slog.LevelError) {
		t.Error("TYPESAFE_LOG_LEVEL=off should keep the logger silent")
	}

	reset()
	t.Setenv(EnvLogLevel, "not-a-level")
	setupLogging()
	if sdkLogger().Enabled(t.Context(), slog.LevelDebug) {
		t.Error("an unknown level should leave the logger silent")
	}

	reset()
	t.Setenv(EnvLogLevel, "  ")
	setupLogging()
	if sdkLogger().Enabled(t.Context(), slog.LevelDebug) {
		t.Error("an empty level should leave the logger silent")
	}
}

func TestLogBodyModes(t *testing.T) {
	t.Cleanup(func() {
		SetLogger(nil)
		_ = SetLogBodyMode(LogBodyOff)
	})
	tests := []struct {
		name string
		mode LogBodyMode
		body []byte
		want string
	}{
		{name: "off", mode: LogBodyOff, body: []byte(`{"name":"secret","count":2}`), want: "<redacted>"},
		{name: "redacted", mode: LogBodyRedacted, body: []byte(`{"name":"secret","count":2,"nested":["value",null]}`), want: `{"count":2,"name":"***","nested":["***",null]}`},
		{name: "redacted top-level string", mode: LogBodyRedacted, body: []byte(`"secret"`), want: `"***"`},
		{name: "full", mode: LogBodyFull, body: []byte(`{"name":"secret"}`), want: `{"name":"secret"}`},
		{name: "invalid redacted", mode: LogBodyRedacted, body: []byte("not json"), want: "<redacted 8 bytes>"},
		{name: "empty", mode: LogBodyFull, body: nil, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := SetLogBodyMode(tt.mode); err != nil {
				t.Fatalf("SetLogBodyMode: %v", err)
			}
			if got := formatLoggedBody(tt.body); got != tt.want {
				t.Errorf("formatLoggedBody() = %q, want %q", got, tt.want)
			}
		})
	}
	if err := SetLogBodyMode("bogus"); err == nil {
		t.Fatal("SetLogBodyMode should reject unknown modes")
	} else {
		var root *TypeSafeError
		if !errors.As(err, &root) {
			t.Fatalf("unknown mode error should be TypeSafeError: %v", err)
		}
	}
}

func TestLoggedBodyTruncation(t *testing.T) {
	t.Cleanup(func() {
		SetLogger(nil)
		_ = SetLogBodyMode(LogBodyOff)
	})
	if err := SetLogBodyMode(LogBodyFull); err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("x"), maxLoggedBodyBytes+1)
	got := formatLoggedBody(body)
	if !strings.HasSuffix(got, "<truncated>") {
		t.Fatalf("formatted body should indicate truncation: suffix %q", got[len(got)-len("<truncated>"):])
	}
	if len(got) != maxLoggedBodyBytes+len("<truncated>") {
		t.Fatalf("formatted body length = %d, want %d", len(got), maxLoggedBodyBytes+len("<truncated>"))
	}
}

// TestLogRecordStrings locks the exact wording of SDK log records: the
// unknown-answer warning mirrors Python's rendered text, wire dumps carry
// direction markers, and a missing request id renders as "-".
func TestLogRecordStrings(t *testing.T) {
	var buf bytes.Buffer
	swapLogger(t, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})).
		With(slog.String("logger", "typesafe_sdk")))

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		// No request-id header: INFO records must fall back to "-".
		writeJSON(w, http.StatusOK, `{
		  "model": "m", "usage": {},
		  "answers": {"mystery": {"type": "aurora", "glow": 1}}
		}`)
	})
	client := newLoggedOutClient(t, server.URL)
	defer client.Close()
	if _, err := systemOneCall(t, client, "x", Questions{"q": Noul{Instructions: "?"}}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}

	logged := buf.String()
	if !strings.Contains(logged, "msg=\"Ignoring answer 'mystery' with unrecognized type 'aurora'\"") {
		t.Errorf("unknown-answer warning text mismatch: %s", logged)
	}
	if !strings.Contains(logged, "msg=wire") || !strings.Contains(logged, "dir=") {
		t.Errorf("DEBUG wire dump should mark the request direction: %s", logged)
	}
	if !strings.Contains(logged, "dir=<-") {
		t.Errorf("DEBUG wire dump should mark the response direction: %s", logged)
	}
	if !strings.Contains(logged, "request_id=-") {
		t.Errorf("missing request id should render as '-': %s", logged)
	}
}

// TestSetLoggerRaceSafe: swapping the logger while requests are in flight
// must stay race-free (run under -race).
func TestSetLoggerRaceSafe(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, `{"models": []}`)
	})
	client := newLoggedOutClient(t, server.URL)
	defer client.Close()

	original := sdkLogger()
	t.Cleanup(func() { loggerPtr.Store(original) })
	quiet := newLogger()
	loud := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})).
		With(slog.String("logger", "typesafe_sdk"))
	var swapping sync.WaitGroup
	swapping.Go(func() {
		for range 50 {
			SetLogger(quiet)
			SetLogger(loud)
		}
	})
	for range 10 {
		if _, err := client.Models.List(t.Context(), nil); err != nil {
			t.Fatalf("List: %v", err)
		}
	}
	swapping.Wait()
}
