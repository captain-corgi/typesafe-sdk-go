package typesafe

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
)

// systemOneCall runs a minimal SystemOne request against a handler.
func systemOneCall(t *testing.T, client *Client, state JSONContent, questions Questions) (*SystemOneResponse, error) {
	t.Helper()
	return client.SystemOne(t.Context(), &SystemOneParams{State: state, Questions: questions})
}

func requireValidationError(t *testing.T, err error) *ResponseValidationError {
	t.Helper()
	var validationErr *ResponseValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected *ResponseValidationError, got %T: %v", err, err)
	}
	return validationErr
}

func TestMalformedSystemOneResponses(t *testing.T) {
	tests := []struct {
		name      string
		answers   string
		hasModel  bool
		fieldPath string
	}{
		{"missing model", ``, false, "model"},
		{"noul missing value", `{"n": {"type": "noul"}}`, true, "answers.n.noul"},
		// Duplicate answer names collapse last-wins (v1 and Python json.loads
		// semantics): the shadowed valid entry is never decoded, so the error
		// names the trailing one.
		{"duplicate answer name, last wins", `{"n": {"type": "noul", "noul": 0.9}, "n": {"type": "noul"}}`, true, "answers.n.noul"},
		{"noul wrong type", `{"n": {"type": "noul", "noul": "0.5"}}`, true, "answers.n.noul"},
		{"choice missing confidence", `{"c": {"type": "choice", "choice": "a", "probabilities": {}}}`, true, "answers.c.confidence"},
		{"choice missing choice", `{"c": {"type": "choice", "confidence": 0.5, "probabilities": {}}}`, true, "answers.c.choice"},
		{"score legend is a list", `{"s": {"type": "score", "score": 1, "confidence": 1, "legend": [], "probabilities": {}}}`, true, "answers.s.legend"},
		{"score legend bad key", `{"s": {"type": "score", "score": 1, "confidence": 1, "legend": {"x": "bad"}, "probabilities": {}}}`, true, "answers.s.legend.x"},
		{"score legend number value", `{"s": {"type": "score", "score": 1, "confidence": 1, "legend": {"0": 5}, "probabilities": {}}}`, true, "answers.s.legend.0.str"},
		{"score legend null value", `{"s": {"type": "score", "score": 1, "confidence": 1, "legend": {"0": null}, "probabilities": {}}}`, true, "answers.s.legend.0.str"},
		{"score legend padded int key coerces", `{"s": {"type": "score", "score": 1, "confidence": 1, "legend": {" 1": "low"}, "probabilities": {" 1": 0.5}}}`, true, ""},
		{"choice empty key with bad value", `{"c": {"type": "choice", "choice": "a", "confidence": 1, "probabilities": {"": "x"}}}`, true, "answers.c.probabilities."},
		{"answer not an object", `{"c": "not-a-mapping"}`, true, "answers.c.type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			answers := `"answers": {}`
			if tt.answers != `` {
				answers = `"answers": ` + tt.answers
			}
			model := `,"model": "test"`
			if !tt.hasModel {
				model = ``
			}
			body := `{"usage": {"input_tokens": 1, "output_tokens": 1}` + model + `,` + answers + `}`
			server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-Typesafe-Request-Id", "req-123")
				writeJSON(w, http.StatusOK, body)
			})
			client := newLoggedOutClient(t, server.URL)
			result, err := systemOneCall(t, client, "x", Questions{"q": RawQuestion{"type": "noul", "instructions": "?"}})
			if tt.fieldPath == "" {
				if err != nil {
					t.Fatalf("expected success, got %v", err)
				}
				score, ok := result.Scores()["s"]
				if !ok {
					t.Fatalf("expected score answer, got %v", result.Answers)
				}
				if len(score.Legend) != 1 {
					t.Errorf("legend = %v, want one padded-key entry", score.Legend)
				}
				return
			}
			validationErr := requireValidationError(t, err)
			if validationErr.FieldPath != tt.fieldPath {
				t.Errorf("FieldPath = %q, want %q", validationErr.FieldPath, tt.fieldPath)
			}
			if validationErr.Status != http.StatusOK {
				t.Errorf("Status = %d, want 200", validationErr.Status)
			}
			if validationErr.RequestID != "req-123" {
				t.Errorf("RequestID = %q, want req-123", validationErr.RequestID)
			}
			want := `POST ` + server.URL + `/v1/systemone: 200 Invalid response data at '` + tt.fieldPath + `'. (request_id=req-123)`
			if validationErr.Error() != want {
				t.Errorf("Error() = %q\n       want %q", validationErr.Error(), want)
			}
		})
	}
}

