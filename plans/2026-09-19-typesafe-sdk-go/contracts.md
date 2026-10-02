# TypeSafe AI SDK — Behavioral Contracts (distilled from references)

Reference sources: `references/typesafe-sdk-python` (v0.7.0, primary) and
`references/typesafe-sdk-js` (v0.6.0, secondary). This document is the implementation-time
spec for the Go port. Where the two references differ, the Python v0.7.0 behavior wins.

---

## 1. Wire protocol

### Endpoints

| | |
|---|---|
| `POST /v1/systemone` | body `{"state": <content>, "model": "<id>", "questions": {<name>: <question>}}` + any extra user fields forwarded verbatim |
| `GET /v1/models` | no body |

### Question wire forms

```jsonc
// noul
{"type": "noul", "instructions": "...?" , "criteria": {"true": "...", "false": "..."}}  // instructions/criteria optional, omitted when unset
// choice — criteria REQUIRED
{"type": "choice", "instructions": "...?", "criteria": {"label": <desc-or-null>, ...}}
// score — criteria REQUIRED, ordered, non-empty, one entry per score from zero
{"type": "score", "instructions": "...?", "criteria": ["bad", "ok", "great"]}
```

- Optional fields left unset are **omitted** from the wire; explicit `null`s **nested inside**
  criteria/instructions values are **preserved**.
- Raw dict questions are passed through **untouched** (unknown fields included) — the API is
  the schema validator for raw form; the client only checks minimal shape (see §6).

### Response bodies

```jsonc
// POST /v1/systemone
{
  "model": "jev-latest",
  "usage": {"input_tokens": 123, "output_tokens": 456},   // both optional
  "answers": {
    "<name>": {"type": "noul",   "noul": 0.92},
    "<name>": {"type": "choice", "choice": "billing", "confidence": 0.9,
                "probabilities": {"billing": 0.9, "technical": 0.1}},
    "<name>": {"type": "score",  "score": 1.4, "confidence": 0.8,
                "legend": {"0": "bad", "1": "ok", "2": "great"},   // values: str | obj | list; keys are stringified ints
                "probabilities": {"0": 0.1, "1": 0.7, "2": 0.2}}
  }
}
// GET /v1/models
{"models": [{"name": "...", "description": "...", "release_date": "YYYY-MM-DD"}]}
```

### Headers sent

| Header | Value | Overridable by user? |
|---|---|---|
| `Authorization` | `Bearer <api_key>` | **No** (protected) |
| `Accept` | `application/json` | **No** |
| `Content-Type` | `application/json` (only when a body exists) | **No** |
| `User-Agent` | `typesafe-sdk/<version>` | **No** |
| `X-TypeSafe-SDK` | `typesafe-sdk/<version>` | **No** |
| `X-TypeSafe-Runtime` | `go/<goversion> (<GOOS>; <GOARCH>)` | **No** |
| `X-TypeSafe-Retry-Count` | `"<n>"` attempt number — set on retries only | **No** (always stripped from user input) |

Merge order: client default headers → per-call extra headers (case-insensitive, last-wins) →
strip `X-TypeSafe-Retry-Count` → force-set protected headers last.

### Headers consumed

- `x-typesafe-request-id` → `RequestID` on the response
- `retry-after` (seconds or HTTP-date) and `retry-after-ms` → retry delay (§4)

### Defaults & environment

| | |
|---|---|
| Base URL | `https://api.typesafe.ai` (trailing `/` stripped) — env `TYPESAFE_BASE_URL` |
| Model | `jev-latest` — env `TYPESAFE_DEFAULT_MODEL` |
| Timeout | 10 s per attempt — precedence: per-call > client > default |
| API key | ctor arg > `TYPESAFE_API_KEY` (trimmed; whitespace-only = unset) > error |
| Log level | `TYPESAFE_LOG_LEVEL` ∈ `debug \| info \| warn \| warning \| error \| off` |

## 2. Error contract

### Taxonomy (Python names → Go types)

```
TypeSafeError                       *TypeSafeError (base, wraps sentinels)
├── APIError                        *APIError{Status, Body, Headers, Endpoint, RequestID}
│   ├── BadRequestError             *BadRequestError            (400)
│   ├── AuthenticationError         *AuthenticationError        (401)
│   ├── PermissionDeniedError       *PermissionDeniedError      (403)
│   ├── NotFoundError               *NotFoundError              (404)
│   ├── UnprocessableEntityError    *UnprocessableEntityError   (422)
│   ├── RateLimitError              *RateLimitError (429)       + RetryAfterMs *float64
│   ├── InternalServerError         *InternalServerError        (any status ≥ 500)
│   └── APIResponseValidationError  *ResponseValidationError    + FieldPath string
├── APIConnectionError              *ConnectionError            (no HTTP response; cause preserved)
└── APITimeoutError                 *TimeoutError               (satisfies net.Error; timeout preserved)
```

