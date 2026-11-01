package typesafe

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestResolveConfigPrecedence(t *testing.T) {
	t.Run("explicit argument wins over environment and default", func(t *testing.T) {
		t.Setenv(EnvAPIKey, "env-key")
		t.Setenv(EnvBaseURL, "https://env.example.test/")
		t.Setenv(EnvDefaultModel, "env-model")
		cfg, err := resolveConfig("arg-key", "https://arg.example.test/", "arg-model", 3*time.Second, 0, false, nil)
		if err != nil {
			t.Fatalf("resolveConfig: %v", err)
		}
		if cfg.apiKey != "arg-key" || cfg.baseURL != "https://arg.example.test" || cfg.defaultModel != "arg-model" || cfg.timeout != 3*time.Second {
			t.Fatalf("unexpected config: %+v", cfg)
		}
	})

	t.Run("environment wins over default", func(t *testing.T) {
		t.Setenv(EnvAPIKey, "  env-key  ")
		t.Setenv(EnvBaseURL, " https://env.example.test/// ")
		t.Setenv(EnvDefaultModel, " env-model ")
		cfg, err := resolveConfig("", "", "", 0, 0, false, nil)
		if err != nil {
			t.Fatalf("resolveConfig: %v", err)
		}
		if cfg.apiKey != "env-key" {
			t.Errorf("api key not trimmed: %q", cfg.apiKey)
		}
		if cfg.baseURL != "https://env.example.test" {
			t.Errorf("base URL trailing slashes not stripped: %q", cfg.baseURL)
		}
		if cfg.defaultModel != "env-model" {
			t.Errorf("model not trimmed: %q", cfg.defaultModel)
		}
		if cfg.timeout != DefaultTimeout {
			t.Errorf("timeout = %v, want default %v", cfg.timeout, DefaultTimeout)
		}
	})

	t.Run("defaults", func(t *testing.T) {
		t.Setenv(EnvAPIKey, "env-key")
		cfg, err := resolveConfig("", "", "", 0, 0, false, nil)
		if err != nil {
			t.Fatalf("resolveConfig: %v", err)
		}
		if cfg.baseURL != DefaultBaseURL || cfg.defaultModel != DefaultModel || cfg.timeout != DefaultTimeout {
			t.Fatalf("unexpected config: %+v", cfg)
		}
	})

	t.Run("whitespace-only environment values are ignored", func(t *testing.T) {
		t.Setenv(EnvAPIKey, "env-key")
		t.Setenv(EnvBaseURL, "   ")
		t.Setenv(EnvDefaultModel, "\t\n")
		cfg, err := resolveConfig("", "", "", 0, 0, false, nil)
		if err != nil {
			t.Fatalf("resolveConfig: %v", err)
		}
		if cfg.baseURL != DefaultBaseURL || cfg.defaultModel != DefaultModel {
			t.Fatalf("whitespace-only env should fall through to defaults: %+v", cfg)
		}
	})

	t.Run("missing API key errors", func(t *testing.T) {
		t.Setenv(EnvAPIKey, "")
		_, err := resolveConfig("", "", "", 0, 0, false, nil)
		if err == nil {
			t.Fatal("expected an error for a missing API key")
		}
		if !errors.Is(err, ErrMissingAPIKey) {
			t.Errorf("error should wrap ErrMissingAPIKey, got %v", err)
		}
		var typeSafeErr *TypeSafeError
		if !errors.As(err, &typeSafeErr) {
			t.Errorf("error should be a *TypeSafeError, got %T", err)
		}
		want := "No API key was provided. Pass WithAPIKey or set the TYPESAFE_API_KEY environment variable."
		if err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}

		// A whitespace-only env key (Python's " \t\n " case) is equally unset.
		t.Setenv(EnvAPIKey, " \t\n ")
		if _, err := resolveConfig("", "", "", 0, 0, false, nil); !errors.Is(err, ErrMissingAPIKey) {
			t.Errorf("whitespace-only env key should be unset, got %v", err)
		}
	})

	t.Run("invalid timeout rejected", func(t *testing.T) {
		t.Setenv(EnvAPIKey, "env-key")
		for _, timeout := range []time.Duration{-time.Second, -time.Millisecond} {
			if _, err := resolveConfig("", "", "", timeout, 0, false, nil); err == nil {
				t.Errorf("timeout %v should be rejected", timeout)
			}
		}
	})

	t.Run("default headers stored", func(t *testing.T) {
		t.Setenv(EnvAPIKey, "env-key")
		headers := http.Header{"X-Team": []string{"core"}}
		cfg, err := resolveConfig("", "", "", 0, 0, false, headers)
		if err != nil {
			t.Fatalf("resolveConfig: %v", err)
		}
		if got := cfg.defaultHeaders.Get("X-Team"); got != "core" {
			t.Errorf("default header lost: %q", got)
		}
	})
}

func TestResolveTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{time.Millisecond, time.Hour} {
		if err := resolveTimeout(timeout); err != nil {
			t.Errorf("timeout %v should be valid, got %v", timeout, err)
		}
	}
	for _, timeout := range []time.Duration{0, -time.Second} {
		err := resolveTimeout(timeout)
		if err == nil {
			t.Errorf("timeout %v should be invalid", timeout)
			continue
		}
		want := "timeout must be a positive, finite number of seconds."
		if err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
	}
}

func TestNewClientOptionValidation(t *testing.T) {
	t.Run("negative timeout rejected", func(t *testing.T) {
		_, err := NewClient(WithAPIKey("k"), WithTimeout(-time.Second))
		if err == nil {
			t.Fatal("expected an error for a negative timeout")
		}
	})
	t.Run("negative response body size rejected", func(t *testing.T) {
		_, err := NewClient(WithAPIKey("k"), WithMaxResponseBodySize(-1))
		if err == nil || err.Error() != "max response body size must be non-negative." {
			t.Fatalf("expected negative response body size error, got %v", err)
		}
	})
	t.Run("zero response body size uses default", func(t *testing.T) {
		client, err := NewClient(WithAPIKey("k"), WithMaxResponseBodySize(0))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		defer client.Close()
		if client.config.maxResponseBodySize != DefaultMaxResponseBodySize {
			t.Fatalf("max response body size = %d, want %d", client.config.maxResponseBodySize, DefaultMaxResponseBodySize)
		}
	})
	t.Run("invalid retry policy rejected", func(t *testing.T) {
		policy := DefaultRetryPolicy()
		policy.MaxRetries = -1
		_, err := NewClient(WithAPIKey("k"), WithRetry(policy))
		if err == nil {
			t.Fatal("expected an error for an invalid retry policy")
		}
	})
	t.Run("nil HTTP client rejected", func(t *testing.T) {
		_, err := NewClient(WithAPIKey("k"), WithHTTPClient(nil))
		if err == nil {
			t.Fatal("expected an error for a nil HTTP client")
		}
	})
	t.Run("valid client from options", func(t *testing.T) {
		server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models": []}`))
		})
		client, err := NewClient(WithAPIKey("key"), WithBaseURL(server.URL+"/"), WithAllowInsecureHTTP(), WithModel("custom"))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		defer client.Close()
		if client.config.apiKey != "key" || client.config.defaultModel != "custom" {
			t.Fatalf("unexpected config: %+v", client.config)
		}
	})
}

func TestValidateBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		allow   bool
		want    string
		wantErr bool
	}{
		{name: "https", raw: "https://api.typesafe.ai/", want: "https://api.typesafe.ai"},
		{name: "https path", raw: "https://example.com/api///", want: "https://example.com/api"},
		{name: "localhost without opt in", raw: "http://localhost:8080", wantErr: true},
		{name: "localhost with opt in", raw: "http://localhost:8080", allow: true, want: "http://localhost:8080"},
		{name: "ipv4 loopback with opt in", raw: "http://127.0.0.1:8080", allow: true, want: "http://127.0.0.1:8080"},
		{name: "ipv6 loopback with opt in", raw: "http://[::1]:8080", allow: true, want: "http://[::1]:8080"},
		{name: "remote http", raw: "http://example.com", allow: true, wantErr: true},
		{name: "ftp", raw: "ftp://example.com", wantErr: true},
		{name: "broken", raw: "://broken", wantErr: true},
		{name: "userinfo", raw: "https://user:pass@example.com", wantErr: true},
		{name: "fragment", raw: "https://example.com#fragment", wantErr: true},
		{name: "query", raw: "https://example.com?x=1", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateBaseURL(tt.raw, tt.allow)
			if tt.wantErr {
				if err == nil || !errors.Is(err, ErrInvalidBaseURL) {
					t.Fatalf("validateBaseURL() error = %v, want invalid base URL", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("validateBaseURL() = %q, %v; want %q, nil", got, err, tt.want)
			}
		})
	}
}

func TestBaseURLEnvironmentValidation(t *testing.T) {
	t.Setenv(EnvAPIKey, "key")
	t.Setenv(EnvBaseURL, "http://example.com")
	_, err := NewClient()
	if err == nil || !errors.Is(err, ErrInvalidBaseURL) {
		t.Fatalf("NewClient() error = %v, want invalid base URL", err)
	}
}

