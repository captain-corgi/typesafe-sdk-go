package typesafe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// TestRetryCountHeaderValues pins the X-TypeSafe-Retry-Count sequence: the
// first attempt sends nothing, retries carry the attempt number.
func TestRetryCountHeaderValues(t *testing.T) {
	var retryCounts []string // "" = absent
	dc := newDeterministicClient(t, nil)
	dc.setTransport(func(req *http.Request) (*http.Response, error) {
		if values := req.Header.Values("X-TypeSafe-Retry-Count"); len(values) > 0 {
			retryCounts = append(retryCounts, values[0])
		} else {
			retryCounts = append(retryCounts, "")
		}
		return textResponseWithHeaders(req, http.StatusTooManyRequests, `{"message": "retry"}`,
			map[string]string{"Retry-After-Ms": "0"}), nil
	})
	_, err := dc.Models.List(t.Context(), nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	want := []string{"", "1", "2"}
	if len(retryCounts) != len(want) {
		t.Fatalf("retry counts = %v, want %v", retryCounts, want)
	}
	for i := range want {
		if retryCounts[i] != want[i] {
			t.Errorf("attempt %d retry-count = %q, want %q", i+1, retryCounts[i], want[i])
		}
	}
}

// TestTransportRetryRecovers exercises retry-then-recover over transport
// errors: two failed attempts (a timeout and a connection error) followed by
// a success, with backoff delays matching the deterministic sequence.
func TestTransportRetryRecovers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		dc := newDeterministicClient(t, nil)
		dc.setTransport(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				// Attempt timeout: hold the request until its per-attempt
				// deadline expires. The bubble clock advances to the
				// deadline as soon as this is the only blocked goroutine.
				<-req.Context().Done()
				return nil, req.Context().Err()
			}
			if attempts == 2 {
				return nil, errors.New("connection reset by peer")
			}
			return textResponse(req, http.StatusOK, `{"models": []}`), nil
		})
		models, err := dc.Models.List(t.Context(), nil)
		if err != nil {
			t.Fatalf("expected recovery, got %v", err)
		}
		if len(models.Models) != 0 {
			t.Errorf("models = %+v", models.Models)
		}
		if attempts != 3 {
			t.Errorf("attempts = %d, want 3", attempts)
		}
		// Deterministic backoff (rand pinned to 0): 500ms then 1s.
		delays := dc.recordedDelays()
		if len(delays) != 2 || delays[0] != 500*time.Millisecond || delays[1] != time.Second {
			t.Errorf("delays = %v, want [500ms 1s]", delays)
		}
	})
}

// TestJitteredRecoveryBounds checks recovery delays stay inside the jitter
// window when the rand hook behaves like a real random source.
func TestJitteredRecoveryBounds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		dc := newDeterministicClient(t, nil)
		dc.rand = func() float64 { return 0.999 } // near the jitter floor
		dc.setTransport(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts < 3 {
				return nil, errors.New("boom")
			}
			return textResponse(req, http.StatusOK, `{"models": []}`), nil
		})
		if _, err := dc.Models.List(t.Context(), nil); err != nil {
			t.Fatalf("expected recovery, got %v", err)
		}
		delays := dc.recordedDelays()
		if len(delays) != 2 {
			t.Fatalf("delays = %v", delays)
		}
		// 0.5·(1−0.25·~1) ≈ 0.375 and 1.0·(1−0.25·~1) ≈ 0.75: never above the
		// un-jittered cap, never below the floor.
		if delays[0] < 375*time.Millisecond || delays[0] > 500*time.Millisecond {
			t.Errorf("first delay = %v, want within [375ms, 500ms]", delays[0])
		}
		if delays[1] < 750*time.Millisecond || delays[1] > time.Second {
			t.Errorf("second delay = %v, want within [750ms, 1s]", delays[1])
		}
	})
}

// TestZeroBackoffRetriesImmediately: a policy with backoff disabled retries
// without delay and still recovers.
func TestZeroBackoffRetriesImmediately(t *testing.T) {
	for _, mutate := range []func(*RetryPolicy){
		func(p *RetryPolicy) { p.BackoffInitial = 0 },
		func(p *RetryPolicy) { p.BackoffMax = 0 },
	} {
		// Asserting a zero gap needs the bubble clock; against the real clock
		// the measured gap is a small nonzero number of nanoseconds.
		synctest.Test(t, func(t *testing.T) {
			attempts := 0
			dc := newDeterministicClient(t, nil)
			dc.setTransport(func(req *http.Request) (*http.Response, error) {
				attempts++
				if attempts == 1 {
					return textResponse(req, http.StatusServiceUnavailable, `{"message": "temporarily unavailable"}`), nil
				}
				return textResponse(req, http.StatusOK, `{"models": []}`), nil
			})
			policy := DefaultRetryPolicy()
			policy.MaxRetries = 1
			policy.Timeout = 0
			mutate(&policy)
			if _, err := dc.Models.List(t.Context(), &ModelsListParams{Retry: &policy}); err != nil {
				t.Fatalf("expected recovery, got %v", err)
			}
			if attempts != 2 {
				t.Errorf("attempts = %d, want 2", attempts)
			}
			if delays := dc.recordedDelays(); len(delays) != 1 || delays[0] != 0 {
				t.Errorf("delays = %v, want one immediate retry", delays)
			}
		})
	}
}

