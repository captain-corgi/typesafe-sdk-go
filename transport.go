package typesafe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net"
	"net/http"
	"runtime"
	"slices"
	"strings"
	"time"
)

var (
	// runtime.Version() reports "go1.27.1" for releases; strip the toolchain
	// prefix so the runtime header reads go/<semver>, not go/go<semver>.
	goRuntimeVersion = strings.TrimPrefix(runtime.Version(), "go")
	goOS             = runtime.GOOS
	goArch           = runtime.GOARCH
)

// preparedRequest is an immutable description of a single HTTP request.
type preparedRequest struct {
	method  string
	url     string
	path    string
	headers http.Header
	content []byte
	timeout time.Duration
}

// endpointFromResponse describes the originating request as
// "<METHOD> <url>" with credentials, query, and fragment removed, and an
// explicit default port (https:443, http:80) normalized away.
func endpointFromResponse(resp *http.Response) string {
	if resp.Request == nil || resp.Request.URL == nil {
		return ""
	}
	target := *resp.Request.URL
	target.User = nil
	target.RawQuery = ""
	target.Fragment = ""
	target.RawFragment = ""
	if port := target.Port(); port != "" {
		if (target.Scheme == "https" && port == "443") || (target.Scheme == "http" && port == "80") {
			target.Host = target.Hostname()
		}
	}
	return resp.Request.Method + " " + target.String()
}

// setHeader case-insensitively replaces any existing header of the same name
// so per-call extra headers win over client defaults, last write winning.
func setHeader(headers http.Header, name, value string) {
	for existing := range headers {
		if existing != name && !strings.EqualFold(existing, name) {
			continue
		}
		headers.Del(existing)
	}
	headers.Set(name, value)
}

// prepareRequest merges headers (client defaults → per-call extras, then
// protected headers force-set last), encodes the body, and resolves the
// per-attempt timeout.
func prepareRequest(cfg *config, method, path string, body []byte, timeout time.Duration, extraHeaders map[string]string) (*preparedRequest, error) {
	headers := cfg.defaultHeaders.Clone()
	if headers == nil {
		headers = http.Header{}
	}
	for _, name := range sortedHeaderNames(extraHeaders) {
		setHeader(headers, name, extraHeaders[name])
	}
	headers.Del(retryCountHeader)
	setHeader(headers, authorizationHeader, "Bearer "+cfg.apiKey)
	setHeader(headers, acceptHeader, jsonContentType)
	if body != nil {
		setHeader(headers, contentTypeHeader, jsonContentType)
	}
	setHeader(headers, userAgentHeader, sdkName+"/"+Version)
	setHeader(headers, sdkHeader, sdkName+"/"+Version)
	setHeader(headers, runtimeHeader, runtimeHeaderValue())

	resolved := cfg.timeout
	if timeout != 0 {
		resolved = timeout
	}
	return &preparedRequest{
		method:  method,
		url:     cfg.baseURL + path,
		path:    path,
		headers: headers,
		content: body,
		timeout: resolved,
	}, nil
}

// sortedHeaderNames makes extra-header application deterministic when a map
// holds the same name in multiple spellings.
func sortedHeaderNames(headers map[string]string) []string {
	return slices.Sorted(maps.Keys(headers))
}

// encodeBody marshals the wire body, wrapping encoding failures.
func encodeBody(body map[string]any) ([]byte, error) {
	encoded, err := marshalJSONCompact(body)
	if err != nil {
		return nil, newTypeSafeError("The request body could not be encoded as JSON")
	}
	return encoded, nil
}

// attemptResult carries what one HTTP attempt produced.
type attemptOutcome struct {
	resp  *http.Response // non-nil when the server answered
	body  []byte         // buffered response body
	value any            // parsed response value
	err   error
}

// execute runs the retry loop for a prepared request: it sends the request,
// classifies failures, waits between attempts per the policy, and always
// surfaces the last attempt's error when retries are exhausted. The parse
// callback turns a 2xx response into the typed result and may fail with a
// *ResponseValidationError.
func execute[T any](ctx context.Context, c *Client, req *preparedRequest, override *RetryPolicy, parse func(*http.Response, []byte) (T, error)) (T, error) {
	var zero T
	if err := c.checkClosed(); err != nil {
		return zero, err
	}
	policy := c.retry.copy()
	if override != nil {
		policy = override.copy()
	}
	if err := policy.Validate(); err != nil {
		return zero, err
	}
	if err := resolveTimeout(req.timeout); err != nil {
		return zero, err
	}
	parseAny := func(resp *http.Response, body []byte) (any, error) {
		return parse(resp, body)
	}

	start := time.Now()
	for attempt := 1; ; attempt++ {
		outcome := c.attempt(ctx, req, attempt, parseAny)
		if outcome.err == nil {
			if parsed, ok := outcome.value.(T); ok {
				return parsed, nil
			}
			return zero, nil
		}
		err := outcome.err

		// A caller-canceled context is never retried and propagates unwrapped.
		// This is checked before the retry condition so user predicates never
		// run for caller-side cancellation (Python would call them with
		// CancelledError).
		if ctx.Err() != nil && !isAPIError(err) {
			return zero, ctx.Err()
		}

		// The retry condition runs on every failed attempt — including the
		// final one, which is never retried — so RetryOn selectors and
		// Predicate observe each failure, like the Python policy.
		retryable := policy.retryable(err)
		if attempt > policy.MaxRetries {
			return zero, err
		}
		if !retryable {
			return zero, err
		}

		delay := policy.computeDelay(attempt, err, c.rand)
		if policy.Timeout > 0 {
			// Stop before a retry whose delay would reach the budget. The
			// comparison is written as delay >= Timeout-elapsed so a delay
			// clamped to the maximum duration cannot overflow the addition.
			if delay >= policy.Timeout-time.Since(start) {
				return zero, err
			}
		}
		if err := sleepWithContext(ctx, delay); err != nil {
			return zero, err
		}
	}
}