func TestRejectedBaseURLSendsNoRequest(t *testing.T) {
	called := false
	httpClient := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("unexpected request")
	})}
	_, err := NewClient(
		WithAPIKey("secret"),
		WithBaseURL("http://example.com"),
		WithHTTPClient(httpClient),
		WithAllowInsecureHTTP(),
	)
	if err == nil || !errors.Is(err, ErrInvalidBaseURL) {
		t.Fatalf("NewClient() error = %v, want invalid base URL", err)
	}
	if called {
		t.Fatal("rejected base URL must not invoke the HTTP transport")
	}
}

func TestRuntimeHeaderValue(t *testing.T) {
	value := runtimeHeaderValue()
	// Full shape go/<semver> (<os>; <arch>): runtime.Version() reports
	// "go1.27.1", so a missing strip shows up as a doubled "go/go1.27.1"
	// prefix (Python sends "python/3.12.1 (linux; x86_64)", JS
	// "node/24.11.1 (win32; x64)" — both <lang>/<semver>).
	shape := regexp.MustCompile(`^go/\d+\.\d+\.\d+ \([a-z0-9]+; [a-z0-9_]+\)$`)
	if !shape.MatchString(value) {
		t.Errorf("runtime header %q should match %s", value, shape)
	}
	if strings.Count(value, "go/") != 1 {
		t.Errorf("runtime header %q should carry exactly one go/ prefix", value)
	}
}

func TestAPIKeyValidation(t *testing.T) {
	missing := fmt.Sprintf("No API key was provided. Pass WithAPIKey or set the %s environment variable.", EnvAPIKey)
	printable := "API key must contain only printable ASCII characters without whitespace."
	tests := []struct {
		name    string
		apiKey  string
		env     string
		message string
		wantKey string
	}{
		{"missing key", "", "", missing, ""},
		{"whitespace-only env ignored", "", "   \t\n", missing, ""},
		{"valid explicit key stripped", "  ts_live_ok  ", "env-key", "", "ts_live_ok"},
		{"valid env key", "", " env-key ", "", "env-key"},
		// Edge whitespace is stripped (Python's .strip()); only invalid
		// characters inside the key are rejected.
		{"edge newline stripped", "\nts_live_ok\r\n", "env-key", "", "ts_live_ok"},
		{"all-edge whitespace stripped", " \t\r\nts_live_ok \t\r\n", "env-key", "", "ts_live_ok"},
		{"internal newline rejected", "ts_live_x\ny", "env-key", printable, ""},
		{"internal tab rejected", "ts_live_x\ty", "env-key", printable, ""},
		{"internal carriage return rejected", "ts_live_x\ry", "env-key", printable, ""},
		{"internal unit separator rejected", "ts_live_x\x1fy", "env-key", printable, ""},
		{"internal delete rejected", "ts_live_x\x7fy", "env-key", printable, ""},
		{"internal nul rejected", "\x00ts_live_x", "env-key", printable, ""},
		{"internal space rejected", "ts live x", "env-key", printable, ""},
		{"non-ascii rejected", "ts_live_é", "env-key", printable, ""},
		{"zero-width space rejected", "ts_live_\u200b", "env-key", printable, ""},
		{"invalid env key rejected", "", "ts_live_é", printable, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvAPIKey, tt.env)
			cfg, err := resolveConfig(tt.apiKey, "", "", 0, 0, false, nil)
			if tt.message != "" {
				if err == nil || err.Error() != tt.message {
					t.Fatalf("error = %v, want %q", err, tt.message)
				}
				if errors.Is(err, ErrMissingAPIKey) != (tt.message == missing) {
					t.Errorf("ErrMissingAPIKey wrapping = %v for %q", errors.Is(err, ErrMissingAPIKey), tt.message)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveConfig: %v", err)
			}
			if cfg.apiKey != tt.wantKey {
				t.Errorf("api key = %q, want %q", cfg.apiKey, tt.wantKey)
			}
		})
	}
}

func TestAPIKeyNeverLeaked(t *testing.T) {
	const secret = "ts_live_secret_value"
	t.Setenv(EnvAPIKey, "")
	_, err := NewClient(WithAPIKey(secret + "\x00" + "suffix"))
	if err == nil {
		t.Fatal("expected an invalid-key error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error leaks the key: %v", err)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	swapLogger(t, logger)
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, resultBody)
	})
	client, err := NewClient(WithAPIKey(secret), WithBaseURL(server.URL), WithAllowInsecureHTTP())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()
	if _, err := systemOneCall(t, client, "x", Questions{"q": Noul{Instructions: "?"}}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if logged := logs.String(); strings.Contains(logged, secret) {
		t.Errorf("wire logs leak the key: %s", logged)
	}
}