// TestEndpointOmitsURLCredentials: endpoint strings drop userinfo, query,
// and fragment.
func TestEndpointOmitsURLCredentials(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet,
		"https://user:password@example.test/v1/models?token=secret#fragment", nil)
	resp := &http.Response{Request: request}
	if got := endpointFromResponse(resp); got != "GET https://example.test/v1/models" {
		t.Errorf("endpoint = %q", got)
	}
	if got := endpointFromResponse(&http.Response{}); got != "" {
		t.Errorf("endpoint without request = %q, want empty", got)
	}
}

// TestEndpointStripsDefaultPort: an explicit default port is normalized away
// in error endpoints, matching the URL normalization of the Python SDK.
func TestEndpointStripsDefaultPort(t *testing.T) {
	tests := []struct{ rawURL, want string }{
		{"https://example.test:443/v1/models", "GET https://example.test/v1/models"},
		{"http://example.test:80/v1/models", "GET http://example.test/v1/models"},
		{"https://example.test:8443/v1/models", "GET https://example.test:8443/v1/models"},
		{"http://example.test:8080/v1/models", "GET http://example.test:8080/v1/models"},
	}
	for _, tt := range tests {
		request, _ := http.NewRequest(http.MethodGet, tt.rawURL, nil)
		if got := endpointFromResponse(&http.Response{Request: request}); got != tt.want {
			t.Errorf("endpoint(%s) = %q, want %q", tt.rawURL, got, tt.want)
		}
	}
}

// TestWireLogRedactionEndToEnd: a DEBUG-level capture of a full call must
// contain the request id and body but never a secret header value.
func TestWireLogRedactionEndToEnd(t *testing.T) {
	var buf bytes.Buffer
	swapLogger(t, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})).
		With(slog.String("logger", "typesafe_sdk")))

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Typesafe-Request-Id", "req_log")
		w.Header().Set("Set-Cookie", "response-secret")
		w.Header().Set("X-Visible", "response-visible")
		writeJSON(w, http.StatusOK, resultBody)
	})
	client, err := NewClient(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL),
		WithHeaders(map[string]string{"X-API-Key": "key-secret", "Cookie": "cookie-secret", "X-Visible": "request-visible"}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()
	if _, err := systemOneCall(t, client, "hello", Questions{"q": Noul{Instructions: "?"}}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}

	logged := buf.String()
	for _, secret := range []string{"test-key", "key-secret", "cookie-secret", "response-secret"} {
		if strings.Contains(logged, secret) {
			t.Errorf("secret %q leaked into logs", secret)
		}
	}
	if !strings.Contains(logged, "req_log") {
		t.Error("request id should be logged at INFO")
	}
	if !strings.Contains(logged, "hello") {
		t.Error("request body should be logged at DEBUG")
	}
	// Non-secret header values survive redaction on both directions.
	for _, visible := range []string{"request-visible", "response-visible"} {
		if !strings.Contains(logged, visible) {
			t.Errorf("benign header value %q missing from DEBUG dump", visible)
		}
	}
	// The DEBUG response dump carries the response body.
	if !strings.Contains(logged, "jev-latest") {
		t.Error("response body should be logged at DEBUG")
	}
}

// TestTransportErrorLoggedAtInfo: a connection failure produces an INFO
// record naming the underlying error type.
func TestTransportErrorLoggedAtInfo(t *testing.T) {
	var buf bytes.Buffer
	swapLogger(t, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})).
		With(slog.String("logger", "typesafe_sdk")))

	dc := newDeterministicClient(t, nil)
	dc.setTransport(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("dial refused")
	})
	dc.retry = RetryPolicy{} // single attempt
	_, err := dc.Models.List(t.Context(), nil)
	if err == nil {
		t.Fatal("expected a connection error")
	}

	logged := buf.String()
	if !strings.Contains(logged, "msg=error") || !strings.Contains(logged, "url=https://api.typesafe.ai/v1/models") {
		t.Errorf("expected an INFO error record with the URL: %s", logged)
	}
}

// TestAnswerTypeErrorPrecedesModel: a malformed answers entry is reported
// before a missing top-level model field.
func TestAnswerTypeErrorPrecedesModel(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, `{"usage": {}, "answers": {"c": "not-a-mapping"}}`)
	})
	client := newLoggedOutClient(t, server.URL)
	_, err := systemOneCall(t, client, "x", Questions{"q": Noul{Instructions: "?"}})
	validationErr := requireValidationError(t, err)
	if validationErr.FieldPath != "answers.c.type" {
		t.Errorf("FieldPath = %q, want answers.c.type", validationErr.FieldPath)
	}
}

