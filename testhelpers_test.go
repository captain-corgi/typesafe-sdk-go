package typesafe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
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
