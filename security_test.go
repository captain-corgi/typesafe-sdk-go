package typesafe

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestSecurityReservedExtraBodyFieldsFailBeforeTransport(t *testing.T) {
	for _, key := range []string{"state", "model", "questions"} {
		t.Run(key, func(t *testing.T) {
			calls := 0
			client := newDeterministicClient(t, func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("unexpected transport call")
			})
			_, err := client.SystemOne(t.Context(), &SystemOneParams{
				State:     "state",
				Questions: Questions{"question": Noul{}},
				ExtraBody: map[string]JSONValue{key: true},
			})
			if err == nil || !strings.Contains(err.Error(), `ExtraBody cannot override the reserved field`) {
				t.Fatalf("SystemOne error = %v, want reserved-field error", err)
			}
			if calls != 0 {
				t.Fatalf("transport calls = %d, want 0", calls)
			}
		})
	}
}

func TestSecurityResponseLimitIsNotRetried(t *testing.T) {
	attempts := 0
	client := newDeterministicClient(t, func(req *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("{}")),
			Request:    req,
		}, nil
	}, WithMaxResponseBodySize(1))
	_, err := client.Models.List(t.Context(), nil)
	var tooLarge *ResponseTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("Models.List error = %v, want ResponseTooLargeError", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestSecurityAPIKeyNeverAppearsInWireLogs(t *testing.T) {
	const apiKey = "sk-super-secret-key"
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Authorization", apiKey)
		writeJSON(w, http.StatusOK, resultBody)
	})
	for _, mode := range []LogBodyMode{LogBodyOff, LogBodyRedacted, LogBodyFull} {
		t.Run(string(mode), func(t *testing.T) {
			var logs bytes.Buffer
			swapLogger(t, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			if err := SetLogBodyMode(mode); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = SetLogBodyMode(LogBodyOff) })
			client, err := NewClient(
				WithAPIKey(apiKey),
				WithBaseURL(server.URL),
				WithAllowInsecureHTTP(),
			)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			t.Cleanup(client.Close)
			if _, err := systemOneCall(t, client, "safe state", Questions{"q": Noul{}}); err != nil {
				t.Fatalf("SystemOne: %v", err)
			}
			if strings.Contains(logs.String(), apiKey) {
				t.Fatalf("API key leaked in %s logs: %s", mode, logs.String())
			}
		})
	}
}

func TestSecuritySensitiveBodiesOnlyAppearInFullWireLogs(t *testing.T) {
	const sensitiveState = "SSN 123-45-6789"
	const sensitiveAnswer = "leaked-answer-value"
	responseBody := strings.Replace(resultBody, "friendly", sensitiveAnswer, 1)
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, responseBody)
	})
	for _, mode := range []LogBodyMode{LogBodyOff, LogBodyRedacted, LogBodyFull} {
		t.Run(string(mode), func(t *testing.T) {
			var logs bytes.Buffer
			swapLogger(t, slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			if err := SetLogBodyMode(mode); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = SetLogBodyMode(LogBodyOff) })
			client, err := NewClient(
				WithAPIKey("test-key"),
				WithBaseURL(server.URL),
				WithAllowInsecureHTTP(),
			)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			t.Cleanup(client.Close)
			if _, err := systemOneCall(t, client, sensitiveState, Questions{"q": Noul{}}); err != nil {
				t.Fatalf("SystemOne: %v", err)
			}
			logged := logs.String()
			if mode == LogBodyFull {
				for _, secret := range []string{sensitiveState, sensitiveAnswer} {
					if !strings.Contains(logged, secret) {
						t.Errorf("full logs do not contain %q: %s", secret, logged)
					}
				}
			} else {
				for _, secret := range []string{sensitiveState, sensitiveAnswer} {
					if strings.Contains(logged, secret) {
						t.Errorf("%s logs contain %q: %s", mode, secret, logged)
					}
				}
			}
		})
	}
}

func TestSecurityOversizedResponseRejectedByClient(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, strings.Repeat("x", 64))
	})
	client, err := NewClient(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithAllowInsecureHTTP(),
		WithMaxResponseBodySize(32),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()
	_, err = client.Models.List(t.Context(), nil)
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("Models.List error = %v, want ErrResponseTooLarge", err)
	}
}

func TestSecurityProtectedHeadersCannotBeSpoofed(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(authorizationHeader); got != "Bearer real-key" {
			t.Errorf("Authorization = %q, want SDK value", got)
		}
		if got := r.Header.Get(userAgentHeader); got != sdkName+"/"+Version {
			t.Errorf("User-Agent = %q, want SDK value", got)
		}
		if got := r.Header.Get(sdkHeader); got != sdkName+"/"+Version {
			t.Errorf("X-TypeSafe-SDK = %q, want SDK value", got)
		}
		writeJSON(w, http.StatusOK, `{"models":[]}`)
	})
	client, err := NewClient(
		WithAPIKey("real-key"),
		WithBaseURL(server.URL),
		WithAllowInsecureHTTP(),
		WithHeaders(map[string]string{
			"authorization":  "Bearer spoofed",
			"user-agent":     "spoofed",
			"x-typesafe-sdk": "spoofed",
		}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()
	_, err = client.Models.List(t.Context(), &ModelsListParams{
		ExtraHeaders: map[string]string{
			"Authorization":  "Bearer per-call-spoof",
			"User-Agent":     "per-call-spoof",
			"X-TypeSafe-SDK": "per-call-spoof",
		},
	})
	if err != nil {
		t.Fatalf("Models.List: %v", err)
	}
}

func TestSecurityRedirectDoesNotLeakAuthorization(t *testing.T) {
	redirectTarget := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("redirect target received a request")
	})
	source := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", redirectTarget.URL+"/v1/models")
		writeJSON(w, http.StatusFound, `{}`)
	})
	client, err := NewClient(
		WithAPIKey("real-key"),
		WithBaseURL(source.URL),
		WithAllowInsecureHTTP(),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()
	_, err = client.Models.List(t.Context(), nil)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusFound {
		t.Fatalf("Models.List error = %T %v, want 302 APIError", err, err)
	}
}

func TestSecurityCustomHTTPClientWorksWithValidationAndLimits(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, `{"models":[]}`)
	})
	calls := 0
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return http.DefaultTransport.RoundTrip(req)
	})
	client, err := NewClient(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithAllowInsecureHTTP(),
		WithHTTPClient(&http.Client{Transport: transport}),
		WithMaxResponseBodySize(32),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()
	if _, err := client.Models.List(t.Context(), nil); err != nil {
		t.Fatalf("Models.List: %v", err)
	}
	if calls != 1 {
		t.Fatalf("RoundTrip calls = %d, want 1", calls)
	}
}