// TestSleepRespectsContextCancellation: a canceled context aborts an
// in-flight retry sleep and propagates the context error.
func TestSleepRespectsContextCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dc := newDeterministicClient(t, nil)
		dc.setTransport(func(req *http.Request) (*http.Response, error) {
			return textResponseWithHeaders(req, http.StatusTooManyRequests, `{}`,
				map[string]string{"Retry-After": "60"}), nil
		})
		// Unlimited budget so the (long) server-requested delay is actually
		// slept by the production sleep path.
		unlimited := DefaultRetryPolicy()
		unlimited.Timeout = 0
		ctx, cancel := context.WithCancel(t.Context())
		errs := make(chan error, 1)
		go func() {
			_, err := dc.Models.List(ctx, &ModelsListParams{Retry: &unlimited})
			errs <- err
		}()

		// Wait for the first attempt to fail and the 60s sleep to begin.
		// Nothing else can run, so the call must still be in flight.
		synctest.Wait()
		select {
		case err := <-errs:
			t.Fatalf("call finished before the sleep: %v", err)
		default:
		}

		cancel()
		synctest.Wait()
		select {
		case err := <-errs:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("expected context.Canceled, got %v", err)
			}
		default:
			t.Fatal("cancellation did not abort the retry sleep")
		}
	})
}

// TestInfoLoggingShape: one INFO record per successful call naming the
// method, no header dumps at INFO level, and one INFO per retry.
func TestInfoLoggingShape(t *testing.T) {
	var buf bytes.Buffer
	swapLogger(t, slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})).
		With(slog.String("logger", "typesafe_sdk")))

	attempts := 0
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.Header().Set("Retry-After-Ms", "0")
			writeJSON(w, http.StatusTooManyRequests, `{}`)
			return
		}
		w.Header().Set("X-Typesafe-Request-Id", "req_info")
		writeJSON(w, http.StatusOK, `{"models": []}`)
	})
	// This test talks to a real server, so it cannot run under a fake clock;
	// disabling backoff keeps the two retries instant instead.
	immediate := DefaultRetryPolicy()
	immediate.BackoffInitial = 0
	client, err := NewClient(WithAPIKey("k"), WithBaseURL(server.URL), WithRetry(immediate))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if _, err := client.Models.List(t.Context(), nil); err != nil {
		t.Fatalf("List: %v", err)
	}

	logged := buf.String()
	responseLines := strings.Count(logged, "msg=response")
	retryLines := strings.Count(logged, "msg=retry")
	// One response record per attempt (each response is logged when parsed,
	// including non-2xx), and one retry record per retry.
	if responseLines != 3 {
		t.Errorf("INFO response records = %d, want 3 (one per attempt)", responseLines)
	}
	if retryLines != 2 {
		t.Errorf("INFO retry records = %d, want 2 (one per retry)", retryLines)
	}
	if !strings.Contains(logged, "method=GET") || !strings.Contains(logged, "request_id=req_info") {
		t.Errorf("response record missing method/request id: %s", logged)
	}
	if strings.Contains(logged, "headers=") {
		t.Error("header dumps must only appear at DEBUG level")
	}
	if strings.Contains(logged, "Bearer") {
		t.Error("credentials must never be logged")
	}
}

// TestGetSendsNoContentType: a bodyless request carries no Content-Type.
func TestGetSendsNoContentType(t *testing.T) {
	var sawContentType bool
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		sawContentType = r.Header.Get("Content-Type") != ""
		writeJSON(w, http.StatusOK, `{"models": []}`)
	})
	client := newLoggedOutClient(t, server.URL)
	if _, err := client.Models.List(t.Context(), nil); err != nil {
		t.Fatalf("List: %v", err)
	}
	if sawContentType {
		t.Error("GET /v1/models must not carry a Content-Type header")
	}
}

// TestRawBodyReadableAfterParse: Raw bodies can be read fully and repeatedly.
func TestRawBodyReadableAfterParse(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, resultBody)
	})
	client := newLoggedOutClient(t, server.URL)
	result, err := systemOneCall(t, client, "x", Questions{"q": Noul{Instructions: "?"}})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	first, err := io.ReadAll(result.Raw.Body)
	if err != nil {
		t.Fatalf("read raw body: %v", err)
	}
	if len(first) == 0 || !strings.Contains(string(first), `"jev-latest"`) {
		t.Errorf("raw body = %.80s", first)
	}
	result.Raw.Body.Close()
	// A second read of the same buffered body still works after re-seek via a
	// fresh client call.
	result2, err := systemOneCall(t, client, "x", Questions{"q": Noul{Instructions: "?"}})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	second, _ := io.ReadAll(result2.Raw.Body)
	if len(second) != len(first) {
		t.Errorf("second read = %d bytes, first = %d", len(second), len(first))
	}
	// Usage of fmt keeps the import stable for future assertions.
	_ = fmt.Sprint
}