func TestReadResponseBodyLimit(t *testing.T) {
	for _, tt := range []struct {
		name      string
		size      int
		wantError bool
	}{
		{name: "below", size: 63},
		{name: "equal", size: 64},
		{name: "above", size: 65, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body, err := readResponseBody(io.NopCloser(strings.NewReader(strings.Repeat("x", tt.size))), 64)
			if tt.wantError {
				var tooLarge *ResponseTooLargeError
				if !errors.As(err, &tooLarge) || tooLarge.Limit != 64 {
					t.Fatalf("expected ResponseTooLargeError, got %T: %v", err, err)
				}
				if !errors.Is(err, ErrResponseTooLarge) {
					t.Error("oversized response should wrap ErrResponseTooLarge")
				}
				var root *TypeSafeError
				if !errors.As(err, &root) {
					t.Error("oversized response should match *TypeSafeError")
				}
				return
			}
			if err != nil || len(body) != tt.size {
				t.Fatalf("readResponseBody() = %d bytes, %v; want %d bytes, nil", len(body), err, tt.size)
			}
		})
	}
}

func TestReadResponseBodyMaximumLimit(t *testing.T) {
	const want = `{"models":[]}`
	body, err := readResponseBody(io.NopCloser(strings.NewReader(want)), math.MaxInt64)
	if err != nil {
		t.Fatalf("readResponseBody() error = %v", err)
	}
	if string(body) != want {
		t.Fatalf("readResponseBody() = %q, want %q", body, want)
	}
}

func TestClientRejectsOversizedResponsesWithoutRetry(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusBadRequest, http.StatusInternalServerError} {
		t.Run(fmt.Sprintf("status-%d", status), func(t *testing.T) {
			requests := 0
			client := newDeterministicClient(t, func(req *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{
					StatusCode: status,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", 65))),
					Request:    req,
				}, nil
			}, WithMaxResponseBodySize(64))
			_, err := client.Models.List(t.Context(), nil)
			var tooLarge *ResponseTooLargeError
			if !errors.As(err, &tooLarge) {
				t.Fatalf("expected ResponseTooLargeError, got %T: %v", err, err)
			}
			if requests != 1 {
				t.Errorf("oversized body requests = %d, want 1", requests)
			}
		})
	}
}

func TestStandaloneParserUsesDefaultResponseBodyLimit(t *testing.T) {
	body := strings.Repeat("x", int(DefaultMaxResponseBodySize)+1)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	_, err := ParseSystemOneResponse(resp)
	var tooLarge *ResponseTooLargeError
	if !errors.As(err, &tooLarge) || tooLarge.Limit != DefaultMaxResponseBodySize {
		t.Fatalf("expected default response limit error, got %T: %v", err, err)
	}
}

type readErrorBody struct{}

func (readErrorBody) Read([]byte) (int, error) { return 0, errors.New("read failed") }
func (readErrorBody) Close() error             { return nil }

func TestResponseReadErrorsRemainConnectionErrors(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusOK, Body: readErrorBody{}}
	_, err := ParseSystemOneResponse(resp)
	var connectionErr *ConnectionError
	if !errors.As(err, &connectionErr) {
		t.Fatalf("expected ConnectionError, got %T: %v", err, err)
	}
}

// TestParseSystemOneResponseStandalone: the exported parser decodes a
// captured response outside the client, buffers the body for re-reading, and
// maps non-2xx statuses onto the typed taxonomy — the counterpart of
// Python's Response.from_http_response.
func TestParseSystemOneResponseStandalone(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://api.typesafe.ai/v1/systemone", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"X-Typesafe-Request-Id": []string{"req_p1"}},
		Request:    req,
		Body:       io.NopCloser(strings.NewReader(resultBody)),
	}
	result, err := ParseSystemOneResponse(resp)
	if err != nil {
		t.Fatalf("ParseSystemOneResponse: %v", err)
	}
	if result.Model != "jev-latest" {
		t.Errorf("Model = %q", result.Model)
	}
	if result.RequestID != "req_p1" {
		t.Errorf("RequestID = %q", result.RequestID)
	}
	if len(result.Nouls()) == 0 {
		t.Error("expected noul answers")
	}
	replayed, err := io.ReadAll(resp.Body)
	if err != nil || len(replayed) == 0 {
		t.Errorf("body must stay readable after parsing: %d bytes, %v", len(replayed), err)
	}
}

