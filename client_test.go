package typesafe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestRoundTripWireBody(t *testing.T) {
	for _, form := range []string{"typed", "raw", "mixed"} {
		t.Run(form, func(t *testing.T) {
			criteria := ScoreCriteria{"bad", "ok", "great"}
			raw := Questions{
				"spam":    RawQuestion{"type": "noul", "instructions": "Spam?"},
				"tone":    RawQuestion{"type": "choice", "instructions": "Tone?", "criteria": map[string]any{"friendly": nil, "hostile": nil}},
				"quality": RawQuestion{"type": "score", "instructions": "Quality?", "criteria": []any{"bad", "ok", "great"}},
			}
			questions := raw
			switch form {
			case "typed":
				questions = Questions{
					"spam":    Noul{Instructions: "Spam?"},
					"tone":    Choice{Instructions: "Tone?", Criteria: ChoiceCriteria{"friendly": nil, "hostile": nil}},
					"quality": Score{Instructions: "Quality?", Criteria: criteria},
				}
			case "mixed":
				questions = Questions{
					"spam":    raw["spam"],
					"tone":    Choice{Instructions: "Tone?", Criteria: ChoiceCriteria{"friendly": nil, "hostile": nil}},
					"quality": Score{Instructions: "Quality?", Criteria: criteria},
				}
			}
			expected := map[string]any{
				"state": map[string]any{"document": "Hello 🌍"},
				"model": "jev-latest",
				"questions": map[string]any{
					"spam":    map[string]any{"type": "noul", "instructions": "Spam?"},
					"tone":    map[string]any{"type": "choice", "instructions": "Tone?", "criteria": map[string]any{"friendly": nil, "hostile": nil}},
					"quality": map[string]any{"type": "score", "instructions": "Quality?", "criteria": []any{"bad", "ok", "great"}},
				},
			}
			var gotPath, gotMethod string
			var gotBody map[string]any
			var gotContentType string
			server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath, gotMethod = r.URL.Path, r.Method
				data, _ := io.ReadAll(r.Body)
				gotBody = decodeJSONMap(t, data)
				gotContentType = r.Header.Get("Content-Type")
				writeJSON(w, http.StatusOK, resultBody)
			})
			client := newLoggedOutClient(t, server.URL)
			result, err := systemOneCall(t, client, map[string]any{"document": "Hello 🌍"}, questions)
			if err != nil {
				t.Fatalf("SystemOne: %v", err)
			}
			if gotMethod != http.MethodPost || gotPath != "/v1/systemone" {
				t.Errorf("request was %s %s", gotMethod, gotPath)
			}
			if gotContentType != "application/json" {
				t.Errorf("Content-Type = %q", gotContentType)
			}
			compareJSON(t, expected, gotBody)
			if result.Scores()["quality"].Score != 1.7 || result.Choices()["tone"].Confidence != 0.9 {
				t.Errorf("decoded answers wrong: %+v", result.Answers)
			}
		})
	}
}

func TestExtraBodyShallowOverride(t *testing.T) {
	expected := map[string]any{
		"state":      "hi",
		"model":      "override-model",
		"questions":  map[string]any{"q": map[string]any{"type": "noul", "instructions": "?"}},
		"beam_width": 4.0,
		"nullable":   nil,
	}
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		compareJSON(t, expected, decodeJSONMap(t, data))
		writeJSON(w, http.StatusOK, resultBody)
	})
	client := newLoggedOutClient(t, server.URL)
	_, err := client.SystemOne(t.Context(), &SystemOneParams{
		State:     "hi",
		Questions: Questions{"q": RawQuestion{"type": "noul", "instructions": "?"}},
		Model:     "call-model",
		ExtraBody: map[string]JSONValue{"model": "override-model", "beam_width": 4, "nullable": nil},
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
}

func TestUnserializableRequestBody(t *testing.T) {
	attempts := 0
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		writeJSON(w, http.StatusOK, resultBody)
	})
	client := newLoggedOutClient(t, server.URL)
	_, err := client.SystemOne(t.Context(), &SystemOneParams{
		State:     "x",
		Questions: Questions{"q": RawQuestion{"type": "noul"}},
		ExtraBody: map[string]JSONValue{"bad": make(chan int)},
	})
	var typeSafeErr *TypeSafeError
	if !errors.As(err, &typeSafeErr) || !strings.Contains(err.Error(), "could not be encoded as JSON") {
		t.Fatalf("expected an encoding error, got %v", err)
	}
	if attempts != 0 {
		t.Errorf("unencodable body reached the network (%d attempts)", attempts)
	}
}

func TestRawQuestionPassthrough(t *testing.T) {
	questions := Questions{
		"q":      RawQuestion{"type": "noul", "instructions": "Spam?", "weight": 3, "nested": map[string]any{"k": nil}},
		"choice": RawQuestion{"type": "choice", "criteria": map[string]any{"a": nil}, "weight": 2},
		"score":  RawQuestion{"type": "score", "criteria": []any{"good"}, "weight": 1},
	}
	expected := map[string]any{
		"state": "hi",
		"model": "jev-latest",
		"questions": map[string]any{
			"q":      map[string]any{"type": "noul", "instructions": "Spam?", "weight": 3.0, "nested": map[string]any{"k": nil}},
			"choice": map[string]any{"type": "choice", "criteria": map[string]any{"a": nil}, "weight": 2.0},
			"score":  map[string]any{"type": "score", "criteria": []any{"good"}, "weight": 1.0},
		},
	}
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		compareJSON(t, expected, decodeJSONMap(t, data))
		writeJSON(w, http.StatusOK, resultBody)
	})
	client := newLoggedOutClient(t, server.URL)
	if _, err := systemOneCall(t, client, "hi", questions); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
}

func TestRawQuestionSchemaValidationLeftToAPI(t *testing.T) {
	// Raw dictionaries with invalid inner shapes are sent untouched; the API
	// is the schema validator for the raw form.
	question := RawQuestion{"type": "noul", "instructions": 1}
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		got := decodeJSONMap(t, data)["questions"].(map[string]any)["q"]
		compareJSON(t, map[string]any{"type": "noul", "instructions": 1.0}, got)
		writeJSON(w, http.StatusUnprocessableEntity, `{"detail": "Invalid question"}`)
	})
	client := newLoggedOutClient(t, server.URL)
	_, err := systemOneCall(t, client, "x", Questions{"q": question})
	var unprocessable *UnprocessableEntityError
	if !errors.As(err, &unprocessable) {
		t.Fatalf("expected UnprocessableEntityError, got %v", err)
	}

	// A raw choice whose criteria is present but not a mapping also goes
	// through untouched (presence is the only local check).
	listCriteria := RawQuestion{"type": "choice", "criteria": []any{"invalid", "shape"}}
	server = newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		got := decodeJSONMap(t, data)["questions"].(map[string]any)["q"]
		compareJSON(t, map[string]any{"type": "choice", "criteria": []any{"invalid", "shape"}}, got)
		writeJSON(w, http.StatusUnprocessableEntity, `{"detail": "Invalid criteria"}`)
	})
	client = newLoggedOutClient(t, server.URL)
	_, err = systemOneCall(t, client, "x", Questions{"q": listCriteria})
	if !errors.As(err, &unprocessable) {
		t.Fatalf("expected UnprocessableEntityError, got %v", err)
	}
}