func isAPIError(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr)
}

// attempt performs exactly one HTTP exchange, classifying transport failures
// into the SDK error taxonomy and logging the exchange.
func (c *Client) attempt(ctx context.Context, req *preparedRequest, attemptNumber int, parse func(*http.Response, []byte) (any, error)) attemptOutcome {
	headers := req.headers.Clone()
	if attemptNumber > 1 {
		headers.Set(retryCountHeader, fmt.Sprintf("%d", attemptNumber-1))
		sdkLogger().Info("retry", "method", req.method, "url", req.url, "attempt", attemptNumber-1)
	}

	attemptCtx, cancel := context.WithTimeout(ctx, req.timeout)
	defer cancel()

	var bodyReader io.Reader
	if req.content != nil {
		bodyReader = bytes.NewReader(req.content)
	}
	httpReq, err := http.NewRequestWithContext(attemptCtx, req.method, req.url, bodyReader)
	if err != nil {
		// An unparseable URL fails immediately: unlike a transport failure,
		// it is deterministic, so (like the Python SDK, where the URL error
		// escapes unwrapped) it is never retried. The cause stays reachable
		// through Unwrap.
		return attemptOutcome{err: &TypeSafeError{
			msg:     fmt.Sprintf("Connection error: %v", err),
			wrapped: err,
		}}
	}
	httpReq.Header = headers

	sdkLogger().Debug("wire", "method", req.method, "path", req.path, "dir", "->", "headers", redactHeaders(headers), "body", formatLoggedBody(req.content))
	started := time.Now()

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		sdkLogger().Info("error", "method", req.method, "url", req.url, "error", transportErrorName(err))
		if ctx.Err() != nil {
			// The caller's context ended (cancellation or its own deadline);
			// propagate it unwrapped.
			return attemptOutcome{err: ctx.Err()}
		}
		if isTransportTimeout(err) {
			return attemptOutcome{err: newTimeoutError(req.timeout, err)}
		}
		return attemptOutcome{err: newConnectionError(err)}
	}

	body, readErr := readResponseBody(resp.Body, c.config.maxResponseBodySize)
	if readErr != nil {
		var tooLarge *ResponseTooLargeError
		if errors.As(readErr, &tooLarge) {
			return attemptOutcome{err: readErr}
		}
		sdkLogger().Info("error", "method", req.method, "url", req.url, "error", transportErrorName(readErr))
		if ctx.Err() != nil {
			// The caller's context ended (cancellation or its own deadline);
			// propagate it unwrapped.
			return attemptOutcome{err: ctx.Err()}
		}
		if isTransportTimeout(readErr) {
			return attemptOutcome{err: newTimeoutError(req.timeout, readErr)}
		}
		return attemptOutcome{err: newConnectionError(readErr)}
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))

	elapsed := time.Since(started)
	requestID := resp.Header.Get(requestIDHeader)
	if requestID == "" {
		requestID = "-"
	}
	sdkLogger().Info("response",
		"method", req.method, "url", req.url, "status", resp.StatusCode,
		"elapsed_ms", math.Round(float64(elapsed)/float64(time.Millisecond)), "request_id", requestID)
	sdkLogger().Debug("wire", "method", req.method, "path", req.path, "dir", "<-",
		"status", resp.StatusCode, "headers", redactHeaders(resp.Header), "body", formatLoggedBody(body))

	if err := responseStatusError(resp, body); err != nil {
		return attemptOutcome{resp: resp, body: body, err: err}
	}

	parsed, err := parse(resp, body)
	return attemptOutcome{resp: resp, body: body, value: parsed, err: err}
}

// isTransportTimeout reports whether a transport error represents a timeout:
// either the per-attempt deadline expired (the error chain carries
// context.DeadlineExceeded from the attempt context) or the underlying
// net.Error marks itself a timeout.
func isTransportTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// transportErrorName names the underlying error type for INFO logs.
func transportErrorName(err error) string {
	if cause := errors.Unwrap(err); cause != nil {
		return fmt.Sprintf("%T", cause)
	}
	return fmt.Sprintf("%T", err)
}