func TestParseResponseErrorPaths(t *testing.T) {
	if _, err := ParseSystemOneResponse(nil); err == nil {
		t.Error("nil response must be rejected")
	}
	if _, err := ParseListModelsResponse(nil); err == nil {
		t.Error("nil response must be rejected")
	}

	req, _ := http.NewRequest(http.MethodGet, "https://api.typesafe.ai/v1/models", nil)
	resp := &http.Response{
		StatusCode: http.StatusNotFound,
		Header:     http.Header{"X-Typesafe-Request-Id": []string{"req_p2"}},
		Request:    req,
		Body:       io.NopCloser(strings.NewReader(`{"message": "no such account"}`)),
	}
	_, err := ParseListModelsResponse(resp)
	var notFound *NotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("expected *NotFoundError, got %T: %v", err, err)
	}
	want := "GET https://api.typesafe.ai/v1/models: 404 no such account (request_id=req_p2)"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}

	resp = &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Request:    req,
		Body:       io.NopCloser(strings.NewReader(`{"models": [{"name": "m", "description": 5}]}`)),
	}
	_, err = ParseListModelsResponse(resp)
	validationErr := requireValidationError(t, err)
	if validationErr.FieldPath != "models[0].description" {
		t.Errorf("FieldPath = %q, want models[0].description", validationErr.FieldPath)
	}
}

func TestAnswerErrorsFollowDocumentOrder(t *testing.T) {
	// The Python SDK validates map entries in input order, so the first entry
	// in the JSON document is the one named in the error.
	body := `{"model": "x", "usage": {}, "answers": {"b": {"type": "noul"}, "a": {"type": "noul"}}}`
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, body)
	})
	client := newLoggedOutClient(t, server.URL)
	_, err := systemOneCall(t, client, "x", Questions{"q": RawQuestion{"type": "noul", "instructions": "?"}})
	validationErr := requireValidationError(t, err)
	if validationErr.FieldPath != "answers.b.noul" {
		t.Errorf("FieldPath = %q, want answers.b.noul (document order, not sorted)", validationErr.FieldPath)
	}
}

func TestSystemOneStructuralFailures(t *testing.T) {
	tests := []struct {
		name string
		body string
		path string
	}{
		{"empty body", ``, ""},
		{"null body", `null`, ""},
		{"array body", `[]`, ""},
		{"text body", `not json`, ""},
		{"missing usage", `{"model": "x"}`, "usage"},
		{"null usage", `{"model": "x", "usage": null}`, "usage"},
		{"usage wrong type", `{"model": "x", "usage": 5}`, "usage"},
		{"usage token wrong type", `{"model": "x", "usage": {"input_tokens": "1"}}`, "usage.input_tokens"},
		{"usage token not an integer", `{"model": "x", "usage": {"input_tokens": 1.5}}`, "usage.input_tokens"},
		{"model wrong type", `{"usage": {}, "model": 5}`, "model"},
		{"answers null", `{"model": "x", "usage": {}, "answers": null}`, "answers"},
		{"answers wrong type", `{"model": "x", "usage": {}, "answers": []}`, "answers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, tt.body)
			})
			client := newLoggedOutClient(t, server.URL)
			_, err := systemOneCall(t, client, "x", Questions{"q": RawQuestion{"type": "noul", "instructions": "?"}})
			validationErr := requireValidationError(t, err)
			if validationErr.FieldPath != tt.path {
				t.Errorf("FieldPath = %q, want %q", validationErr.FieldPath, tt.path)
			}
		})
	}
}

func TestUnknownAnswerTypeDropped(t *testing.T) {
	body := `{
	  "model": "test",
	  "usage": {"input_tokens": 1, "output_tokens": 1},
	  "answers": {
	    "spam": {"type": "noul", "noul": 0.9},
	    "mystery": {"type": "aurora", "value": 3}
	  }
	}`
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Typesafe-Request-Id", "req-9")
		writeJSON(w, http.StatusOK, body)
	})
	client := newLoggedOutClient(t, server.URL)
	result, err := systemOneCall(t, client, "text", Questions{"q": RawQuestion{"type": "noul", "instructions": "?"}})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if len(result.Answers) != 1 || result.Nouls()["spam"].Noul != 0.9 {
		t.Errorf("answers = %+v, want only spam", result.Answers)
	}
	// The unknown answer is still available on the raw response.
	raw := decodeJSONMap(t, readAllBody(t, result.Raw))
	answers := raw["answers"].(map[string]any)
	if answers["mystery"].(map[string]any)["type"] != "aurora" {
		t.Error("unknown answer should remain on Raw")
	}
	if result.RequestID != "req-9" {
		t.Errorf("RequestID = %q, want req-9", result.RequestID)
	}
}

