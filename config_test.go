package typesafe

import (
	"errors"
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
		cfg, err := resolveConfig("arg-key", "https://arg.example.test/", "arg-model", 3*time.Second, nil)
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
		cfg, err := resolveConfig("", "", "", 0, nil)
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
		cfg, err := resolveConfig("", "", "", 0, nil)
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
		cfg, err := resolveConfig("", "", "", 0, nil)
		if err != nil {
			t.Fatalf("resolveConfig: %v", err)
		}
		if cfg.baseURL != DefaultBaseURL || cfg.defaultModel != DefaultModel {
			t.Fatalf("whitespace-only env should fall through to defaults: %+v", cfg)
		}
	})

	t.Run("missing API key errors", func(t *testing.T) {
		t.Setenv(EnvAPIKey, "")
		_, err := resolveConfig("", "", "", 0, nil)
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
		if _, err := resolveConfig("", "", "", 0, nil); !errors.Is(err, ErrMissingAPIKey) {
			t.Errorf("whitespace-only env key should be unset, got %v", err)
		}
	})

	t.Run("invalid timeout rejected", func(t *testing.T) {
		t.Setenv(EnvAPIKey, "env-key")
		for _, timeout := range []time.Duration{-time.Second, -time.Millisecond} {
			if _, err := resolveConfig("", "", "", timeout, nil); err == nil {
				t.Errorf("timeout %v should be rejected", timeout)
			}
		}
	})

	t.Run("default headers stored", func(t *testing.T) {
		t.Setenv(EnvAPIKey, "env-key")
		headers := http.Header{"X-Team": []string{"core"}}
		cfg, err := resolveConfig("", "", "", 0, headers)
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
		client, err := NewClient(WithAPIKey("key"), WithBaseURL(server.URL+"/"), WithModel("custom"))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		defer client.Close()
		if client.config.apiKey != "key" || client.config.defaultModel != "custom" {
			t.Fatalf("unexpected config: %+v", client.config)
		}
	})
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