- Subclasses embed `*APIError` and expose `Unwrap() error` so `errors.As` matches both
  specific and base; `*TimeoutError` implements `Timeout() bool` / `Temporary() bool`.
- Status → type: {400, 401, 403, 404, 422, 429} mapped; unmapped < 500 → base `*APIError`;
  ≥ 500 → `*InternalServerError`.
- `Error()` format: `"<METHOD> <url>: <status> <message> (request_id=<id>)"` with parts
  omitted when absent — e.g. `GET https://api.typesafe.ai/v1/models: 429 Server explanation (request_id=req_123)`.

### Message extraction precedence (`extractMessage`)

1. whole body is a JSON string → that string
2. `error` (string)
3. `error.message`
4. `message`
5. `detail` (string)
6. `detail.message`
7. `detail` is a FastAPI array `[{loc, msg}, …]` → join as `"path.to.field: msg; …"`
   (all `"body"` elements of `loc` dropped; all segments, including integer indices,
   joined with `.`, e.g. `questions.0`; this is distinct from response-validation
   FieldPath formatting, which uses `[i]`)
8. otherwise: raw body truncated to 200 chars + `"…"`; empty/null body → `"status code (no body)"`

Invalid JSON bodies decode leniently as UTF-8 text (replacement chars) and fall through to 8.

### `parseRetryAfter` → milliseconds (`*float64`, nil = absent)

- Check `retry-after-ms` first (multiplier 1), then `retry-after` (multiplier 1000).
- Numeric parse; empty string counts as 0; negative `retry-after-ms` → try next header;
  negative `retry-after` → nil; non-finite → nil.
- Non-numeric `retry-after` parsed as HTTP-date (`http.ParseTime`) → `max(0, date − now)` in ms.

## 3. Response decode contract

- Non-2xx → error taxonomy (§2), never a response value.
- Strict-ish decode of success bodies into typed structs; failures raise
  `*ResponseValidationError` with dotted `FieldPath`:
  - Answer decoded independently per name; stdlib type errors prefixed `answers.<name>.`
    (Python parity examples: `answers.n.noul`, `answers.tone.confidence`).
  - List indices formatted `[i]` (e.g. `models[1].name`).
  - Bad map keys that must coerce to int (score `legend`/`probabilities`) produce paths like
    `answers.s.legend.x`.
- Each answer must have a string `type`; **unknown `type` values are dropped with a warning**
  (forward compatibility) — the raw payload remains accessible on `Raw`.
- Unknown fields elsewhere are ignored. Usage token counts are optional.
- `RequestID` + buffered `Raw *http.Response` attached to every response.

## 4. Retry contract

### `RetryPolicy` defaults

| Field | Default | Notes |
|---|---|---|
| `MaxRetries` | 2 | 0 disables; total attempts = MaxRetries + 1 |
| `BackoffInitial` | 500 ms | 0 disables backoff (immediate re-attempt) |
| `BackoffMax` | 5 s | cap on the un-jittered exponential |
| `BackoffJitter` | 0.25 | fraction subtracted, ∈ [0, 1] |
| `HTTPStatuses` | {408, 429, 500–599} | retryable `*APIError` statuses |
| `RespectRetryAfter` | true | server-requested delay wins over backoff |
| `APIConnectionError` | true | retry `*ConnectionError` |
| `APITimeoutError` | true | retry `*TimeoutError` |
| `RetryOn` | ∅ | extra error types (matched via `errors.As`) |
| `Predicate` | nil | custom `func(error) bool` |
| `Timeout` | 30 s | total budget per SDK call; 0 = unlimited |

Validation (at construction / `Validate()`): MaxRetries ≥ 0; backoffs ≥ 0 and finite;
jitter ∈ [0,1]; budget ≥ 0 (zero disables the budget in Go).

### Dispatch

Retry an error iff any of: `*TimeoutError` && APITimeoutError; `*ConnectionError` &&
APIConnectionError; other `*APIError` && status ∈ HTTPStatuses; matches `RetryOn`;
or `Predicate(err)` returns true. The retry condition is evaluated on **every failed
attempt** — including the final one beyond MaxRetries, which is never retried — so
selectors and predicates observe each failure, like tenacity. Caller-context
cancellation is **never** retried, propagates unwrapped, and returns before the
condition runs. The exhausted error surfaced to the user is always the **last**
attempt's error. Policy snapshotted per call (per-call `Retry` replaces the client policy).

### Backoff math

- `exponential = min(initial · 2^(attempt−1), max)` with an exponent guard to avoid float
  overflow (must survive initial=1e-300 / max=1e300 extremes).