func TestAnswerFieldFailures(t *testing.T) {
	tests := []struct {
		name  string
		entry string
		path  string
	}{
		{"noul null", `{"type": "noul", "noul": null}`, "answers.n.noul"},
		{"choice probabilities wrong type", `{"type": "choice", "choice": "a", "confidence": 1, "probabilities": []}`, "answers.c.probabilities"},
		{"choice bad probability value", `{"type": "choice", "choice": "a", "confidence": 1, "probabilities": {"a": "x"}}`, "answers.c.probabilities.a"},
		{"score missing score", `{"type": "score", "confidence": 1, "legend": {}, "probabilities": {}}`, "answers.s.score"},
		{"score legend bad value", `{"type": "score", "score": 1, "confidence": 1, "legend": {"0": 5}, "probabilities": {}}`, "answers.s.legend.0.str"},
		{"score probabilities bad key", `{"type": "score", "score": 1, "confidence": 1, "legend": {}, "probabilities": {"y": 0.5}}`, "answers.s.probabilities.y"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"model": "x", "usage": {}, "answers": {"` + tt.path[8:9] + `": ` + tt.entry + `}}`
			server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, body)
			})
			client := newLoggedOutClient(t, server.URL)
			_, err := systemOneCall(t, client, "x", Questions{"q": RawQuestion{"type": "noul", "instructions": "?"}})
			validationErr := requireValidationError(t, err)
			if validationErr.FieldPath != tt.path {
				t.Errorf("FieldPath = %q, want %q", validationErr.FieldPath, tt.path)
			}
		})
	}
}

func TestSystemOneSuccessDecodes(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Typesafe-Request-Id", "req-42")
		writeJSON(w, http.StatusOK, resultBody)
	})
	client := newLoggedOutClient(t, server.URL)
	result, err := systemOneCall(t, client, "text", Questions{"q": RawQuestion{"type": "noul", "instructions": "?"}})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if result.Model != "jev-latest" {
		t.Errorf("Model = %q", result.Model)
	}
	if result.Usage.InputTokens == nil || *result.Usage.InputTokens != 12 || result.Usage.OutputTokens == nil || *result.Usage.OutputTokens != 3 {
		t.Errorf("Usage = %+v", result.Usage)
	}
	if len(result.Answers) != 3 || len(result.Nouls()) != 1 || len(result.Choices()) != 1 || len(result.Scores()) != 1 {
		t.Fatalf("answer grouping wrong: %+v", result.Answers)
	}
	if result.Nouls()["spam"].Noul != 0.98 {
		t.Errorf("noul = %v", result.Nouls()["spam"].Noul)
	}
	choice := result.Choices()["tone"]
	if choice.Choice != "friendly" || choice.Confidence != 0.9 || choice.Probabilities["hostile"] != 0.1 {
		t.Errorf("choice = %+v", choice)
	}
	score := result.Scores()["quality"]
	if score.Score != 1.7 || score.Confidence != 0.8 {
		t.Errorf("score = %+v", score)
	}
	if len(score.Legend) != 3 || score.Legend[2] != "great" {
		t.Errorf("legend = %+v (int keys coerced?)", score.Legend)
	}
	if score.Probabilities[2] != 0.8 {
		t.Errorf("probabilities = %+v", score.Probabilities)
	}
	// Answer identity: the grouped views carry the same answer values.
	if _, ok := result.Answers["quality"].(ScoreAnswer); !ok {
		t.Error("grouped score should be the same answer value")
	}
	// Memoized views are stable across calls: the same map is returned.
	if first, second := fmt.Sprintf("%p", result.Nouls()), fmt.Sprintf("%p", result.Nouls()); first != second {
		t.Error("Nouls() should return the memoized map")
	}
	if first, second := fmt.Sprintf("%p", result.Choices()), fmt.Sprintf("%p", result.Choices()); first != second {
		t.Error("Choices() should return the memoized map")
	}
	if first, second := fmt.Sprintf("%p", result.Scores()), fmt.Sprintf("%p", result.Scores()); first != second {
		t.Error("Scores() should return the memoized map")
	}
	if result.RequestID != "req-42" || result.Raw == nil || result.Raw.StatusCode != 200 {
		t.Errorf("metadata missing: %+v", result)
	}
	if got := result.Raw.Header.Get("X-Typesafe-Request-Id"); got != "req-42" {
		t.Errorf("Raw.Header request id = %q, want req-42", got)
	}
}

func TestUnknownFieldsTolerated(t *testing.T) {
	body := `{
	  "model": "test",
	  "usage": {"input_tokens": 1, "output_tokens": 1, "reasoning_tokens": 9, "billing_units": 1},
	  "answers": {"spam": {"type": "noul", "noul": 0.9, "explanation": "spammy"}}
	}`
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, body)
	})
	client := newLoggedOutClient(t, server.URL)
	result, err := systemOneCall(t, client, "x", Questions{"q": RawQuestion{"type": "noul", "instructions": "?"}})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if result.Nouls()["spam"].Noul != 0.9 {
		t.Errorf("noul = %v", result.Nouls()["spam"].Noul)
	}
	raw := decodeJSONMap(t, readAllBody(t, result.Raw))
	if raw["usage"].(map[string]any)["reasoning_tokens"] == nil {
		t.Error("unknown usage fields should remain on Raw")
	}
}

func TestUsageTokensOptional(t *testing.T) {
	body := `{"model": "x", "usage": {}, "answers": {}}`
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, body)
	})
	client := newLoggedOutClient(t, server.URL)
	result, err := systemOneCall(t, client, "x", Questions{"q": Noul{Instructions: "?"}})
	if err != nil {
		t.Fatalf("SystemOne: %v", err)
	}
	if result.Usage.InputTokens != nil || result.Usage.OutputTokens != nil {
		t.Errorf("usage tokens should default to nil: %+v", result.Usage)
	}
	if len(result.Answers) != 0 {
		t.Errorf("answers should default to empty: %+v", result.Answers)
	}

	// Explicit nulls count as absent, like Python's Optional fields.
	body = `{"model": "x", "usage": {"input_tokens": null, "output_tokens": null}, "answers": {}}`
	server = newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, body)
	})
	client = newLoggedOutClient(t, server.URL)
	result, err = systemOneCall(t, client, "x", Questions{"q": Noul{Instructions: "?"}})
	if err != nil {
		t.Fatalf("SystemOne with null usage: %v", err)
	}
	if result.Usage.InputTokens != nil || result.Usage.OutputTokens != nil {
		t.Errorf("null usage tokens should stay nil: %+v", result.Usage)
	}
}

func TestModelsResponseDecodes(t *testing.T) {
	card := `{"name": "jev-latest", "description": "Fast model", "release_date": "2026-08-01"}`
	t.Run("shape", func(t *testing.T) {
		server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			writeJSON(w, http.StatusOK, `{"models": [`+card+`]}`)
		})
		client := newLoggedOutClient(t, server.URL)
		result, err := client.Models.List(t.Context(), nil)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(result.Models) != 1 || result.Models[0] != (ModelMetadata{Name: "jev-latest", Description: "Fast model", ReleaseDate: "2026-08-01"}) {
			t.Errorf("models = %+v", result.Models)
		}
	})

	t.Run("unknown fields ignored but kept on raw", func(t *testing.T) {
		server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, `{"models": [{"name": "jev-latest", "description": "d", "release_date": "2026-08-01", "context_window": 128000}]}`)
		})
		client := newLoggedOutClient(t, server.URL)
		result, err := client.Models.List(t.Context(), nil)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if result.Models[0].Name != "jev-latest" {
			t.Errorf("model = %+v", result.Models[0])
		}
		raw := decodeJSONMap(t, readAllBody(t, result.Raw))
		models := raw["models"].([]any)
		if models[0].(map[string]any)["context_window"] == nil {
			t.Error("unknown model fields should remain on Raw")
		}
	})

	t.Run("missing nested field paths", func(t *testing.T) {
		for _, missing := range []string{"name", "description", "release_date"} {
			full := `{"name": "test", "description": "Test model", "release_date": "2026-09-14"}`
			partial := stripJSONField(t, full, missing)
			body := `{"models": [` + full + `, ` + partial + `]}`
			server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, body)
			})
			client := newLoggedOutClient(t, server.URL)
			_, err := client.Models.List(t.Context(), nil)
			validationErr := requireValidationError(t, err)
			want := "models[1]." + missing
			if validationErr.FieldPath != want {
				t.Errorf("FieldPath = %q, want %q", validationErr.FieldPath, want)
			}
			wantMsg := `GET ` + server.URL + `/v1/models: 200 Invalid response data at 'models[1].` + missing + `'.`
			if validationErr.Error() != wantMsg {
				t.Errorf("Error() = %q, want %q", validationErr.Error(), wantMsg)
			}
		}
	})

	t.Run("invalid bodies", func(t *testing.T) {
		for _, body := range []string{``, `null`, `{}`, `{"models": "bad"}`, `{"models": [{"name": "x"}]}`} {
			server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, body)
			})
			client := newLoggedOutClient(t, server.URL)
			_, err := client.Models.List(t.Context(), nil)
			requireValidationError(t, err)
		}
	})
}

// --- helpers -----------------------------------------------------------------

// newLoggedOutClient builds a plain client pointed at a test server.
func newLoggedOutClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	// These tests talk to a real httptest server, so they cannot run under a
	// synctest fake clock. Backoff is disabled instead, keeping retryable
	// statuses instant without changing how many attempts are made.
	immediate := DefaultRetryPolicy()
	immediate.BackoffInitial = 0
	client, err := NewClient(WithAPIKey("test-key"), WithBaseURL(baseURL), WithAllowInsecureHTTP(), WithRetry(immediate))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(client.Close)
	return client
}

func readAllBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading raw body: %v", err)
	}
	return data
}

// stripJSONField removes one top-level key from a flat JSON object literal.
func stripJSONField(t *testing.T, object, field string) string {
	t.Helper()
	m := decodeJSONMap(t, []byte(object))
	delete(m, field)
	return mustMarshal(t, m)
}

func mustMarshal(t *testing.T, value any) string {
	t.Helper()
	encoded, err := marshalJSONCompact(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(encoded)
}

// TestIntKeyCoercionForms: integral float spellings and underscored keys
// coerce like Python's lax key coercion; fractions fail with the raw key.
func TestIntKeyCoercionForms(t *testing.T) {
	body := `{"model": "m", "usage": {}, "answers": {"s": {"type": "score", "score": 1, "confidence": 1,
	  "legend": {"1.0": "low", "2.0000": "high"}, "probabilities": {"1_0": 0.5}}}}`
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, body)
	})
	client := newLoggedOutClient(t, server.URL)
	result, err := systemOneCall(t, client, "x", Questions{"q": Noul{Instructions: "?"}})
	if err != nil {
		t.Fatalf("integral float keys should coerce: %v", err)
	}
	score := result.Scores()["s"]
	if _, ok := score.Legend[1]; !ok {
		t.Errorf("legend = %v, want key 1 coerced from \"1.0\"", score.Legend)
	}
	if _, ok := score.Legend[2]; !ok {
		t.Errorf("legend = %v, want key 2 coerced from \"2.0000\"", score.Legend)
	}
	if _, ok := score.Probabilities[10]; !ok {
		t.Errorf("probabilities = %v, want key 10 coerced from \"1_0\"", score.Probabilities)
	}

	for name, key := range map[string]string{"fraction": "1.5", "exponent": "1e1"} {
		bad := `{"model": "m", "usage": {}, "answers": {"s": {"type": "score", "score": 1, "confidence": 1,
		  "legend": {"` + key + `": "low"}, "probabilities": {}}}}`
		server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, bad)
		})
		client := newLoggedOutClient(t, server.URL)
		_, err := systemOneCall(t, client, "x", Questions{"q": Noul{Instructions: "?"}})
		validationErr := requireValidationError(t, err)
		if want := "answers.s.legend." + key; validationErr.FieldPath != want {
			t.Errorf("%s: FieldPath = %q, want %q", name, validationErr.FieldPath, want)
		}
	}
}

// TestParseResponseNilBody: a hand-built response without a Body reads as
// empty instead of panicking.
func TestParseResponseNilBody(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}}
	if _, err := ParseListModelsResponse(resp); err == nil {
		t.Fatal("an empty body must fail validation")
	}
	if resp.Body == nil {
		t.Error("Body should be re-attached as an empty reader")
	}
}