func TestRichDescriptions(t *testing.T) {
	criteria := map[string]any{"summary": "duplicated", "examples": []any{"charged twice"}}
	expected := map[string]any{
		"state": "a ticket",
		"model": "custom",
		"questions": map[string]any{
			"duplicate": map[string]any{"type": "noul", "instructions": map[string]any{"question": "Duplicate?"}, "criteria": map[string]any{"true": criteria}},
			"team":      map[string]any{"type": "choice", "instructions": "Team?", "criteria": map[string]any{"billing": criteria, "other": nil}},
			"risk":      map[string]any{"type": "score", "instructions": "Risk?", "criteria": []any{criteria}},
		},
	}
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		compareJSON(t, expected, decodeJSONMap(t, data))
		writeJSON(w, http.StatusOK, `{
		  "model": "custom",
		  "usage": {"input_tokens": 1, "output_tokens": 1},
		  "answers": {"risk": {"type": "score", "score": 0, "confidence": 1,
		              "legend": {"0": `+mustMarshal(t, criteria)+`}, "probabilities": {"0": 1}}}
		}`)
	})
	client := newLoggedOutClient(t, server.URL)
	result, err := client.SystemOne(t.Context(), &SystemOneParams{
		State: "a ticket",
		Model: "custom",
		Questions: Questions{
			"duplicate": RawQuestion{"type": "noul", "instructions": map[string]any{"question": "Duplicate?"}, "criteria": map[string]any{"true": criteria}},
			"team":      Choice{Instructions: "Team?", Criteria: ChoiceCriteria{"billing": criteria, "other": nil}},
			"risk":      Score{Instructions: "Risk?", Criteria: ScoreCriteria{criteria}},
		},
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if got := result.Scores()["risk"].Legend[0]; !mapsEqual(t, criteria, got) {
		t.Errorf("legend[0] = %v", got)
	}
}

func mapsEqual(t *testing.T, want map[string]any, got any) bool {
	t.Helper()
	gotMap, ok := got.(map[string]any)
	if !ok {
		return false
	}
	wantJSON, _ := marshalJSONCompact(want)
	gotJSON, _ := marshalJSONCompact(gotMap)
	return string(wantJSON) == string(gotJSON)
}

func TestErrorMappingExactStrings(t *testing.T) {
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
			server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Typesafe-Request-Id", "req_123")
				w.Header().Set("Retry-After-Ms", "125")
				writeJSON(w, tt.status, `{"detail": {"message": "Server explanation"}}`)
			})
			client := newLoggedOutClient(t, server.URL)
			_, err := client.Models.List(t.Context(), nil)
			if got := fmt.Sprintf("%T", err); got != tt.typ {
				t.Fatalf("mapped to %s, want %s", got, tt.typ)
			}
			want := fmt.Sprintf("GET %s/v1/models: %d Server explanation (request_id=req_123)", server.URL, tt.status)
			if err.Error() != want {
				t.Errorf("Error() = %q, want %q", err.Error(), want)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatal("should match *APIError")
			}
			if apiErr.Headers.Get("Retry-After-Ms") != "125" {
				t.Error("headers should be carried")
			}
			if rateLimit, ok := err.(*RateLimitError); ok && (rateLimit.RetryAfterMs == nil || *rateLimit.RetryAfterMs != 125) {
				t.Errorf("RetryAfterMs = %v, want 125", rateLimit.RetryAfterMs)
			}
		})
	}
}