- `delay = exponential · (1 − rand()·jitter)`, rounded to 3 decimals, never above the
  un-jittered cap. Default sequence: 0.5 → 1.0 → 2.0 → 4.0 → 5.0 → 5.0…
  Jitter floor at rand=1.0 with defaults: 0.375.
- Server `Retry-After` is honored regardless of length (even > budget), when RespectRetryAfter.
- Budget semantics (**stop-before-delay**): do not begin an attempt whose preceding cumulative
  delay would reach the total budget; the budget resets per SDK call (per-call state only).
  This does not interrupt an in-flight attempt; its own timeout and caller context apply.

### Per-attempt behavior

- Each attempt runs under its own timeout (resolved per §1); attempt-deadline expiry →
  `*TimeoutError` carrying the resolved timeout; caller abort → ctx error, unretried.
- Retry attempts set `X-TypeSafe-Retry-Count: <n>` (first attempt sends nothing).
- One structured INFO record per re-attempt (`msg=retry`), one per response
  (`msg=response`), DEBUG `msg=wire` dumps with redacted headers (§5).

## 5. Logging contract

- Logger attribution `typesafe_sdk`, silent by default (no-op handler) unless `TYPESAFE_LOG_LEVEL` set
  or the user supplies a handler/logger.
- Records are structured `log/slog` records. Python's `"%s %s <- %s in %.0fms"` format strings are
  record *templates* in their logging calls, not literal output — the structured record is the Go
  analog, not a deviation:
  - INFO `msg=response`: `method`, `url`, `status`, `elapsed_ms` (rounded to whole ms), `request_id`
    (`"-"` when the header is absent).
  - INFO `msg=retry` (before each re-attempt): `method`, `url`, `attempt` (the retry number,
    i.e. attempt−1).
  - INFO `msg=error` (transport failure): `method`, `url`, `error` (the transport cause's Go type).
  - DEBUG `msg=wire`: `method`, `path`, `dir` (`->` request / `<-` response), plus `headers`
    (redacted), `body`, and on responses `status`.
- Redaction (single choke point for DEBUG header dumps): header names in
  {`authorization`, `proxy-authorization`, `x-api-key`, `api-key`, `cookie`, `set-cookie`}
  (case-insensitive) **or containing** `token`/`secret` → value masked. Bodies are not redacted.
- API key never appears in any error message or log.

## 6. Pre-network validation (before any I/O)

Raise `*TypeSafeError` (no request sent) when:

- questions map is empty;
- a raw (map) question lacks a non-empty string `type`;
- a raw choice/score question lacks a `criteria` key;
- score criteria (typed or raw) is empty.

For raw score criteria, follow ordinary pointer/interface chains with cycle
detection; a cycle raises `*TypeSafeError` before encoding. Custom JSON/text
marshalers are passed through for encoding and API validation, without inspecting
their underlying Go zero values or invoking them during validation. Nil pointers
still count as null. The request is encoded once per call and reused on retries.

Typed `Noul`/`Choice`/`Score` structs are validated in the same pass (Go adaptation: one
code path for typed + raw, instead of Python's eager-construction validation).

## 7. Lifecycle & concurrency

- `Close()` closes idle connections of the underlying `*http.Client` — owned **and**
  user-supplied (`WithHTTPClient`).
- Using a closed client returns an error (`ErrClientClosed`).
- Client is safe for concurrent use; all per-call state (attempt counters, retry policy
  copy, budget deadline, headers) is call-local — concurrent calls with distinct
  model/timeout/retry overrides must not interfere (verified under `-race`).

## 8. Key test contracts to port (from the reference suites)

- Round trip: exact wire body `{state, model, questions}` — nil optionals omitted, explicit
  nested nulls kept, extra fields forwarded; answers keyed by question names.
- Error mapping table with **exact `Error()` strings** (endpoint, status, message, request id).
- Retry table: {408, 429, 500–599} retried; {400, 401, 403, 404, 409, 422, 302} not;
  retry-count header sequence `[absent, "1", "2"]`; MaxRetries=n → n+1 attempts; Retry-After
  (s / ms / HTTP-date / 0 / negative / garbage) → exact delays; backoff sequence + jitter
  floor via injected clock/rand; budget stop-before-delay math; per-call override isolation.
- Timeout precedence: call > client > default 10 s.
- Config precedence: arg > env > default; whitespace-only env ignored; missing key errors.
- Response validation matrix: exact `FieldPath` strings (`answers.n.noul`, `models[1].name`,
  `answers.s.legend.x`); unknown answer type dropped with warning + present on `Raw`.
- Header protection: user headers can never override the protected set (§1) case-insensitively.
- Validation before network: empty questions / bad raw shape → error, zero HTTP attempts.
- Concurrency: parallel calls with distinct overrides under `-race`.
