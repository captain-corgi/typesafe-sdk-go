# Error handling

Every failure an SDK call can produce is a Go error matched with
`errors.As` (or `errors.Is` for sentinels). Owner: `errors.go`; taxonomy and
message extraction locked by `errors_test.go`, contract in
[contracts.md §2](../plans/2026-09-19-typesafe-sdk-go/contracts.md).

## Taxonomy

```mermaid
classDiagram
    class TypeSafeError {
        shared root of every SDK error
        directly used for SDK-side failures
        wraps ErrMissingAPIKey, ErrClientClosed
    }
    class APIError {
        Status
        Body raw wire text
        DecodedBody parsed JSON
        Headers
        Endpoint
        RequestID
    }
    class BadRequestError {
        400
    }
    class AuthenticationError {
        401
    }
    class PermissionDeniedError {
        403
    }
    class NotFoundError {
        404
    }
    class UnprocessableEntityError {
        422
    }
    class RateLimitError {
        429
        RetryAfterMs *float64
    }
    class InternalServerError {
        any status >= 500
    }
    class ResponseValidationError {
        2xx body failed schema validation
        FieldPath dotted path
    }
    class ConnectionError {
        no HTTP response
        transport cause preserved
    }
    class TimeoutError {
        attempt exceeded its timeout
        Duration resolved timeout
    }
    class NetError {
        <<interface>>
        net.Error
    }
    TypeSafeError <|.. APIError : matches via errors.As
    TypeSafeError <|.. ConnectionError
    TypeSafeError <|.. TimeoutError
    APIError <|-- BadRequestError
    APIError <|-- AuthenticationError
    APIError <|-- PermissionDeniedError
    APIError <|-- NotFoundError
    APIError <|-- UnprocessableEntityError
    APIError <|-- RateLimitError
    APIError <|-- InternalServerError
    APIError <|-- ResponseValidationError
    ConnectionError <|-- TimeoutError : Unwrap exposes *ConnectionError
    NetError <|.. TimeoutError : Timeout() / Temporary() true
```

Arrows mean "matches with `errors.As`". Go has no inheritance: each subclass
**embeds** `*APIError` and exposes `Unwrap() error` to it, which is why a
subclass matches both its specific type and `*APIError`. `*TimeoutError`
additionally unwraps to a `*ConnectionError` (mirroring the Python class
hierarchy) and satisfies `net.Error`.

## Matching recipes

```go
// A specific class — plus everything on it:
var rl *typesafe.RateLimitError
if errors.As(err, &rl) && rl.RetryAfterMs != nil {
    fmt.Println("retry after ms:", *rl.RetryAfterMs)
}

// Any non-2xx response:
var apiErr *typesafe.APIError
if errors.As(err, &apiErr) { /* apiErr.Status, Body, Headers, Endpoint, RequestID */ }

// A 2xx body that violated the response schema:
var rv *typesafe.ResponseValidationError
if errors.As(err, &rv) { log.Println(rv.FieldPath) }

// Catch-all — every SDK error matches the shared root:
var root *typesafe.TypeSafeError
if errors.As(err, &root) { log.Printf("typesafe call failed: %v", root) }

// SDK-side failures carry sentinels for errors.Is:
if errors.Is(err, typesafe.ErrMissingAPIKey) { ... }
if errors.Is(err, typesafe.ErrClientClosed) { ... }
```

**What is not an SDK error:** caller cancellation and deadline expiry return
`context.Canceled` / `context.DeadlineExceeded` directly, unwrapped and never
retried. They do not match `*TypeSafeError` — treat them as your own
cancellation, not an API failure.

## How a status becomes an error

```mermaid
flowchart TB
    m0["Non-2xx HTTP response"] --> m1{"Status in<br>400, 401, 403, 404, 422, 429?"}
    m1 -->|yes| m2["The mapped subclass<br>(429 also parses Retry-After headers)"]
    m1 -->|no| m3{"Status >= 500?"}
    m3 -->|yes| m4["*InternalServerError"]
    m3 -->|no| m5["Plain *APIError<br>(e.g. 3xx, 409 — note: 3xx surfaces as an error<br>because the SDK does not follow redirects)"]
    m2 --> m6["Error string: METHOD url: status message (request_id=...)"]
    m4 --> m6
    m5 --> m6
```

Example `Error()` output — parts are omitted when absent:

```
POST https://api.typesafe.ai/v1/systemone: 429 Too many requests (request_id=req_123)
```

## How the message is extracted

The human-readable message inside `*APIError` is pulled from the response
body with a fixed precedence (`extractMessage` + `deriveAPIMessage`):

```mermaid
flowchart TB
    x0["Decoded response body"] --> x1{"Whole body is a<br>JSON string?"}
    x1 -->|yes| m["use that string"]
    x1 -->|no| x2{"error (string), then<br>error.message"}
    x2 -->|found| m
    x2 -->|no| x3{"message?"}
    x3 -->|found| m
    x3 -->|no| x4{"detail (string),<br>then detail.message?"}
    x4 -->|found| m
    x4 -->|no| x5{"detail is a FastAPI array<br>of loc/msg entries?"}
    x5 -->|yes| m2["Join as path.to.field: msg; ...<br>body loc segments dropped"]
    x5 -->|no| x6["Raw body truncated to 200 chars + ellipsis<br>empty body: status code (no body)"]
```

Details that matter:

- FastAPI `loc` segments are joined with **dots** (`questions.0`) — distinct
  from `ResponseValidationError.FieldPath`, which uses `[i]`.
- Invalid-JSON bodies decode leniently as UTF-8 text (invalid runs collapse
  to one replacement character) and fall through to truncation.
- The API key never appears in any error message or log output.

## Connection and timeout failures

Failures without an HTTP response are classified in the attempt layer
(`transport.go`): a per-attempt deadline expiry or a `net.Error` timeout
becomes `*TimeoutError`; any other transport failure becomes
`*ConnectionError` with the cause preserved via `Unwrap`. A caller context
that ends mid-flight propagates `ctx.Err()` unwrapped instead. Which of these
get retried is [Retries and timeouts](retries.md)'s subject.
