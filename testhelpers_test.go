package typesafe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestServer starts an httptest server that is closed when the test ends.
// NewTestServer registers that cleanup itself but hands back an unstarted
// server, so Start must follow: it is what allocates the loopback listener the
// SDK's own transport dials, and what populates Server.URL.
func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewTestServer(t, handler)
	server.Start()
	return server
}

// roundTripperFunc adapts a function to an http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// recordedRequest captures a request's headers and wire body.
type recordedRequest struct {
	request *http.Request
	body    []byte
}

// trackingTransport records requests and CloseIdleConnections calls.
type trackingTransport struct {
	mu       sync.Mutex
	requests []recordedRequest
	closeIDs int
	inner    http.HandlerFunc
}

func (tt *trackingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body = readAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	tt.mu.Lock()
	tt.requests = append(tt.requests, recordedRequest{request: req, body: body})
	tt.mu.Unlock()

	recorder := httptest.NewRecorder()
	tt.inner(recorder, req)
	response := recorder.Result()
	if response.Header == nil {
		response.Header = http.Header{}
	}
	return response, nil
}

func (tt *trackingTransport) CloseIdleConnections() {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	tt.closeIDs++
}

func (tt *trackingTransport) requestCount() int {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return len(tt.requests)
}

func (tt *trackingTransport) closeCalls() int {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return tt.closeIDs
}

func readAll(r io.Reader) []byte {
	data, err := io.ReadAll(r)
	if err != nil {
		panic(err)
	}
	return data
}

// deterministicClient builds a client against a swappable round-tripper and
// times every attempt, so the gap between one attempt finishing and the next
// starting is the delay the retry loop slept.
//
// Jitter is pinned through the rand seam, but time is not faked here, so a
// test must run inside a [synctest.Test] bubble whenever it either reaches a
// nonzero backoff (the bubble clock makes the production sleep and
// elapsed-time paths resolve instantly instead of really waiting) or asserts
// on recordedDelays (only the bubble clock yields exact durations; against the
// real clock even an immediate retry measures a few nonzero nanoseconds).
// Tests that merely count attempts may run outside a bubble.
type deterministicClient struct {
	*Client

	mu      sync.Mutex
	inner   http.RoundTripper
	delays  []time.Duration
	lastEnd time.Time
}

func newDeterministicClient(t *testing.T, transport roundTripperFunc, opts ...Option) *deterministicClient {
	t.Helper()
	client, err := NewClient(append([]Option{WithAPIKey("test-key")}, opts...)...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(client.Close)
	dc := &deterministicClient{Client: client}
	if transport != nil {
		dc.inner = transport
	}
	// Send through dc itself so attempts are timestamped even after a test
	// swaps the delegate with setTransport.
	client.httpClient = &http.Client{Transport: dc}
	client.rand = func() float64 { return 0 }
	return dc
}

// setTransport replaces the round tripper that recorded attempts delegate to.
func (dc *deterministicClient) setTransport(transport roundTripperFunc) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.inner = transport
}

// RoundTrip records the gap since the previous attempt returned, then
// delegates. Under a fake clock that gap is exactly the slept backoff,
// because everything else the retry loop does between attempts is
// instantaneous.
func (dc *deterministicClient) RoundTrip(req *http.Request) (*http.Response, error) {
	started := time.Now()
	dc.mu.Lock()
	inner := dc.inner
	if !dc.lastEnd.IsZero() {
		dc.delays = append(dc.delays, started.Sub(dc.lastEnd))
	}
	dc.mu.Unlock()

	resp, err := inner.RoundTrip(req)

	dc.mu.Lock()
	dc.lastEnd = time.Now()
	dc.mu.Unlock()
	return resp, err
}

func (dc *deterministicClient) recordedDelays() []time.Duration {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return slices.Clone(dc.delays)
}

// resultBody is the canonical three-answer SystemOne success body used
// across response tests, mirroring the Python SDK's suite fixture.
const resultBody = `{
  "model": "jev-latest",
  "usage": {"input_tokens": 12, "output_tokens": 3},
  "answers": {
    "spam": {"type": "noul", "noul": 0.98},
    "tone": {"type": "choice", "choice": "friendly", "confidence": 0.9,
             "probabilities": {"friendly": 0.9, "hostile": 0.1}},
    "quality": {"type": "score", "score": 1.7, "confidence": 0.8,
                "legend": {"0": "bad", "1": "ok", "2": "great"},
                "probabilities": {"0": 0.1, "1": 0.1, "2": 0.8}}
  }
}`

// decodeJSONMap decodes JSON into a generic map for structural comparison.
func decodeJSONMap(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("decoding %s: %v", data, err)
	}
	m, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("expected a JSON object, got %T from %s", value, data)
	}
	return m
}

// writeJSON writes a status and JSON body to a test recorder.
func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprint(w, body)
}

// swapLogger installs l as the SDK logger for the test's duration.
func swapLogger(t *testing.T, l *slog.Logger) {
	t.Helper()
	previous := loggerPtr.Swap(l)
	t.Cleanup(func() { loggerPtr.Store(previous) })
}

// --- multipart and asset test helpers (mirroring the Python suite) ---------

