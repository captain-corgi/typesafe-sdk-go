package typesafe

import (
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrMissingAPIKey is wrapped by the error returned when no API key resolves
// from the constructor argument or the TYPESAFE_API_KEY environment variable.
var ErrMissingAPIKey = errors.New("missing API key")

// ErrClientClosed is wrapped by the error returned when a closed client is
// used again.
var ErrClientClosed = errors.New("client is closed")

// TypeSafeError is the root of every SDK error, like the Python SDK's
// TypeSafeError base class: API failures ([APIError] and its subclasses,
// including [ResponseValidationError]), transport failures
// ([ConnectionError]), timeouts ([TimeoutError]), and SDK-side failures all
// match `errors.As(err, &root)` with root of type *TypeSafeError, so a single
// catch-all handler sees every SDK-classified failure. Caller cancellation and
// deadlines are returned as unwrapped context errors and do not match this root.
// SDK-side failures —
// invalid arguments, configuration, request encoding — use this type
// directly and may wrap a sentinel such as [ErrMissingAPIKey] or
// [ErrClientClosed], which [errors.Is] matches.
type TypeSafeError struct {
	msg     string
	wrapped error
}

func newTypeSafeError(format string, args ...any) *TypeSafeError {
	return &TypeSafeError{msg: fmt.Sprintf(format, args...)}
}

// Error implements the error interface.
func (e *TypeSafeError) Error() string { return e.msg }

// Unwrap exposes the wrapped cause, if any.
func (e *TypeSafeError) Unwrap() error { return e.wrapped }

// asTypeSafeRoot satisfies an errors.As target of **TypeSafeError with a
// root carrying the error's own message, implementing the shared SDK root
// for error types that cannot embed it directly.
func asTypeSafeRoot(target any, message func() string) bool {
	if root, ok := target.(**TypeSafeError); ok {
		*root = &TypeSafeError{msg: message()}
		return true
	}
	return false
}

// APIError describes an unsuccessful HTTP response with its body and request
// metadata. Use [errors.As] to match the typed subclasses: [BadRequestError],
// [AuthenticationError], [PermissionDeniedError], [NotFoundError],
// [UnprocessableEntityError], [RateLimitError], [InternalServerError], and
// [ResponseValidationError]. Like every SDK error, it also matches the shared
// [TypeSafeError] root through errors.As.
type APIError struct {
	Status      int
	Body        string
	DecodedBody any
	Headers     http.Header
	Endpoint    string
	RequestID   string

	message          string
	requestIDPresent bool
}

// newAPIError builds an APIError, deriving its message from the decoded body.
func newAPIError(status int, rawBody string, decodedBody any, headers http.Header, endpoint string) *APIError {
	e := &APIError{
		Status:      status,
		Body:        rawBody,
		DecodedBody: decodedBody,
		Headers:     headers,
		Endpoint:    endpoint,
	}
	if id, present := headerValue(headers, requestIDHeader); present {
		e.RequestID = id
		e.requestIDPresent = true
	}
	e.message = deriveAPIMessage(decodedBody, rawBody)
	return e
}

// Error formats the status and message with the available request context:
// "<METHOD> <url>: <status> <message> (request_id=<id>)", omitting parts that
// are absent.
func (e *APIError) Error() string {
	message := strconv.Itoa(e.Status)
	if e.message != "" {
		message = fmt.Sprintf("%d %s", e.Status, e.message)
	}
	if e.Endpoint != "" {
		message = e.Endpoint + ": " + message
	}
	if e.requestIDPresent {
		message += fmt.Sprintf(" (request_id=%s)", e.RequestID)
	}
	return message
}

// As additionally matches the shared SDK root: errors.As(err, &root) with a
// *TypeSafeError target succeeds for this error and every subclass (the
// method is promoted through the embedded *APIError). A subclass built
// without its embedded *APIError cannot render a message and never matches.
func (e *APIError) As(target any) bool { return e != nil && asTypeSafeRoot(target, e.Error) }

// BadRequestError reports an invalid request (HTTP 400).
type BadRequestError struct{ *APIError }

// Error mirrors [APIError.Error].
func (e *BadRequestError) Error() string { return e.APIError.Error() }

// Unwrap matches the embedded [APIError] for [errors.As].
func (e *BadRequestError) Unwrap() error { return e.APIError }

// AuthenticationError reports failed authentication (HTTP 401).
type AuthenticationError struct{ *APIError }

// Error mirrors [APIError.Error].
func (e *AuthenticationError) Error() string { return e.APIError.Error() }

// Unwrap matches the embedded [APIError] for [errors.As].
func (e *AuthenticationError) Unwrap() error { return e.APIError }

// PermissionDeniedError reports denied access (HTTP 403).
type PermissionDeniedError struct{ *APIError }

// Error mirrors [APIError.Error].
func (e *PermissionDeniedError) Error() string { return e.APIError.Error() }

// Unwrap matches the embedded [APIError] for [errors.As].
func (e *PermissionDeniedError) Unwrap() error { return e.APIError }

// NotFoundError reports a missing resource (HTTP 404).
type NotFoundError struct{ *APIError }

// Error mirrors [APIError.Error].
func (e *NotFoundError) Error() string { return e.APIError.Error() }

// Unwrap matches the embedded [APIError] for [errors.As].
func (e *NotFoundError) Unwrap() error { return e.APIError }

// UnprocessableEntityError reports failed server-side validation (HTTP 422).
type UnprocessableEntityError struct{ *APIError }

// Error mirrors [APIError.Error].
func (e *UnprocessableEntityError) Error() string { return e.APIError.Error() }

// Unwrap matches the embedded [APIError] for [errors.As].
func (e *UnprocessableEntityError) Unwrap() error { return e.APIError }

// RateLimitError reports an exceeded rate limit (HTTP 429). RetryAfterMs is
// the server-requested wait in milliseconds, or nil when unavailable.
type RateLimitError struct {
	*APIError
	RetryAfterMs *float64
}

// Error mirrors [APIError.Error].
func (e *RateLimitError) Error() string { return e.APIError.Error() }

// Unwrap matches the embedded [APIError] for [errors.As].
func (e *RateLimitError) Unwrap() error { return e.APIError }

// InternalServerError reports a server failure (any HTTP status >= 500).
type InternalServerError struct{ *APIError }

// Error mirrors [APIError.Error].
func (e *InternalServerError) Error() string { return e.APIError.Error() }

// Unwrap matches the embedded [APIError] for [errors.As].
func (e *InternalServerError) Unwrap() error { return e.APIError }

// ResponseValidationError reports a successful HTTP response whose body was
// missing or structurally invalid required data. FieldPath is the dotted path
// to the offending field, such as "answers.tone.confidence".
type ResponseValidationError struct {
	*APIError
	FieldPath string
}

// Error mirrors [APIError.Error].
func (e *ResponseValidationError) Error() string { return e.APIError.Error() }

// Unwrap matches the embedded [APIError] for [errors.As].
func (e *ResponseValidationError) Unwrap() error { return e.APIError }

// ConnectionError reports a request that failed without an HTTP response,
// with the transport-level cause preserved for [errors.Unwrap].
type ConnectionError struct {
	cause error
	msg   string
}

func newConnectionError(cause error) *ConnectionError {
	return &ConnectionError{cause: cause, msg: fmt.Sprintf("Connection error: %v", cause)}
}

// Error implements the error interface.
func (e *ConnectionError) Error() string { return e.msg }

// Unwrap exposes the transport-level cause.
func (e *ConnectionError) Unwrap() error { return e.cause }

// As additionally matches the shared SDK root: errors.As(err, &root) with a
// *TypeSafeError target succeeds for connection failures.
func (e *ConnectionError) As(target any) bool { return e != nil && asTypeSafeRoot(target, e.Error) }

// TimeoutError reports a request that exceeded its configured timeout. It
// satisfies [net.Error], and Duration carries the resolved per-attempt
// timeout. Like the Python SDK — where the timeout error subclasses the
// connection error — [Unwrap] exposes a [*ConnectionError] carrying the
// transport-level cause, so a *TimeoutError also matches
// errors.As(err, &connErr). Because the transport cause chain is preserved,
// the error also satisfies errors.Is(err, context.DeadlineExceeded): when
// distinguishing SDK attempt timeouts from your own deadline, match SDK
// types with errors.As first and only treat DeadlineExceeded as caller-side
// once no SDK type matched.
type TimeoutError struct {
	Duration time.Duration
	conn     *ConnectionError
}

func newTimeoutError(duration time.Duration, cause error) *TimeoutError {
	return &TimeoutError{Duration: duration, conn: newConnectionError(cause)}
}

// Error implements the error interface.
func (e *TimeoutError) Error() string {
	return fmt.Sprintf("Request timed out (timeout=%s).", e.Duration)
}

// Unwrap exposes the connection error wrapping the transport-level cause.
func (e *TimeoutError) Unwrap() error { return e.conn }

// As additionally matches the shared SDK root: errors.As(err, &root) with a
// *TypeSafeError target succeeds for timeout failures.
func (e *TimeoutError) As(target any) bool { return e != nil && asTypeSafeRoot(target, e.Error) }

// Timeout reports that this error represents a timeout; it always holds true.
func (e *TimeoutError) Timeout() bool { return true }

// Temporary reports that a timeout is transient; it always holds true.
func (e *TimeoutError) Temporary() bool { return true }

// The TimeoutError satisfies net.Error.
var _ net.Error = (*TimeoutError)(nil)

// apiErrorFor maps a non-success status onto the typed error taxonomy:
// {400, 401, 403, 404, 422, 429} map to their subclasses, statuses >= 500 map
// to [InternalServerError], and anything else maps to a plain [APIError].
func apiErrorFor(status int, rawBody string, decodedBody any, headers http.Header, endpoint string) error {
	base := newAPIError(status, rawBody, decodedBody, headers, endpoint)
	switch status {
	case http.StatusBadRequest:
		return &BadRequestError{base}
	case http.StatusUnauthorized:
		return &AuthenticationError{base}
	case http.StatusForbidden:
		return &PermissionDeniedError{base}
	case http.StatusNotFound:
		return &NotFoundError{base}
	case http.StatusUnprocessableEntity:
		return &UnprocessableEntityError{base}
	case http.StatusTooManyRequests:
		retryAfter := parseRetryAfter(headers)
		return &RateLimitError{APIError: base, RetryAfterMs: retryAfter}
	}
	if status >= http.StatusInternalServerError {
		return &InternalServerError{base}
	}
	return base
}

// validationErrorFor builds a [ResponseValidationError] naming the first
// missing or structurally invalid field.
func validationErrorFor(resp *http.Response, body []byte, fieldPath string) *ResponseValidationError {
	rawBody := string(body)
	base := &APIError{
		Status:      resp.StatusCode,
		Body:        rawBody,
		DecodedBody: decodeJSONLenient(body),
		Headers:     resp.Header,
		Endpoint:    endpointFromResponse(resp),
		message:     fmt.Sprintf("Invalid response data at '%s'.", fieldPath),
	}
	if id, present := headerValue(resp.Header, requestIDHeader); present {
		base.RequestID = id
		base.requestIDPresent = true
	}
	return &ResponseValidationError{APIError: base, FieldPath: fieldPath}
}

// deriveAPIMessage extracts the human-readable message for an APIError,
// falling back to the raw body truncated to maxErrorBodyLength characters.
func deriveAPIMessage(decodedBody any, rawBody string) string {
	if detail := extractMessage(decodedBody); detail != "" {
		return detail
	}
	if decodedBody == nil {
		return "status code (no body)"
	}
	raw := rawBody
	if s, isString := decodedBody.(string); isString {
		raw = s
	} else if encoded, err := marshalJSONCompact(decodedBody); err == nil {
		raw = string(encoded)
	}
	return truncateMessage(raw)
}

// truncateMessage caps a body at maxErrorBodyLength characters (runes),
// appending an ellipsis when it had to cut.
func truncateMessage(raw string) string {
	runes := []rune(raw)
	if len(runes) > maxErrorBodyLength {
		return string(runes[:maxErrorBodyLength]) + "…"
	}
	return raw
}

// extractMessage pulls a server explanation out of a decoded error body,
// returning "" when nothing structured is found. Precedence: a plain string
// body; then the first string of "error", "error.message", "message",
// "detail", "detail.message"; then a FastAPI "detail" array joined as
// "path.to.field: msg; …" with the leading "body" location dropped.
func extractMessage(body any) string {
	switch value := body.(type) {
	case string:
		return value
	case map[string]any:
		if errValue, ok := value["error"]; ok {
			if s, isStr := errValue.(string); isStr {
				return s
			}
			if m, isMap := errValue.(map[string]any); isMap {
				if s, isStr := m["message"].(string); isStr {
					return s
				}
			}
		}
		if s, isStr := value["message"].(string); isStr {
			return s
		}
		if detail, ok := value["detail"]; ok {
			if s, isStr := detail.(string); isStr {
				return s
			}
			if m, isMap := detail.(map[string]any); isMap {
				if s, isStr := m["message"].(string); isStr {
					return s
				}
			}
			if entries, isList := detail.([]any); isList {
				return joinValidationDetails(entries)
			}
		}
	}
	return ""
}

// joinValidationDetails renders a FastAPI detail array as
// "path.to.field: msg; …", skipping entries without a string "msg" and
// dropping "body" location segments. Returns "" when no entry is usable.
func joinValidationDetails(entries []any) string {
	var parts []string
	for _, entry := range entries {
		dict, isDict := entry.(map[string]any)
		if !isDict {
			continue
		}
		msg, isStr := dict["msg"].(string)
		if !isStr {
			continue
		}
		path := ""
		if location, isList := dict["loc"].([]any); isList {
			var segments []string
			for _, item := range location {
				segment := fmt.Sprintf("%v", item)
				if segment == "body" {
					continue
				}
				segments = append(segments, segment)
			}
			path = strings.Join(segments, ".")
		}
		if path != "" {
			parts = append(parts, path+": "+msg)
		} else {
			parts = append(parts, msg)
		}
	}
	return strings.Join(parts, "; ")
}

// headerValue looks a header up case-insensitively and reports whether it was
// present at all, so an explicitly empty value is distinguishable from an
// absent one. Duplicate headers are comma-joined like the Python SDK's HTTP
// client renders them, so repeated Retry-After lines fail to parse rather
// than silently honoring only the first value.
func headerValue(headers http.Header, name string) (string, bool) {
	if headers == nil {
		return "", false
	}
	values, ok := headers[http.CanonicalHeaderKey(name)]
	if !ok {
		return "", false
	}
	if len(values) == 0 {
		return "", true
	}
	return strings.Join(values, ", "), true
}

// parseRetryAfter converts the retry-after-ms and retry-after response
// headers into a delay in milliseconds; nil means no usable value. The ms
// header wins; an empty header counts as zero; a negative or non-finite
// retry-after-ms falls through to retry-after, while a negative retry-after
// means no delay; a non-numeric retry-after is parsed as an HTTP date.
func parseRetryAfter(headers http.Header) *float64 {
	for _, header := range []struct {
		name       string
		multiplier float64
	}{{retryAfterMSHeader, 1}, {retryAfterHeader, 1000}} {
		raw, present := headerValue(headers, header.name)
		if !present {
			continue
		}
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			trimmed = "0"
		}
		if value, err := parseDecimalFloat(trimmed); err == nil {
			if !isFinite(value) {
				continue
			}
			if value >= 0 {
				delay := value * header.multiplier
				if isFinite(delay) {
					delayMs := delay
					return &delayMs
				}
				continue
			}
			if header.name == retryAfterHeader {
				return nil
			}
			continue
		}
		if header.name == retryAfterHeader {
			if date, err := http.ParseTime(raw); err == nil {
				remaining := time.Until(date).Seconds() * 1000
				if remaining < 0 {
					remaining = 0
				}
				return &remaining
			}
		}
	}
	return nil
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// parseDecimalFloat accepts only decimal float syntax, like Python's float().
// strconv.ParseFloat would additionally accept hexadecimal literals such as
// "0x1p4", which float() rejects.
func parseDecimalFloat(s string) (float64, error) {
	if strings.ContainsAny(s, "xXpP") {
		return 0, strconv.ErrSyntax
	}
	return strconv.ParseFloat(s, 64)
}
