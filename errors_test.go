package typesafe

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestExtractMessageMatrix(t *testing.T) {
	tests := []struct {
		name string
		body any
		want string
	}{
		{"whole body is a string", "plain text", "plain text"},
		{"empty string body", "", ""},
		{"non-container bodies", 42.0, ""},
		{"error string", map[string]any{"error": "error", "message": "message", "detail": "detail"}, "error"},
		{"error object message", map[string]any{"error": map[string]any{"message": "nested error"}, "message": "message"}, "nested error"},
		{"message string", map[string]any{"message": "message", "detail": "detail"}, "message"},
		// Duplicate object names collapse last-wins through the lenient
		// decoder (v1 and Python json.loads semantics; guards contracts.md §2
		// message-extraction precedence against a strict-parser regression).
		{"duplicate message names, last wins", decodeJSONLenient([]byte(`{"message":"first","message":"second"}`)), "second"},
		{"detail string", map[string]any{"detail": "detail"}, "detail"},
		{"detail object message", map[string]any{"detail": map[string]any{"message": "nested detail"}}, "nested detail"},
		{
			"fastapi detail list",
			map[string]any{
				"detail": []any{
					map[string]any{"loc": []any{"body", "questions", "q", "score", "criteria", 0.0}, "msg": "Invalid"},
					map[string]any{"msg": "Missing"},
					map[string]any{},
				},
			},
			"questions.q.score.criteria.0: Invalid; Missing",
		},
		{"detail list without usable entries", map[string]any{"detail": []any{nil, 42.0, map[string]any{"msg": 4.0}}}, ""},
		{"unknown fields", map[string]any{"unexpected": true}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractMessage(tt.body); got != tt.want {
				t.Errorf("extractMessage(%v) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

func TestDeriveAPIMessage(t *testing.T) {
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'x'
	}
	tests := []struct {
		name string
		body string // raw wire body
		want string
	}{
		{"empty body", "", "status code (no body)"},
		{"null body", "null", "status code (no body)"},
		{"empty string body renders empty message", `""`, ""},
		{"array body", "[]", "[]"},
		{"number body", "42", "42"},
		{"invalid utf-8 body", "not JSON: \xff", "not JSON: \ufffd"},
		{"invalid utf-8 inside a string literal keeps the raw body", `{"x": "` + "\xff" + `"}`, "{\"x\": \"\ufffd\"}"},
		{"long plain text is not truncated", string(long), string(long)},
		{"long unstructured body truncated", `{"unknown":"` + string(long) + `"}`, `{"unknown":"` + string(long[:188]) + "…"},
		{"empty error string falls back to raw body", `{"error":"","message":"ignored"}`, `{"error":"","message":"ignored"}`},
		{"invalid detail entries fall back to raw body", `{"detail":[null,42,{"msg":4}]}`, `{"detail":[null,42,{"msg":4}]}`},
		{"message key", `{"message":"slow down"}`, "slow down"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveAPIMessage(decodeJSONLenient([]byte(tt.body)), tt.body)
			if got != tt.want {
				t.Errorf("deriveAPIMessage(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name    string
		headers http.Header
		want    *float64
	}{
		{"no headers", http.Header{}, nil},
		{"unparseable", header("Retry-After", "bad"), nil},
		{"negative retry-after", header("Retry-After", "-1"), nil},
		{"nan retry-after-ms falls through", header2("Retry-After-Ms", "NaN", "Retry-After", "1.5"), f64ptr(1500)},
		{"negative retry-after-ms falls through", header2("Retry-After-Ms", "-1", "Retry-After", "2"), f64ptr(2000)},
		{"empty retry-after counts as zero", header("Retry-After", ""), f64ptr(0)},
		{"infinite retry-after-ms", header("Retry-After-Ms", "inf"), nil},
		{"unparseable retry-after-ms falls through", header2("Retry-After-Ms", "bad", "Retry-After", "2"), f64ptr(2000)},
		{"overflowing retry-after", header("Retry-After", "1e308"), nil},
		{"seconds", header("Retry-After", "2"), f64ptr(2000)},
		{"milliseconds", header("Retry-After-Ms", "125"), f64ptr(125)},
		{"ms wins over seconds", header2("Retry-After-Ms", "0", "Retry-After", "50"), f64ptr(0)},
		{"long retry-after still parses", header("Retry-After", "60"), f64ptr(60000)},
		{"hex float seconds rejected", header("Retry-After", "0x1p4"), nil},
		{"hex float ms rejected", header("Retry-After-Ms", "0x1p4"), nil},
		{"hex float ms falls through to seconds", header2("Retry-After-Ms", "0x1p4", "Retry-After", "2"), f64ptr(2000)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRetryAfter(tt.headers)
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("parseRetryAfter = %v, want nil", *got)
			case tt.want != nil && got == nil:
				t.Errorf("parseRetryAfter = nil, want %v", *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Errorf("parseRetryAfter = %v, want %v", *got, *tt.want)
			}
		})
	}
}

func TestParseRetryAfterHTTPDate(t *testing.T) {
	future := time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)
	past := time.Now().Add(-10 * time.Second).UTC().Format(http.TimeFormat)

	if got := parseRetryAfter(header("Retry-After", future)); got == nil || math.Abs(*got-10000) > 3000 {
		t.Errorf("future date = %v, want ~10000ms", val(got))
	}
	if got := parseRetryAfter(header("Retry-After", past)); got == nil || *got != 0 {
		t.Errorf("past date = %v, want 0", val(got))
	}
}

func TestAPIErrorFormatting(t *testing.T) {
	t.Run("full format", func(t *testing.T) {
		err := apiErrorFor(429, `{"message":"Too many requests"}`, decodeJSONLenient([]byte(`{"message":"Too many requests"}`)),
			header2("X-Typesafe-Request-Id", "req_123", "Retry-After-Ms", "125"),
			"POST https://api.typesafe.ai/v1/systemone")
		want := "POST https://api.typesafe.ai/v1/systemone: 429 Too many requests (request_id=req_123)"
		if err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
	})

	t.Run("parts omitted when absent", func(t *testing.T) {
		err := apiErrorFor(400, `{"message":"Bad request"}`, decodeJSONLenient([]byte(`{"message":"Bad request"}`)), http.Header{}, "")
		want := "400 Bad request"
		if err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
	})

	t.Run("empty message renders bare status", func(t *testing.T) {
		base := newAPIError(429, "", nil, http.Header{}, "")
		base.message = ""
		err := &RateLimitError{APIError: base}
		if err.Error() != "429" {
			t.Errorf("Error() = %q, want %q", err.Error(), "429")
		}
	})

	t.Run("empty-string body renders bare status", func(t *testing.T) {
		err := apiErrorFor(400, `""`, decodeJSONLenient([]byte(`""`)), http.Header{}, "")
		if err.Error() != "400" {
			t.Errorf("Error() = %q, want %q", err.Error(), "400")
		}
	})

	t.Run("present-but-empty request id still rendered", func(t *testing.T) {
		err := apiErrorFor(400, `{"message":"Bad request"}`, decodeJSONLenient([]byte(`{"message":"Bad request"}`)), header("X-Typesafe-Request-Id", ""), "")
		want := "400 Bad request (request_id=)"
		if err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
	})
}

func TestDecodedBodyExposed(t *testing.T) {
	body := `{"detail": {"message": "boom"}}`
	err := apiErrorFor(422, body, decodeJSONLenient([]byte(body)), http.Header{}, "")
	decoded, ok := err.(*UnprocessableEntityError).DecodedBody.(map[string]any)
	if !ok {
		t.Fatalf("DecodedBody = %T, want map[string]any", err.(*UnprocessableEntityError).DecodedBody)
	}
	if _, ok := decoded["detail"]; !ok {
		t.Error("DecodedBody should carry the parsed body like the Python SDK's body attribute")
	}
	if got := err.(*UnprocessableEntityError).Body; got != body {
		t.Errorf("Body = %q, want the raw wire text", got)
	}

	empty := apiErrorFor(400, "", decodeJSONLenient(nil), http.Header{}, "")
	if empty.(*BadRequestError).DecodedBody != nil {
		t.Error("DecodedBody should be nil when the body is absent")
	}
}

func TestStatusErrorMapping(t *testing.T) {
	tests := []struct {
		status int
		typ    string
	}{
		{400, "*typesafe.BadRequestError"},
		{401, "*typesafe.AuthenticationError"},
		{403, "*typesafe.PermissionDeniedError"},
		{404, "*typesafe.NotFoundError"},
		{422, "*typesafe.UnprocessableEntityError"},
		{429, "*typesafe.RateLimitError"},
		{500, "*typesafe.InternalServerError"},
		{503, "*typesafe.InternalServerError"},
		{408, "*typesafe.APIError"},
		{409, "*typesafe.APIError"},
		{302, "*typesafe.APIError"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("status %d", tt.status), func(t *testing.T) {
			err := apiErrorFor(tt.status, `{}`, map[string]any{}, header("X-Typesafe-Request-Id", "req_123"), "GET https://api.typesafe.ai/v1/models")
			if got := fmt.Sprintf("%T", err); got != tt.typ {
				t.Fatalf("status %d mapped to %s, want %s", tt.status, got, tt.typ)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("every mapping should match *APIError via errors.As")
			}
			if apiErr.Status != tt.status || apiErr.RequestID != "req_123" {
				t.Errorf("unexpected error payload: %+v", apiErr)
			}
			if rateLimit, ok := err.(*RateLimitError); ok && rateLimit.RetryAfterMs != nil {
				t.Error("RetryAfterMs should be nil without retry-after headers")
			}
		})
	}
}

func TestRateLimitErrorCarriesRetryAfter(t *testing.T) {
	err := apiErrorFor(429, `{}`, map[string]any{}, header("Retry-After-Ms", "125"), "")
	rateLimit := err.(*RateLimitError)
	if rateLimit.RetryAfterMs == nil || *rateLimit.RetryAfterMs != 125 {
		t.Errorf("RetryAfterMs = %v, want 125", val(rateLimit.RetryAfterMs))
	}
}

func TestErrorUnwrapChains(t *testing.T) {
	t.Run("rate limit matches specific and base", func(t *testing.T) {
		err := apiErrorFor(429, `{}`, map[string]any{}, http.Header{}, "")
		var specific *RateLimitError
		var base *APIError
		if !errors.As(err, &specific) || !errors.As(err, &base) {
			t.Error("RateLimitError should match both *RateLimitError and *APIError")
		}
	})

	t.Run("connection error preserves cause", func(t *testing.T) {
		cause := errors.New("dial tcp: refused")
		err := newConnectionError(cause)
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatal("should match *ConnectionError")
		}
		if !errors.Is(err, cause) && errors.Unwrap(err) == nil {
			t.Error("cause should be preserved")
		}
		if err.Error() != "Connection error: dial tcp: refused" {
			t.Errorf("Error() = %q", err.Error())
		}
	})

	t.Run("timeout error satisfies net.Error", func(t *testing.T) {
		err := newTimeoutError(1250*time.Millisecond, errors.New("read timeout"))
		var netErr net.Error
		if !errors.As(err, &netErr) {
			t.Fatal("TimeoutError should satisfy net.Error")
		}
		if !netErr.Timeout() {
			t.Error("Timeout() should hold")
		}
		want := "Request timed out (timeout=1.25s)."
		if err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
		var timeoutErr *TimeoutError
		if !errors.As(err, &timeoutErr) || timeoutErr.Duration != 1250*time.Millisecond {
			t.Error("duration should be carried")
		}
		// Temporary() is deprecated on net.Error (SA1019); assert it on the
		// concrete type, which still honors the transient-timeout contract.
		if timeoutErr == nil || !timeoutErr.Temporary() {
			t.Error("Temporary() should hold")
		}
		// Like Python (timeout subclasses connection), a timeout also matches
		// the connection error type, and the cause survives the chain.
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Error("TimeoutError should also match *ConnectionError")
		}
		if unwrapped := errors.Unwrap(errors.Unwrap(err)); unwrapped == nil || unwrapped.Error() != "read timeout" {
			t.Error("transport cause should survive the unwrap chain")
		}
	})

	t.Run("sentinels wrap through TypeSafeError", func(t *testing.T) {
		// resolveConfig falls back to the environment, so a developer with a
		// real key exported must still see the missing-key path.
		t.Setenv("TYPESAFE_API_KEY", "")
		_, err := resolveConfig("", "", "", 0, nil)
		if !errors.Is(err, ErrMissingAPIKey) {
			t.Error("missing key error should wrap ErrMissingAPIKey")
		}
		client := &Client{}
		client.closed.Store(true)
		if err := client.checkClosed(); !errors.Is(err, ErrClientClosed) {
			t.Error("closed error should wrap ErrClientClosed")
		}
	})

	t.Run("joined trees match by type", func(t *testing.T) {
		// errors.As semantics: wrapping and joined errors both match the
		// nested type.
		joined := errors.Join(errors.New("outer"), apiErrorFor(429, `{}`, map[string]any{}, http.Header{}, ""))
		var rateLimit *RateLimitError
		if !errors.As(joined, &rateLimit) {
			t.Error("a joined error should match the nested *RateLimitError")
		}
	})
}

// TestErrorsShareTypeSafeRoot: like the Python SDK's TypeSafeError base class,
// the *TypeSafeError root matches every SDK error — each HTTP error subclass,
// the unmapped APIError, response validation, connection, and timeout
// failures — so one catch-all handler sees them all. Errors from outside the
// SDK (including caller cancellation) never match the root.
func TestErrorsShareTypeSafeRoot(t *testing.T) {
	validation := validationErrorFor(
		&http.Response{StatusCode: http.StatusOK, Header: http.Header{}},
		[]byte(resultBody), "answers.tone.confidence")
	// resolveConfig falls back to the environment, so a developer with a real
	// key exported must still reach the missing-key error.
	t.Setenv("TYPESAFE_API_KEY", "")
	_, missingKey := resolveConfig("", "", "", 0, nil)

	for name, err := range map[string]error{
		"bad request":           apiErrorFor(400, `{"message":"bad"}`, map[string]any{"message": "bad"}, http.Header{}, ""),
		"authentication":        apiErrorFor(401, `{}`, map[string]any{}, http.Header{}, ""),
		"permission denied":     apiErrorFor(403, `{}`, map[string]any{}, http.Header{}, ""),
		"not found":             apiErrorFor(404, `{}`, map[string]any{}, http.Header{}, ""),
		"unprocessable entity":  apiErrorFor(422, `{}`, map[string]any{}, http.Header{}, ""),
		"rate limit":            apiErrorFor(429, `{}`, map[string]any{}, http.Header{}, ""),
		"internal server error": apiErrorFor(500, `{}`, map[string]any{}, http.Header{}, ""),
		"unmapped APIError":     apiErrorFor(409, `{}`, map[string]any{}, http.Header{}, ""),
		"response validation":   validation,
		"connection":            newConnectionError(errors.New("dial tcp: refused")),
		"timeout":               newTimeoutError(1250*time.Millisecond, errors.New("read timeout")),
		"sdk-side failure":      newTypeSafeError("bad params"),
		"missing key":           missingKey,
	} {
		var root *TypeSafeError
		if !errors.As(err, &root) {
			t.Errorf("%s should match the shared *TypeSafeError root", name)
			continue
		}
		if root.Error() != err.Error() {
			t.Errorf("%s: root message %q should mirror the error's own %q", name, root.Error(), err.Error())
		}
	}

	// The root also surfaces through user-built wrapping and joins.
	for name, err := range map[string]error{
		"wrapped SDK error": fmt.Errorf("app layer: %w", apiErrorFor(429, `{}`, map[string]any{}, http.Header{}, "")),
		"joined SDK error":  errors.Join(errors.New("outer"), newConnectionError(errors.New("y"))),
	} {
		var root *TypeSafeError
		if !errors.As(err, &root) {
			t.Errorf("%s should match the shared *TypeSafeError root", name)
		}
	}

	for name, err := range map[string]error{
		"plain error":       errors.New("outside the SDK"),
		"cancellation":      context.Canceled,
		"deadline exceeded": context.DeadlineExceeded,
		"wrapped plain":     fmt.Errorf("app layer: %w", errors.New("x")),
	} {
		var root *TypeSafeError
		if errors.As(err, &root) {
			t.Errorf("%s must not acquire the SDK root", name)
		}
	}

	// The specific-type matches, cause identity, and net.Error interface all
	// survive the root addition.
	timeout := newTimeoutError(time.Second, errors.New("read timeout"))
	var connErr *ConnectionError
	var netErr net.Error
	var timeoutErr *TimeoutError
	if !errors.As(timeout, &connErr) || !errors.As(timeout, &netErr) || !errors.As(timeout, &timeoutErr) {
		t.Error("timeout must keep matching *ConnectionError, net.Error, and *TimeoutError")
	}
	if !errors.Is(timeout, errors.Unwrap(errors.Unwrap(timeout))) {
		t.Error("transport-cause identity should be preserved")
	}
}

// --- small helpers ----------------------------------------------------------

func header(name, value string) http.Header {
	return http.Header{http.CanonicalHeaderKey(name): []string{value}}
}

func header2(name1, value1, name2, value2 string) http.Header {
	return http.Header{
		http.CanonicalHeaderKey(name1): []string{value1},
		http.CanonicalHeaderKey(name2): []string{value2},
	}
}

func f64ptr(value float64) *float64 { return &value }

func val(p *float64) float64 {
	if p == nil {
		return math.NaN()
	}
	return *p
}

func TestDuplicateHeadersJoined(t *testing.T) {
	// Like the Python client's header lookup, duplicate lines are comma-
	// joined, so conflicting values fail to parse instead of silently
	// honoring the first.
	headers := http.Header{"Retry-After": []string{"5", "3"}}
	if got := parseRetryAfter(headers); got != nil {
		t.Errorf("parseRetryAfter(duplicates) = %v, want nil", *got)
	}
	if raw, present := headerValue(headers, "Retry-After"); !present || raw != "5, 3" {
		t.Errorf("headerValue = %q, %v; want \"5, 3\", true", raw, present)
	}
}