func TestErrorMessagesThroughClient(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"error key wins", `{"error": "error", "message": "message", "detail": "detail"}`, "error"},
		{"nested error message", `{"error": {"message": "nested error"}, "message": "message"}`, "nested error"},
		{"message key", `{"message": "message", "detail": "detail"}`, "message"},
		{"detail string", `{"detail": "detail"}`, "detail"},
		{"nested detail message", `{"detail": {"message": "nested detail"}}`, "nested detail"},
		{
			"fastapi detail join",
			`{"detail": [{"loc": ["body", "questions", "q", "score", "criteria", 0], "msg": "Invalid"}, {"msg": "Missing"}, {}]}`,
			"questions.q.score.criteria.0: Invalid; Missing",
		},
		{"plain text body", `plain text`, "plain text"},
		{"unknown fields fall back to raw body", `{"unexpected": true}`, `{"unexpected":true}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusBadRequest, tt.body)
			})
			client := newLoggedOutClient(t, server.URL)
			_, err := client.Models.List(t.Context(), nil)
			want := fmt.Sprintf("GET %s/v1/models: 400 %s", server.URL, tt.want)
			if err == nil || err.Error() != want {
				t.Errorf("Error() = %q, want %q", err, want)
			}
		})
	}
}

func TestErrorBodyEdgeCases(t *testing.T) {
	long := strings.Repeat("x", 201)
	tests := []struct {
		name string
		body string
		want string
	}{
		{"empty body", "", "status code (no body)"},
		{"null body", "null", "status code (no body)"},
		{"array body", "[]", "[]"},
		{"number body", "42", "42"},
		{"invalid utf-8", "not JSON: \xff", "not JSON: \ufffd"},
		{"long plain message untruncated", long, long},
		{"long unstructured body truncated", `{"unknown":"` + long + `"}`, `{"unknown":"` + strings.Repeat("x", 188) + "…"},
		{"empty error string falls back", `{"error":"","message":"ignored"}`, `{"error":"","message":"ignored"}`},
		{"invalid detail entries fall back", `{"detail":[null,42,{"msg":4}]}`, `{"detail":[null,42,{"msg":4}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(tt.body))
			})
			client := newLoggedOutClient(t, server.URL)
			_, err := client.Models.List(t.Context(), nil)
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected *APIError, got %v", err)
			}
			if apiErr.RequestID != "" {
				t.Error("request id should be absent")
			}
			want := fmt.Sprintf("GET %s/v1/models: 400 %s", server.URL, tt.want)
			if err.Error() != want {
				t.Errorf("Error() = %q, want %q", err.Error(), want)
			}
		})
	}
}

func TestTransportErrorClassification(t *testing.T) {
	t.Run("connection error", func(t *testing.T) {
		cause := errors.New("dial tcp: connection refused")
		client := newTransportClient(t, roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return nil, cause
		}))
		_, err := client.Models.List(t.Context(), nil)
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
		if !errors.Is(err, cause) {
			t.Error("cause should be preserved through the chain")
		}
	})

	t.Run("attempt timeout", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			client := newTransportClient(t, roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				<-req.Context().Done()
				return nil, req.Context().Err()
			}))
			policy := DefaultRetryPolicy()
			policy.MaxRetries = 0
			policy.Timeout = 0
			_, err := client.Models.List(t.Context(), &ModelsListParams{Retry: &policy, Timeout: 60 * time.Millisecond})
			var timeoutErr *TimeoutError
			if !errors.As(err, &timeoutErr) {
				t.Fatalf("expected *TimeoutError, got %T: %v", err, err)
			}
			if timeoutErr.Duration != 60*time.Millisecond {
				t.Errorf("Duration = %v, want 60ms", timeoutErr.Duration)
			}
		})
	})

	t.Run("attempt timeout matches both TimeoutError and DeadlineExceeded", func(t *testing.T) {
		// The transport cause chain is preserved (a tested contract), so an
		// SDK attempt timeout also satisfies errors.Is(err,
		// context.DeadlineExceeded). Documented matching order: rule out SDK
		// types with errors.As before treating DeadlineExceeded as
		// caller-side.
		client := newTransportClient(t, roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		}))
		policy := DefaultRetryPolicy()
		policy.MaxRetries = 0
		policy.Timeout = 0
		_, err := client.Models.List(t.Context(), &ModelsListParams{Retry: &policy, Timeout: 60 * time.Millisecond})
		var timeoutErr *TimeoutError
		if !errors.As(err, &timeoutErr) {
			t.Fatalf("expected *TimeoutError, got %T: %v", err, err)
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Error("an SDK attempt timeout should also match context.DeadlineExceeded through the preserved cause chain")
		}
	})

	t.Run("caller cancellation is unretried", func(t *testing.T) {
		attempts := 0
		client := newTransportClient(t, roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			return nil, context.Canceled
		}))
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := client.Models.List(ctx, nil)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
		if attempts != 1 {
			t.Errorf("attempts = %d, want 1 (cancellation never retries)", attempts)
		}
	})
}

func TestHeaderProtection(t *testing.T) {
	protected := map[string]string{
		"Authorization":      "injected-secret",
		"Accept":             "text/plain",
		"User-Agent":         "wrong",
		"X-TypeSafe-SDK":     "wrong",
		"X-TypeSafe-Runtime": "wrong",
	}
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prefix/v1/systemone" {
			t.Errorf("path = %q, want /prefix/v1/systemone", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		// The identifier is language-neutral on the wire (the runtime header
		// carries the Go details), matching the Python and TS SDKs.
		if got := r.Header.Get("User-Agent"); got != "typesafe-sdk/"+Version {
			t.Errorf("User-Agent = %q", got)
		}
		if got := r.Header.Get("X-TypeSafe-SDK"); got != "typesafe-sdk/"+Version {
			t.Errorf("X-TypeSafe-SDK = %q", got)
		}
		if got := r.Header.Get("X-TypeSafe-Runtime"); got != runtimeHeaderValue() {
			t.Errorf("X-TypeSafe-Runtime = %q", got)
		}
		if _, present := r.Header[http.CanonicalHeaderKey(retryCountHeader)]; present {
			t.Error("X-TypeSafe-Retry-Count must be stripped from user input")
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		if got := r.Header.Get("X-Team"); got != "call" {
			t.Errorf("X-Team = %q (call header should win)", got)
		}
		if got := r.Header.Get("X-Default"); got != "kept" {
			t.Errorf("X-Default = %q", got)
		}
		writeJSON(w, http.StatusOK, resultBody)
	})
	client, err := NewClient(
		WithAPIKey("test-key"),
		WithBaseURL(server.URL+"/prefix///"),
		WithHeaders(map[string]string{
			"Authorization":      protected["Authorization"],
			"Accept":             protected["Accept"],
			"User-Agent":         protected["User-Agent"],
			"X-TypeSafe-SDK":     protected["X-TypeSafe-SDK"],
			"X-TypeSafe-Runtime": protected["X-TypeSafe-Runtime"],
			"X-Team":             "default",
			"X-Default":          "kept",
			"X-API-Key":          "key-secret",
			"Cookie":             "cookie-secret",
		}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()
	_, err = client.SystemOne(t.Context(), &SystemOneParams{
		State:     "hello",
		Questions: Questions{"q": RawQuestion{"type": "noul", "instructions": "?"}},
		ExtraHeaders: map[string]string{
			"Authorization":          protected["Authorization"],
			"Accept":                 protected["Accept"],
			"User-Agent":             protected["User-Agent"],
			"X-TypeSafe-SDK":         protected["X-TypeSafe-SDK"],
			"X-TypeSafe-Runtime":     protected["X-TypeSafe-Runtime"],
			"X-Team":                 "call",
			"X-TypeSafe-Retry-Count": "99",
			"Content-Type":           "wrong",
		},
	})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
}

func TestTimeoutPrecedence(t *testing.T) {
	var deadlines []time.Duration
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		deadline, hasDeadline := req.Context().Deadline()
		if !hasDeadline {
			deadlines = append(deadlines, -1)
		} else {
			deadlines = append(deadlines, time.Until(deadline))
		}
		return textResponse(req, http.StatusOK, `{"models": []}`), nil
	})
	client, err := NewClient(WithAPIKey("k"), WithHTTPClient(&http.Client{Transport: transport}), WithTimeout(7*time.Second))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if _, err := client.Models.List(t.Context(), nil); err != nil {
		t.Fatalf("call without override: %v", err)
	}
	if _, err := client.Models.List(t.Context(), &ModelsListParams{Timeout: 2 * time.Second}); err != nil {
		t.Fatalf("call with override: %v", err)
	}
	if _, err := client.Models.List(t.Context(), nil); err != nil {
		t.Fatalf("second call without override: %v", err)
	}

	if len(deadlines) != 3 {
		t.Fatalf("captured %d deadlines", len(deadlines))
	}
	assertDeadline(t, deadlines[0], 7*time.Second, "client timeout")
	assertDeadline(t, deadlines[1], 2*time.Second, "call timeout")
	assertDeadline(t, deadlines[2], 7*time.Second, "client timeout restored")
}

func TestDefaultTimeoutUsed(t *testing.T) {
	var deadline time.Duration
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		expiry, _ := req.Context().Deadline()
		deadline = time.Until(expiry)
		return textResponse(req, http.StatusOK, `{"models": []}`), nil
	})
	client, err := NewClient(WithAPIKey("k"), WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()
	if _, err := client.Models.List(t.Context(), nil); err != nil {
		t.Fatalf("List: %v", err)
	}
	assertDeadline(t, deadline, DefaultTimeout, "default timeout")
}

// TestHTTPClientTimeoutInherited: like the Python SDK, a supplied client's
// positive Timeout becomes the SDK per-attempt timeout when WithTimeout is
// not set.
func TestHTTPClientTimeoutInherited(t *testing.T) {
	var deadline time.Duration
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		expiry, _ := req.Context().Deadline()
		deadline = time.Until(expiry)
		return textResponse(req, http.StatusOK, `{"models": []}`), nil
	})
	client, err := NewClient(
		WithAPIKey("k"),
		WithHTTPClient(&http.Client{Transport: transport, Timeout: 21 * time.Second}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()
	if _, err := client.Models.List(t.Context(), nil); err != nil {
		t.Fatalf("List: %v", err)
	}
	assertDeadline(t, deadline, 21*time.Second, "injected client timeout")

	// An explicit WithTimeout still wins over the injected client's Timeout.
	client2, err := NewClient(
		WithAPIKey("k"),
		WithHTTPClient(&http.Client{Transport: transport, Timeout: 21 * time.Second}),
		WithTimeout(3*time.Second))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client2.Close()
	if _, err := client2.Models.List(t.Context(), nil); err != nil {
		t.Fatalf("List: %v", err)
	}
	assertDeadline(t, deadline, 3*time.Second, "explicit timeout over injected")
}

// assertDeadlineTight asserts a deadline measured inside the transport: the
// remaining budget never exceeds the resolved timeout and shrinks only by
// scheduling delay, so it stays within a tight window below it.
func assertDeadlineTight(t *testing.T, got, want time.Duration, label string) {
	t.Helper()
	if got <= 0 || got > want || got < want-750*time.Millisecond {
		t.Errorf("%s deadline = %v, want ~%v", label, got, want)
	}
}

// deadlineRecorder captures the effective request deadline seen by the
// transport, which is the minimum of the SDK attempt context's deadline and
// any *http.Client-level Timeout still in force.
type deadlineRecorder struct {
	mu        sync.Mutex
	deadlines []time.Duration
}

func (r *deadlineRecorder) record(req *http.Request) {
	got := time.Duration(-1)
	if deadline, ok := req.Context().Deadline(); ok {
		got = time.Until(deadline)
	}
	r.mu.Lock()
	r.deadlines = append(r.deadlines, got)
	r.mu.Unlock()
}

func (r *deadlineRecorder) snapshot() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.deadlines...)
}

// TestInjectedClientTimeoutIsDefaultNotCap: the injected *http.Client's
// Timeout is a default the SDK inherits only when WithTimeout is unset — it
// must never cap a longer explicit SDK timeout, matching the Python SDK
// (which passes the resolved timeout to each request, overriding the HTTP
// client's own default).
func TestInjectedClientTimeoutIsDefaultNotCap(t *testing.T) {
	recorder := &deadlineRecorder{}
	injected := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			recorder.record(req)
			return textResponse(req, http.StatusOK, `{"models": []}`), nil
		}),
		Timeout: 1 * time.Second,
	}

	// Inheritance: no WithTimeout means the injected Timeout becomes the SDK
	// per-attempt timeout.
	inheriting, err := NewClient(WithAPIKey("k"), WithHTTPClient(injected))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer inheriting.Close()
	if _, err := inheriting.Models.List(t.Context(), nil); err != nil {
		t.Fatalf("List (inherited): %v", err)
	}
	assertDeadlineTight(t, recorder.snapshot()[0], 1*time.Second, "inherited client timeout")

	// Explicit and per-call timeouts, shorter and longer than the injected
	// client's own Timeout.
	selecting, err := NewClient(WithAPIKey("k"), WithHTTPClient(injected), WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer selecting.Close()
	call := func(params *ModelsListParams, label string, want time.Duration) {
		t.Helper()
		if _, err := selecting.Models.List(t.Context(), params); err != nil {
			t.Fatalf("List (%s): %v", label, err)
		}
		deadlines := recorder.snapshot()
		assertDeadlineTight(t, deadlines[len(deadlines)-1], want, label)
	}
	call(nil, "longer client override", 5*time.Second)
	call(&ModelsListParams{Timeout: 2 * time.Second}, "shorter per-call override", 2*time.Second)
	call(&ModelsListParams{Timeout: 7 * time.Second}, "longer per-call override", 7*time.Second)
	call(nil, "client timeout restored", 5*time.Second)

	// The caller's client is never mutated, not even temporarily.
	if injected.Timeout != 1*time.Second {
		t.Errorf("injected client Timeout = %v, want the original 1s", injected.Timeout)
	}
	if selecting.httpClient == injected {
		t.Error("the SDK must send on its own copy, not the caller's client")
	}

	// The same resolution applies to SystemOne.
	systemOneRecorder := &deadlineRecorder{}
	systemOneInjected := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			systemOneRecorder.record(req)
			return textResponse(req, http.StatusOK, resultBody), nil
		}),
		Timeout: 1 * time.Second,
	}
	systemOneClient, err := NewClient(WithAPIKey("k"), WithHTTPClient(systemOneInjected), WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer systemOneClient.Close()
	if _, err := systemOneClient.SystemOne(t.Context(), &SystemOneParams{
		State:     "x",
		Questions: Questions{"q": RawQuestion{"type": "noul"}},
	}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	assertDeadlineTight(t, systemOneRecorder.snapshot()[0], 5*time.Second, "SystemOne longer override")
}

// TestSucceedsAfterInjectedClientTimeout: a response slower than the injected
// client's own Timeout but within the selected SDK timeout succeeds, because
// the only deadline in force is the SDK's resolved per-attempt timeout.
func TestSucceedsAfterInjectedClientTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		injected := &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				select {
				case <-time.After(400 * time.Millisecond):
					return textResponse(req, http.StatusOK, `{"models": []}`), nil
				case <-req.Context().Done():
					return nil, req.Context().Err()
				}
			}),
			Timeout: 100 * time.Millisecond,
		}
		client, err := NewClient(WithAPIKey("k"), WithHTTPClient(injected), WithTimeout(2*time.Second))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		defer client.Close()
		if _, err := client.Models.List(t.Context(), nil); err != nil {
			t.Fatalf("List should succeed before the 2s SDK timeout despite the 100ms injected client timeout: %v", err)
		}

		// The same slow response under a shorter per-call timeout still fails as
		// a *TimeoutError carrying the timeout that actually expired.
		_, err = client.Models.List(t.Context(), &ModelsListParams{Timeout: 150 * time.Millisecond})
		var timeoutErr *TimeoutError
		if !errors.As(err, &timeoutErr) {
			t.Fatalf("expected *TimeoutError, got %T: %v", err, err)
		}
		if timeoutErr.Duration != 150*time.Millisecond {
			t.Errorf("Duration = %v, want the resolved 150ms", timeoutErr.Duration)
		}
	})
}

// TestConcurrentCallsOnCopiedInjectedClient: concurrent calls through one
// injected client keep fully isolated per-call deadlines (safe to share the
// SDK-owned copy; run under -race in CI).
func TestConcurrentCallsOnCopiedInjectedClient(t *testing.T) {
	recorder := &deadlineRecorder{}
	injected := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			recorder.record(req)
			return textResponse(req, http.StatusOK, `{"models": []}`), nil
		}),
		Timeout: 30 * time.Second,
	}
	client, err := NewClient(WithAPIKey("k"), WithHTTPClient(injected), WithTimeout(20*time.Second))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	timeouts := []time.Duration{2 * time.Second, 4 * time.Second, 6 * time.Second}
	var wg sync.WaitGroup
	for _, timeout := range timeouts {
		wg.Add(1)
		go func(timeout time.Duration) {
			defer wg.Done()
			if _, err := client.Models.List(t.Context(), &ModelsListParams{Timeout: timeout}); err != nil {
				t.Errorf("List with %v timeout: %v", timeout, err)
			}
		}(timeout)
	}
	wg.Wait()

	deadlines := recorder.snapshot()
	if len(deadlines) != len(timeouts) {
		t.Fatalf("captured %d deadlines, want %d", len(deadlines), len(timeouts))
	}
	seen := map[time.Duration]int{}
	for _, deadline := range deadlines {
		matched := false
		for _, timeout := range timeouts {
			if deadline > 0 && deadline <= timeout && deadline >= timeout-750*time.Millisecond {
				seen[timeout]++
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("deadline %v matches no per-call timeout", deadline)
		}
	}
	for _, timeout := range timeouts {
		if seen[timeout] != 1 {
			t.Errorf("timeout %v observed %d times, want exactly 1", timeout, seen[timeout])
		}
	}
}

// TestRedirectsNotFollowed: 3xx responses surface as errors instead of being
// followed, matching the Python SDK's non-following client.
func TestRedirectsNotFollowed(t *testing.T) {
	hits := 0
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Location", "https://example.test/elsewhere")
		writeJSON(w, http.StatusFound, `{}`)
	})
	client := newLoggedOutClient(t, server.URL)
	defer client.Close()

	_, err := client.Models.List(t.Context(), nil)
	if err == nil {
		t.Fatal("expected an error for the 302 itself")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusFound {
		t.Fatalf("expected *APIError with status 302, got %T: %v", err, err)
	}
	if hits != 1 {
		t.Errorf("server hits = %d, want 1 (redirect must not be followed)", hits)
	}
}

// TestExplicitAPIKeyVerbatim: a non-blank explicit key is used verbatim (only
// env values are trimmed), and a whitespace-only explicit key falls through
// to the environment.
func TestExplicitAPIKeyVerbatim(t *testing.T) {
	var auth string
	transport := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		auth = req.Header.Get("Authorization")
		return textResponse(req, http.StatusOK, `{"models": []}`), nil
	})

	client, err := NewClient(WithAPIKey(" padded "), WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()
	if _, err := client.Models.List(t.Context(), nil); err != nil {
		t.Fatalf("List: %v", err)
	}
	if auth != "Bearer  padded " {
		t.Errorf("Authorization = %q, want verbatim padded key", auth)
	}

	t.Setenv(EnvAPIKey, "env-key")
	fromEnv, err := NewClient(WithAPIKey("   "), WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatalf("NewClient with whitespace key: %v", err)
	}
	defer fromEnv.Close()
	if _, err := fromEnv.Models.List(t.Context(), nil); err != nil {
		t.Fatalf("List: %v", err)
	}
	if auth != "Bearer env-key" {
		t.Errorf("Authorization = %q, want the env key after a whitespace-only explicit key", auth)
	}
}

// TestBodyReadTimeoutClassified: a per-attempt deadline that fires while the
// response body is still streaming is a *TimeoutError (matching Python's
// ReadTimeout classification), not a ConnectionError.
func TestBodyReadTimeoutClassified(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done() // stall past the client's attempt deadline
	})
	client := newLoggedOutClient(t, server.URL)
	defer client.Close()
	client.retry = RetryPolicy{} // no retries: classify the single failure

	_, err := client.Models.List(t.Context(), &ModelsListParams{Timeout: 150 * time.Millisecond})
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	var timeoutErr *TimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("expected *TimeoutError for a read-phase deadline, got %T: %v", err, err)
	}
	if timeoutErr.Duration != 150*time.Millisecond {
		t.Errorf("Duration = %v, want the resolved per-attempt timeout", timeoutErr.Duration)
	}
}

func assertDeadline(t *testing.T, got, want time.Duration, label string) {
	t.Helper()
	// Allow scheduling slack around the expected per-attempt timeout.
	if got < want-2*time.Second || got > want {
		t.Errorf("%s deadline = %v, want ~%v", label, got, want)
	}
}

// TestPerCallValidationRejectsBadValues: per-call negative timeouts and
// invalid retry policies fail before any network I/O, on both resources.
func TestPerCallValidationRejectsBadValues(t *testing.T) {
	client := newLoggedOutClient(t, "https://unreachable.invalid")
	defer client.Close()
	questions := Questions{"q": RawQuestion{"type": "noul", "instructions": "?"}}

	_, err := client.SystemOne(t.Context(), &SystemOneParams{
		State: "x", Questions: questions, Timeout: -time.Second,
	})
	if err == nil || err.Error() != "timeout must be a positive, finite number of seconds." {
		t.Errorf("negative SystemOne timeout: %v", err)
	}
	_, err = client.Models.List(t.Context(), &ModelsListParams{Timeout: -time.Second})
	if err == nil || err.Error() != "timeout must be a positive, finite number of seconds." {
		t.Errorf("negative List timeout: %v", err)
	}

	badPolicy := RetryPolicy{MaxRetries: -1}
	_, err = client.SystemOne(t.Context(), &SystemOneParams{
		State: "x", Questions: questions, Retry: &badPolicy,
	})
	if err == nil || err.Error() != "max_retries must be a non-negative integer." {
		t.Errorf("invalid per-call policy: %v", err)
	}
	_, err = client.Models.List(t.Context(), &ModelsListParams{Retry: &badPolicy})
	if err == nil || err.Error() != "max_retries must be a non-negative integer." {
		t.Errorf("invalid per-call policy (models): %v", err)
	}
}

// TestExtraHeadersCaseInsensitive: per-call extras win over client defaults
// regardless of letter-casing differences between the two maps.
func TestExtraHeadersCaseInsensitive(t *testing.T) {
	var seen string
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Team")
		writeJSON(w, http.StatusOK, resultBody)
	})
	client, err := NewClient(
		WithAPIKey("k"),
		WithBaseURL(server.URL),
		WithHeaders(map[string]string{"X-Team": "default"}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()
	if _, err := client.SystemOne(t.Context(), &SystemOneParams{
		State:     "x",
		Questions: Questions{"q": RawQuestion{"type": "noul"}},
		ExtraHeaders: map[string]string{
			"x-team": "call", // different casing than the default header
		},
	}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if seen != "call" {
		t.Errorf("X-Team = %q, want the per-call value (case-insensitive override)", seen)
	}
}

// TestProtectedHeadersCaseVariants: even aggressively re-cased spellings of
// protected headers cannot leak through user headers.
func TestProtectedHeadersCaseVariants(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("User-Agent"); got != "typesafe-sdk/"+Version {
			t.Errorf("User-Agent = %q", got)
		}
		if got := r.Header.Get("X-TypeSafe-SDK"); got != "typesafe-sdk/"+Version {
			t.Errorf("X-TypeSafe-SDK = %q", got)
		}
		writeJSON(w, http.StatusOK, resultBody)
	})
	client, err := NewClient(
		WithAPIKey("k"),
		WithBaseURL(server.URL),
		WithHeaders(map[string]string{
			"AUTHORIZATION":  "Bearer evil",
			"uSeR-aGeNt":     "evil",
			"x-tYpEsAfE-sDk": "evil",
		}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()
	if _, err := client.SystemOne(t.Context(), &SystemOneParams{
		State:     "x",
		Questions: Questions{"q": RawQuestion{"type": "noul"}},
		ExtraHeaders: map[string]string{
			"cOnTeNt-TyPe": "text/plain", // must be restored to application/json
		},
	}); err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
}

// TestExhaustedTransportRetries: when every attempt fails at the transport
// layer, the surfaced error is the LAST attempt's failure with its cause.
func TestExhaustedTransportRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		dc := newDeterministicClient(t, nil)
		dc.setTransport(func(req *http.Request) (*http.Response, error) {
			attempts++
			return nil, fmt.Errorf("dial attempt %d failed", attempts)
		})
		_, err := dc.Models.List(t.Context(), nil)
		if err == nil {
			t.Fatal("expected an error")
		}
		if attempts != 3 {
			t.Errorf("attempts = %d, want 3", attempts)
		}
		var connErr *ConnectionError
		if !errors.As(err, &connErr) {
			t.Fatalf("expected *ConnectionError, got %T: %v", err, err)
		}
		if !strings.Contains(connErr.Error(), "dial attempt 3 failed") {
			t.Errorf("final error should come from the last attempt: %q", connErr.Error())
		}
	})
}

// TestZeroBackoffNonRecovery: backoff disabled retries immediately and still
// surfaces the final HTTP error when every attempt fails.
func TestZeroBackoffNonRecovery(t *testing.T) {
	// Asserting a zero gap needs the bubble clock; against the real clock the
	// measured gap is a small nonzero number of nanoseconds.
	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		dc := newDeterministicClient(t, nil)
		dc.setTransport(func(req *http.Request) (*http.Response, error) {
			attempts++
			return textResponseWithHeaders(req, http.StatusServiceUnavailable, `{"message": "temporarily unavailable"}`,
				map[string]string{"Retry-After-Ms": "0"}), nil
		})
		policy := DefaultRetryPolicy()
		policy.BackoffInitial = 0
		policy.BackoffMax = 0
		dc.retry = policy
		_, err := dc.Models.List(t.Context(), nil)
		var serverErr *InternalServerError
		if !errors.As(err, &serverErr) {
			t.Fatalf("expected *InternalServerError, got %T: %v", err, err)
		}
		if attempts != 3 {
			t.Errorf("attempts = %d, want 3", attempts)
		}
		if delays := dc.recordedDelays(); len(delays) != 2 || delays[0] != 0 || delays[1] != 0 {
			t.Errorf("delays = %v, want immediate retries", delays)
		}
		if !strings.Contains(err.Error(), "temporarily unavailable") {
			t.Errorf("final message = %q", err.Error())
		}
	})
}

// TestRetryOnThroughClient: a RetryOn entry opts a 404 into retries through
// the live client path.
func TestRetryOnThroughClient(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		dc := newDeterministicClient(t, nil)
		dc.setTransport(func(req *http.Request) (*http.Response, error) {
			attempts++
			return textResponse(req, http.StatusNotFound, `{"message": "gone"}`), nil
		})
		policy := DefaultRetryPolicy()
		policy.RetryOn = []error{&NotFoundError{}}
		policy.Timeout = 0 // unlimited budget: every backoff is actually slept
		dc.retry = policy
		_, err := dc.Models.List(t.Context(), nil)
		if err == nil {
			t.Fatal("expected the final 404")
		}
		if attempts != 3 {
			t.Errorf("attempts = %d, want 3 (RetryOn retries 404)", attempts)
		}
	})
}

// TestRetryAfterMsDrivesSleepEndToEnd: a nonzero retry-after-ms header drives
// the actual sleep between attempts.
func TestRetryAfterMsDrivesSleepEndToEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		dc := newDeterministicClient(t, nil)
		dc.setTransport(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return textResponseWithHeaders(req, http.StatusTooManyRequests, `{}`,
					map[string]string{"Retry-After-Ms": "125"}), nil
			}
			return textResponse(req, http.StatusOK, `{"models": []}`), nil
		})
		if _, err := dc.Models.List(t.Context(), nil); err != nil {
			t.Fatalf("List: %v", err)
		}
		delays := dc.recordedDelays()
		if len(delays) != 1 || delays[0] != 125*time.Millisecond {
			t.Errorf("delays = %v, want exactly one 125ms sleep from retry-after-ms", delays)
		}
	})
}

// TestBrokenBodyRetried: a 200 whose body fails mid-read is classified as a
// connection error and retried; the recovered attempt's result wins.
func TestBrokenBodyRetried(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		dc := newDeterministicClient(t, nil)
		dc.setTransport(func(req *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(brokenReader{}),
					Request:    req,
				}, nil
			}
			return textResponse(req, http.StatusOK, `{"models": []}`), nil
		})
		if _, err := dc.Models.List(t.Context(), nil); err != nil {
			t.Fatalf("List should recover: %v", err)
		}
		if attempts != 2 {
			t.Errorf("attempts = %d, want 2", attempts)
		}
	})
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestRetryCountHeaderSequence(t *testing.T) {
	requests := 0
	dc := newDeterministicClient(t, nil)
	dc.setTransport(func(req *http.Request) (*http.Response, error) {
		requests++
		return textResponseWithHeaders(req, http.StatusTooManyRequests, `{"message": "failed"}`,
			map[string]string{"Retry-After-Ms": "0"}), nil
	})
	_, err := dc.Models.List(t.Context(), nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if requests != 3 {
		t.Errorf("attempts = %d, want 3", requests)
	}
	// The first attempt sends no retry-count header; retries carry 1, 2.
	if got := len(dc.recordedDelays()); got != 2 {
		t.Errorf("sleeps = %d, want 2", got)
	}
}

func TestMaxRetriesBoundAttempts(t *testing.T) {
	for _, maxRetries := range []int{0, 1, 4} {
		requests := 0
		dc := newDeterministicClient(t, nil)
		dc.setTransport(func(req *http.Request) (*http.Response, error) {
			requests++
			return textResponseWithHeaders(req, http.StatusTooManyRequests, `{"message": "slow"}`,
				map[string]string{"Retry-After-Ms": "0"}), nil
		})
		policy := DefaultRetryPolicy()
		policy.MaxRetries = maxRetries
		policy.Timeout = 0
		_, err := dc.Models.List(t.Context(), &ModelsListParams{Retry: &policy})
		var rateLimit *RateLimitError
		if !errors.As(err, &rateLimit) {
			t.Fatalf("maxRetries=%d: expected RateLimitError, got %v", maxRetries, err)
		}
		if requests != maxRetries+1 {
			t.Errorf("maxRetries=%d: attempts = %d, want %d", maxRetries, requests, maxRetries+1)
		}
	}
}

func TestDefaultRetryStatusesThroughClient(t *testing.T) {
	statuses := []struct {
		status   int
		attempts int
	}{
		{408, 3}, {429, 3}, {500, 3}, {503, 3}, {599, 3},
		{400, 1}, {401, 1}, {403, 1}, {404, 1}, {409, 1}, {422, 1}, {302, 1},
	}
	for _, tt := range statuses {
		t.Run(fmt.Sprintf("status %d", tt.status), func(t *testing.T) {
			requests := 0
			dc := newDeterministicClient(t, nil)
			dc.setTransport(func(req *http.Request) (*http.Response, error) {
				requests++
				return textResponseWithHeaders(req, tt.status, `{"message": "failed"}`,
					map[string]string{"Retry-After-Ms": "0"}), nil
			})
			_, err := dc.Models.List(t.Context(), nil)
			if err == nil {
				t.Fatal("expected an error")
			}
			if requests != tt.attempts {
				t.Errorf("status %d: attempts = %d, want %d", tt.status, requests, tt.attempts)
			}
		})
	}
}

func TestExhaustedRetryPreservesFinalHTTPError(t *testing.T) {
	requests := 0
	dc := newDeterministicClient(t, nil)
	dc.setTransport(func(req *http.Request) (*http.Response, error) {
		requests++
		status := []int{429, 500, 503}[requests-1]
		return textResponseWithHeaders(req, status, fmt.Sprintf(`{"message": "attempt %d"}`, requests),
			map[string]string{"X-Typesafe-Request-Id": fmt.Sprintf("request-%d", requests), "Retry-After-Ms": "0"}), nil
	})
	_, err := dc.SystemOne(t.Context(), &SystemOneParams{
		State:     "x",
		Questions: Questions{"q": Noul{Instructions: "?"}},
	})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %v", err)
	}
	if requests != 3 || apiErr.Status != 503 {
		t.Errorf("attempts = %d, status = %d; want 3 attempts ending at 503", requests, apiErr.Status)
	}
	want := "POST https://api.typesafe.ai/v1/systemone: 503 attempt 3 (request_id=request-3)"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestSystemOneRetryOverride(t *testing.T) {
	requests := 0
	dc := newDeterministicClient(t, nil)
	dc.setTransport(func(req *http.Request) (*http.Response, error) {
		requests++
		status := 429
		if req.Header.Get("X-Call") == "override" {
			status = 409
		}
		return textResponseWithHeaders(req, status, `{"message": "failed"}`,
			map[string]string{"Retry-After-Ms": "0"}), nil
	})
	clientPolicy := DefaultRetryPolicy()
	clientPolicy.MaxRetries = 0 // client-level: 1 attempt on 429
	clientPolicy.Timeout = 0
	dc.Client.retry = clientPolicy

	callPolicy := DefaultRetryPolicy()
	callPolicy.MaxRetries = 1 // call-level: 2 attempts on 409
	callPolicy.HTTPStatuses = map[int]struct{}{409: {}}
	callPolicy.Timeout = 0

	requests = 0
	_, err := dc.SystemOne(t.Context(), &SystemOneParams{
		State:        "hello",
		Questions:    Questions{"q": RawQuestion{"type": "noul", "instructions": "?"}},
		ExtraHeaders: map[string]string{"X-Call": "override"},
		Retry:        &callPolicy,
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if requests != 2 {
		t.Errorf("override attempts = %d, want 2", requests)
	}

	requests = 0
	_, err = dc.SystemOne(t.Context(), &SystemOneParams{
		State:     "hello",
		Questions: Questions{"q": RawQuestion{"type": "noul", "instructions": "?"}},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if requests != 1 {
		t.Errorf("inherited attempts = %d, want 1", requests)
	}
}

func TestPerCallOverrideIsolation(t *testing.T) {
	var bodies []map[string]any
	var teamHeaders []string
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		bodies = append(bodies, decodeJSONMap(t, data))
		teamHeaders = append(teamHeaders, r.Header.Get("X-Team"))
		writeJSON(w, http.StatusOK, resultBody)
	})
	client, err := NewClient(WithAPIKey("k"), WithBaseURL(server.URL), WithModel("client-model"),
		WithHeaders(map[string]string{"X-Default": "kept"}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if _, err := client.SystemOne(t.Context(), &SystemOneParams{
		State:        "one",
		Questions:    Questions{"q": Noul{Instructions: "?"}},
		Model:        "call-model",
		ExtraHeaders: map[string]string{"X-Team": "one"},
		ExtraBody:    map[string]JSONValue{"extra": true},
	}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := client.SystemOne(t.Context(), &SystemOneParams{
		State:     "two",
		Questions: Questions{"q": Noul{Instructions: "?"}},
	}); err != nil {
		t.Fatalf("second call: %v", err)
	}

	if len(bodies) != 2 {
		t.Fatalf("captured %d bodies", len(bodies))
	}
	if bodies[0]["model"] != "call-model" || bodies[0]["extra"] != true || bodies[0]["state"] != "one" {
		t.Errorf("first call body = %v", bodies[0])
	}
	if teamHeaders[0] != "one" {
		t.Errorf("first call header = %q", teamHeaders[0])
	}
	if bodies[1]["model"] != "client-model" || bodies[1]["state"] != "two" {
		t.Errorf("second call body = %v", bodies[1])
	}
	if _, present := bodies[1]["extra"]; present {
		t.Error("extra body keys must not leak between calls")
	}
	if teamHeaders[1] != "" {
		t.Errorf("second call header = %q, want empty", teamHeaders[1])
	}
}

func TestCloseSemantics(t *testing.T) {
	newClientWithTransport := func(t *testing.T) (*Client, *trackingTransport) {
		t.Helper()
		transport := &trackingTransport{inner: func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, `{"models": []}`)
		}}
		client, err := NewClient(WithAPIKey("k"), WithHTTPClient(&http.Client{Transport: transport}))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		return client, transport
	}

	t.Run("closes supplied client idle connections", func(t *testing.T) {
		client, transport := newClientWithTransport(t)
		if _, err := client.Models.List(t.Context(), nil); err != nil {
			t.Fatalf("List: %v", err)
		}
		client.Close()
		client.Close() // double close is safe
		if transport.closeCalls() != 1 {
			t.Errorf("CloseIdleConnections called %d times, want 1", transport.closeCalls())
		}
	})

	t.Run("reuse after close errors", func(t *testing.T) {
		client, transport := newClientWithTransport(t)
		client.Close()
		_, err := client.Models.List(t.Context(), nil)
		if !errors.Is(err, ErrClientClosed) {
			t.Errorf("expected ErrClientClosed, got %v", err)
		}
		_, err = client.SystemOne(t.Context(), &SystemOneParams{
			State: "x", Questions: Questions{"q": Noul{}},
		})
		if !errors.Is(err, ErrClientClosed) {
			t.Errorf("SystemOne after close: %v", err)
		}
		if transport.requestCount() != 0 {
			t.Errorf("closed client reached the network %d times", transport.requestCount())
		}
	})

	t.Run("owned client gets a dedicated transport", func(t *testing.T) {
		client, err := NewClient(WithAPIKey("k"))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		defer client.Close()
		transport, ok := client.httpClient.Transport.(*http.Transport)
		if !ok {
			t.Fatalf("owned client transport = %T, want *http.Transport", client.httpClient.Transport)
		}
		if transport == http.DefaultTransport {
			t.Fatal("owned client must not share the process-global DefaultTransport pool")
		}
	})

	t.Run("close leaves the process-global pool alone", func(t *testing.T) {
		server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, `{"models": []}`)
		})
		get := func() {
			resp, err := http.Get(server.URL)
			if err != nil {
				t.Fatalf("default-client GET: %v", err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}

		// Build the SDK client first so its transport snapshot predates the
		// swap below, then watch the process-default pool through a counting
		// dialer. Package tests run sequentially, so the swap is safe.
		client, err := NewClient(WithAPIKey("k"), WithBaseURL(server.URL))
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		defer client.Close()
		original, _ := http.DefaultTransport.(*http.Transport)
		dials := 0
		global := original.Clone()
		global.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			dials++
			return original.DialContext(ctx, network, addr)
		}
		http.DefaultTransport = global
		t.Cleanup(func() { http.DefaultTransport = original })

		get() // warm one idle connection on the process-default pool
		if _, err := client.Models.List(t.Context(), nil); err != nil {
			t.Fatalf("List: %v", err)
		}
		client.Close()
		get() // must reuse the warm connection: no new dial

		if dials != 1 {
			t.Errorf("default-pool dials = %d, want 1 (SDK Close must not evict other code's idle connections)", dials)
		}
	})
}

func TestValidationHappensBeforeNetwork(t *testing.T) {
	attempts := 0
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempts++
		writeJSON(w, http.StatusOK, resultBody)
	})
	client := newLoggedOutClient(t, server.URL)
	_, err := client.SystemOne(t.Context(), &SystemOneParams{
		State:     "x",
		Questions: Questions{},
	})
	var typeSafeErr *TypeSafeError
	if !errors.As(err, &typeSafeErr) {
		t.Fatalf("expected *TypeSafeError, got %v", err)
	}
	if attempts != 0 {
		t.Errorf("invalid questions reached the network (%d attempts)", attempts)
	}

	// Empty typed score criteria is equally rejected before any I/O.
	_, err = client.SystemOne(t.Context(), &SystemOneParams{
		State:     "x",
		Questions: Questions{"rating": Score{Instructions: "Quality?"}},
	})
	if !errors.As(err, &typeSafeErr) ||
		err.Error() != `Score question "rating" has no criteria; at least one score is required.` {
		t.Errorf("empty score criteria: %v", err)
	}
	if attempts != 0 {
		t.Errorf("empty score criteria reached the network (%d attempts)", attempts)
	}
}

func TestConcurrentCallsWithDistinctOverrides(t *testing.T) {
	type callSpec struct {
		name     string
		attempts int
	}
	calls := []callSpec{{"one", 1}, {"three", 3}, {"default", 2}}

	var mu sync.Mutex
	seen := map[string][]map[string]any{}
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		key := r.Header.Get("X-Call")
		mu.Lock()
		seen[key] = append(seen[key], decodeJSONMap(t, data))
		mu.Unlock()
		writeJSON(w, http.StatusTooManyRequests, `{"message": "retry"}`)
	})
	// A real server rules out a fake clock, so backoff is disabled to keep the
	// concurrent retries instant; only per-call isolation is under test here.
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 1
	policy.Timeout = 0
	policy.BackoffInitial = 0
	client, err := NewClient(WithAPIKey("k"), WithBaseURL(server.URL), WithRetry(policy))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	var wg sync.WaitGroup
	for _, spec := range calls {
		wg.Go(func() {
			callPolicy := DefaultRetryPolicy()
			callPolicy.MaxRetries = spec.attempts - 1
			callPolicy.Timeout = 0
			callPolicy.BackoffInitial = 0
			params := &SystemOneParams{
				State:        spec.name,
				Questions:    Questions{"q": Noul{Instructions: "?"}},
				Model:        spec.name,
				ExtraHeaders: map[string]string{"X-Call": spec.name},
			}
			if spec.name != "default" {
				params.Retry = &callPolicy
				params.Timeout = time.Duration(spec.attempts) * time.Second
			}
			if _, err := client.SystemOne(t.Context(), params); err == nil {
				t.Errorf("call %q: expected an error", spec.name)
			}
		})
	}
	wg.Wait()

	for _, spec := range calls {
		bodies, ok := seen[spec.name]
		if !ok || len(bodies) != spec.attempts {
			t.Errorf("call %q: %d attempts recorded, want %d", spec.name, len(bodies), spec.attempts)
			continue
		}
		for _, body := range bodies {
			if body["model"] != spec.name || body["state"] != spec.name {
				t.Errorf("call %q: body overrides leaked: %v", spec.name, body)
			}
		}
	}
}

// --- helpers -----------------------------------------------------------------

func newTransportClient(t *testing.T, transport roundTripperFunc) *Client {
	t.Helper()
	client, err := NewClient(WithAPIKey("test-key"), WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(client.Close)
	return client
}

func textResponse(req *http.Request, status int, body string) *http.Response {
	return textResponseWithHeaders(req, status, body, nil)
}

func textResponseWithHeaders(req *http.Request, status int, body string, headers map[string]string) *http.Response {
	header := http.Header{}
	for name, value := range headers {
		header.Set(name, value)
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

// TestUnparseableBaseURLFailsFast: a malformed base URL fails immediately
// with a non-retryable error (the Python SDK's URL error also escapes
// unwrapped and unretried).
func TestUnparseableBaseURLFailsFast(t *testing.T) {
	client, err := NewClient(WithAPIKey("k"), WithBaseURL("http://[::1"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()
	_, err = client.Models.List(t.Context(), nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	var connErr *ConnectionError
	if errors.As(err, &connErr) {
		t.Fatalf("URL errors must not classify as ConnectionError (retried): %v", err)
	}
	var typeSafeErr *TypeSafeError
	if !errors.As(err, &typeSafeErr) {
		t.Fatalf("expected *TypeSafeError, got %T: %v", err, err)
	}
}