// pngFixture and jpegFixture are the image fixtures shared by the asset
// tests; only format identification happens in the SDK.
var (
	pngFixture  = []byte("\x89PNG\r\n\x1a\nimage data")
	jpegFixture = []byte("\xff\xd8\xffimage data")
)

// assetOKBody is the empty System One success body used by asset tests.
const assetOKBody = `{"model": "vision", "usage": {}, "answers": {}}`

var (
	partNameRegex        = regexp.MustCompile(`name="([^"]+)"`)
	partContentTypeRegex = regexp.MustCompile(`Content-Type: ([^\r\n]+)`)
)

// multipartPart is one parsed part of a multipart/form-data body.
type multipartPart struct {
	head    []byte
	content []byte
}

// multipartParts splits a multipart/form-data request body into its parts'
// headers and contents, keyed by part name; the boundary is parsed from the
// Content-Type header, mirroring the Python suite's helper.
func multipartParts(t *testing.T, contentType string, body []byte) map[string]multipartPart {
	t.Helper()
	boundary, ok := strings.CutPrefix(contentType, "multipart/form-data; boundary=")
	if !ok {
		t.Fatalf("not a multipart content type: %q", contentType)
	}
	marker := []byte("--" + boundary)
	chunks := bytes.Split(body, marker)
	if len(chunks[0]) != 0 {
		t.Fatalf("body must start with the boundary marker, got %q", chunks[0])
	}
	if last := chunks[len(chunks)-1]; !bytes.Equal(last, []byte("--\r\n")) {
		t.Fatalf("body must end with the closing marker --%s--\\r\\n, got %q", boundary, last)
	}
	parts := make(map[string]multipartPart)
	for _, chunk := range chunks[1 : len(chunks)-1] {
		trimmed := bytes.TrimSuffix(bytes.TrimPrefix(chunk, []byte("\r\n")), []byte("\r\n"))
		head, content, _ := bytes.Cut(trimmed, []byte("\r\n\r\n"))
		match := partNameRegex.FindSubmatch(head)
		if match == nil {
			t.Fatalf("part head has no name: %q", head)
		}
		parts[string(match[1])] = multipartPart{head: head, content: content}
	}
	return parts
}

// requestBody decodes a request's JSON body; for a multipart upload, each
// asset's "part" is replaced by its "data" bytes and "content_type",
// mirroring the Python suite's helper.
func requestBody(t *testing.T, contentType string, body []byte) map[string]any {
	t.Helper()
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		return decodeJSONMap(t, body)
	}
	parts := multipartParts(t, contentType, body)
	decoded := decodeJSONMap(t, parts[requestPartName].content)
	entries, ok := decoded["assets"].(map[string]any)
	if !ok {
		t.Fatalf("multipart body has no assets map: %v", decoded["assets"])
	}
	for _, value := range entries {
		entry, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("asset entry is not an object: %v", value)
		}
		if _, has := entry["format"]; has {
			t.Fatalf("multipart asset entries must not carry format: %v", entry)
		}
		name, _ := entry["part"].(string)
		part, ok := parts[name]
		if !ok {
			t.Fatalf("asset part %q missing", name)
		}
		delete(entry, "part")
		entry["data"] = part.content
		match := partContentTypeRegex.FindSubmatch(part.head)
		if match == nil {
			t.Fatalf("part %q has no Content-Type header: %q", name, part.head)
		}
		entry["content_type"] = string(match[1])
	}
	return decoded
}

// newAssetCaptureClient builds a client whose transport records each request
// and answers with an empty System One success body.
func newAssetCaptureClient(t *testing.T) (*Client, *trackingTransport) {
	t.Helper()
	transport := &trackingTransport{inner: func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, assetOKBody)
	}}
	client, err := NewClient(WithAPIKey("test-key"), WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(client.Close)
	return client, transport
}

// systemOneWithAssets runs a minimal SystemOne request carrying assets.
func systemOneWithAssets(t *testing.T, client *Client, state JSONContent, questions Questions, assets map[string]*Image) (*SystemOneResponse, error) {
	t.Helper()
	return client.SystemOne(t.Context(), &SystemOneParams{State: state, Questions: questions, Assets: assets})
}

// capturedRequest is one recorded SystemOne request with its decoded body.
type capturedRequest struct {
	contentType string
	body        []byte
	decoded     map[string]any
}

// newBodyCaptureClient builds a client whose transport records each request
// body, decoded through requestBody, alongside the raw wire form.
func newBodyCaptureClient(t *testing.T) (*Client, *[]capturedRequest) {
	t.Helper()
	captured := &[]capturedRequest{}
	transport := &trackingTransport{inner: func(w http.ResponseWriter, r *http.Request) {
		data := readAll(r.Body)
		*captured = append(*captured, capturedRequest{
			contentType: r.Header.Get("Content-Type"),
			body:        data,
			decoded:     requestBody(t, r.Header.Get("Content-Type"), data),
		})
		writeJSON(w, http.StatusOK, assetOKBody)
	}}
	client, err := NewClient(WithAPIKey("test-key"), WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(client.Close)
	return client, captured
}
