# TypeSafe AI SDK — Behavioral Contracts (distilled from references)

Reference sources: `references/typesafe-sdk-python` (v0.7.5, primary) and
`references/typesafe-sdk-js` (v0.7.0, secondary). This document is the implementation-time
spec for the Go port. Where the two references differ, the Python v0.7.5 behavior wins.

Subsections marked **(v0.7.x)** were added or changed by the v0.7.0 → v0.7.5 sync
(images/Box/multipart, non-finite numbers, request validation, API-key validation,
keep-alive, zstd); everything else is the unchanged v0.7.0 contract.

---

## 1. Wire protocol

### Endpoints

| | |
|---|---|
| `POST /v1/systemone` | body `{"state": <content>, "model": "<id>", "questions": {<name>: <question>}}` + any extra user fields forwarded verbatim; becomes `multipart/form-data` when assets are sent (§1b) |
| `GET /v1/models` | no body |

### Question wire forms

```jsonc
// noul
{"type": "noul", "instructions": "...?" , "criteria": {"true": "...", "false": "..."}}  // instructions/criteria optional, omitted when unset
// choice — criteria REQUIRED
{"type": "choice", "instructions": "...?", "criteria": {"label": <desc-or-null>, ...}}
// score — criteria REQUIRED, ordered, non-empty, one entry per score from zero
{"type": "score", "instructions": "...?", "criteria": ["bad", "ok", "great"]}
// box (v0.7.5) — instructions optional, omitted when unset
{"type": "box", "instructions": "The dog"}
```

- Optional fields left unset are **omitted** from the wire; explicit `null`s **nested inside**
  criteria/instructions values are **preserved**.
- Raw dict questions are passed through **untouched** (unknown fields included) — the API is
  the schema validator for raw form; the client only checks minimal shape (see §6). A raw
  `{"type": "box", ...}` question gets only the generic raw checks (non-empty string `type`)
  — there are no box-specific raw validations.

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
                "probabilities": {"0": 0.1, "1": 0.7, "2": 0.2}},
    "<name>": {"type": "box",    "box": {"top": 0.1, "bottom": 0.9, "left": 0.2, "right": 0.8},   // v0.7.5; fractions of
                "confidence": 0.85}                                                               // height (top/bottom), width (left/right)
  }
}
// GET /v1/models
{"models": [{"name": "...", "description": "...", "release_date": "YYYY-MM-DD"}]}
```

Box answers (v0.7.5): coordinates are fractions of image height (`top`/`bottom`) and width
(`left`/`right`), each 0–1 and the expected value of its edge; field order is top, bottom,
left, right; `confidence` is 0–1 or `null` when the model does not provide it. Unknown
fields on an answer are ignored (still on `Raw`). The strict decode produces
`FieldPath`s of the shape `answers.<name>.box.<edge>`.

### Headers sent

| Header | Value | Overridable by user? |
|---|---|---|
| `Authorization` | `Bearer <api_key>` | **No** (protected) |
| `Accept` | `application/json` | **No** |
| `Content-Type` | `application/json` (only when a body exists); `multipart/form-data; boundary=<32 hex>` when assets are sent (§1b) | **No** |
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

### API-key validation (v0.7.1)

- Both sources are **edge-whitespace-stripped** (constructor argument and environment
  value). A resolved key must contain only printable ASCII without whitespace — control
  characters, DEL, non-ASCII, and internal spaces all fail with the static message
  `API key must contain only printable ASCII characters without whitespace.` (static so
  the key never leaks into errors; the key is likewise absent from logs — §5).
- Validation is **early**: at client construction, before any request. A non-empty but
  invalid explicit key errors **without falling back to the environment**.
- Go deviation: an explicit *empty* key still inherits `TYPESAFE_API_KEY` (the
  pre-existing Go zero-value ergonomics, README deviation 9), where Python v0.7.1
  errors on an explicit `""` with no env fallback.

### Connection keep-alive (v0.7.3)

- Python constructs its own HTTP clients with `Limits(max_connections=100,
  max_keepalive_connections=100, keepalive_expiry=30.0)` — keep every pooled connection
  alive 30 s; user-supplied HTTP clients are untouched.
- Go mapping (best effort, on the SDK-owned transport clone only): `MaxIdleConnsPerHost
  = 100` (Go's default is 2) and `IdleConnTimeout = 30s`. `MaxConnsPerHost` — Go's
  nearest knob to a max-connections cap — is deliberately left unset: it caps per-host
  connections by *queueing* excess requests before dialing, which would silently change
  latency semantics versus httpx2's pool (Python's cap is also per-client total, not
  per-host). The keep-alive goal (connection reuse) is met by the idle-conn settings
  alone. Transports supplied via `WithHTTPClient` are never mutated.

## 1b. Images, references, and asset parts (v0.7.5)

### Image construction and identity

- `NewImageFromBytes(data []byte)` — raw image file bytes; the bytes are **copied once, at
  first use**, so the slice may be reused or mutated afterwards. Never fails; validated
  lazily at first use.
- `NewImageFromFile(name string)` — path to an image file; **read when the request is
  prepared**, not at construction, so the file may be created after the image and before
  the call.
- `NewImageFromBase64(text string) (*Image, error)` — decodes **immediately**; strips all
  whitespace (wrapped base64 line breaks ignored). Errors: `Image data must contain
  non-empty ASCII base64 text.` (empty/whitespace-only/non-ASCII) and `Image data must
  contain valid base64 text.` (non-alphabet characters, bad padding).
- Python's `Image.from_pil` has **no Go counterpart** in this sync (no Pillow); users
  encode manually (e.g. `image/png`) and call `NewImageFromBytes`. Port note, not a Go
  contract.
- **Resolution is lazy and cached**: data is read and validated once, on first use, and the
  cached resolution is reused by every later request even if the source bytes or file
  change. The multipart payload is likewise identical across retries.
- Client-side data validation, with **no dimension/size limits**: empty data →
  `Image data is empty.`; unreadable path → `Image file '<path>' could not be
  read: <err>.`; a Go zero-value Image → `Image data is empty.`. Python's other
  two checks (str data, inaccessible buffer) are unrepresentable in Go — the
  constructors accept only `[]byte`, a path, or base64 text. Only format
  identification happens in the SDK; the API validates image contents (its
  documented 16 MiB/pixel/32 MiB limits are server-side).
- Images compare by **identity**, not content: two images built from identical bytes are
  distinct assets with distinct references.
- Content type is sniffed from the file signature only: PNG magic `"\x89PNG\r\n\x1a\n"` →
  `image/png`, JPEG magic `"\xff\xd8\xff"` → `image/jpeg`, anything else →
  `application/octet-stream` (still sent; the API detects the real format).

### References

- Each image mints a reference at construction: `<image:` + exactly 32 lowercase hex
  characters + `>` — 40 ASCII bytes, unique per instance, JSON-safe, identical in text and
  JSON. `Image.Reference()`/`Image.String()` return it; fmt functions interpolate it.
- References are found by regex `<image:[0-9a-f]{32}>` scanned over the **encoded JSON
  body bytes**, so a reference is found inside any string anywhere in the body (state,
  instructions, criteria, raw dicts, extra_body). The match is whole-value: 31/33 hex
  digits, uppercase hex, or a truncated form do **not** match; a well-formed lookalike that
  names no known image is sent as plain text with no asset.
- Reference text is **never rewritten** in the body — it stays verbatim, and the `assets`
  map is keyed by that same reference string.

### Placed images (in content) and the assets map

- An image **placed** in content (state, question instructions/criteria, raw dicts — any
  nesting, including as the entire state) serializes as its **alias** when it is a member
  of the `assets` map (by identity), else as its **reference**; every placed occurrence of
  one image maps to that single key.
- `assets` map validation: keys must be non-empty strings (`Asset aliases must be
  non-empty strings.`); values must be assets (`Asset '<name>' must be an asset.`); each
  distinct asset is resolved once by identity; resolution errors are prefixed per alias:
  `Asset '<name>': <error>`; the first failing alias (Go: in sorted-key order, where
  Python uses mapping order) is reported.
- **Alias + reference combine**: an image passed under an alias whose reference also
  appears as literal text yields two entries — the alias (for placed occurrences) and the
  reference (for the text match); the image is still read once.
- **Collision**: a reference equal to an existing alias key never overwrites the alias
  entry; one reference matched many times in the body produces one entry.
- **No client-side limits** on reference count, assets count, or total bytes; the only
  constraint is the fixed 40-byte reference shape. The `assets` key is omitted entirely
  when no assets are sent (the API rejects an empty `assets` map).

### Attachments (Go `AttachAssets`)

- `client.AttachAssets(assets ...*Image) (detach func(), err error)` is the Go shape of
  Python's `attach_assets` context manager (Go has no with-blocks; `defer detach()` is
  the idiomatic scope release). Enter-time validation rejects non-assets with Python's
  message (`Attached value <value> must be an asset.`); in Go, non-assets are already
  unrepresentable (`*Image` parameters) and only `nil` is rejected.
- Holds are **per-reference refcounts** under a mutex: an asset attached by overlapping
  scopes stays attached until the last scope detaches. The returned `detach` is
  **single-shot** (guarded by `sync.Once`) — holds being per-reference, an unguarded
  second call would decrement another holder's count and silently detach an in-scope
  asset; Python's context manager makes double-exit effectively impossible.
- During the scope, calls on **this client** send each attached asset whose reference
  appears in the request. Attachments nest (inner and outer both visible), are
  per-client, reach concurrent goroutines (snapshot taken under lock at call time), and
  are removed when the scope ends.

### Multipart transport

- A request carrying any assets **always** becomes `multipart/form-data`; the SDK never
  emits base64 `data_base64` assets — that wire form stays reachable solely through a
  user `ExtraBody["assets"]` override, exactly like Python.
- **ExtraBody override**: an `ExtraBody` containing the key `assets` replaces the SDK
  asset machinery entirely — placed and passed assets are neither read, encoded, nor
  validated (a missing file raises nothing) and the body stays plain JSON with the user's
  value verbatim.
- Boundary: 32 random lowercase hex characters (Python: `uuid4().hex`). Each part is
  `--{boundary}\r\nContent-Disposition: form-data; name="{name}"[; filename="{filename}"]\r\nContent-Type: {content_type}\r\n\r\n{content}\r\n`; the body ends
  `--{boundary}--\r\n` (RFC 7578). Part names/filenames are written unescaped, so they
  must be ASCII without quotes. The `Content-Type` header becomes
  `multipart/form-data; boundary={boundary}`; all other headers are preserved.
- **Part layout**: first part name `request`, `Content-Type: application/json`, **no**
  filename, content = the complete JSON body bytes; then one part per asset: name **and**
  filename both `asset-{index}`, `Content-Type` from the signature sniff, content = raw
  image bytes. `asset-{index}` indexes the pending enumeration: `assets`-map entries
  first — each uploaded **even when the body never names it** — then references in order
  of first appearance in the encoded body. Python numbers by mapping insertion order; Go
  maps are unordered, so Go fixes **sorted `Assets` keys** then body-scan order
  (documented deviation — part names are opaque to the server, only numbering differs).
- **Assets wire map**: `{key: {"type": "image", "part": "asset-{index}"}}`; keys are
  aliases then references as above; `type` precedes `part`; entries never carry `format`
  or `data_base64`. The API requires each part to be claimed by exactly one asset, so the
  same image under N names produces N entries and N parts (bytes sent once per name)
  while being read once.

## 1c. Number and body encoding deltas (v0.7.4/v0.7.5)

### Non-finite numbers (v0.7.4)

- JSON encoding stringifies non-finite floats, recursively anywhere in the body (state,
  nested arrays/objects, extra_body): NaN → the string `"NaN"`, +Inf → `"Infinity"`,
  -Inf → `"-Infinity"`; finite floats stay numbers. Example wire: `{"value": "NaN",
  "arr": [1, "Infinity", "-Infinity"]}`.
- Go coverage boundary (documented): covered — `float64`/`float32` NaN/±Inf and `*Image`
  substitution anywhere inside maps/slices/arrays reachable through the rewrite walk,
  **including pointer indirection** (JSON(`*T`) is byte-identical to JSON(`T`)), with
  typed numeric containers (`[]float64`, `map[string]float64`, `[N]float64`) converted
  to heterogeneous containers only when a substitution occurs. NOT covered — values
  inside arbitrary user **struct fields** (the walk never opens structs: a non-finite
  float field still fails with encoding/json's `json: unsupported value: +Inf`, and an
  `*Image` held in a struct field serializes via its `MarshalJSON` as an uncollected
  reference) and anything behind custom `json.Marshaler`/`TextMarshaler` (never invoked,
  per the questions.go marshaler-passthrough rule). Python's Content type has no struct
  or pointer analog; Go's `JSONContent` does, so the docs state this exact boundary.
- Typed-nil and bare-value handling (Go-specific): a nil `*Image` inside content
  serializes as JSON `null` (content containers may hold null elements) instead of
  panicking; a bare non-pointer `Image` value in content is rejected with `Images must
  be passed as *Image.` (surfaced verbatim, never wrapped in the generic encode
  message) rather than silently encoding `{}`.

### Request compression (v0.7.5 — Go deviation)

- Python compresses request bodies with zstd by default when a compressor is available
  (bodies ≥ 1024 bytes, `Content-Encoding: zstd`, body compressed once and reused for
  every retry). **Go does not implement it and gets no option**: the Go standard library
  has no zstd (`compress/` = flate, gzip, zlib, bzip2, lzw — verified via
  `ls $(go env GOROOT)/src/compress`); bodies ≥ 1024 bytes are sent uncompressed and
  `Content-Encoding` is never set. A `WithCompression` whose only non-default value
  could solely fail is API noise; ported callers get a docs pointer instead.
  **RISK FLAGGED**: if api.typesafe.ai ever *requires* `Content-Encoding: zstd` for
  large bodies, a stdlib-only Go SDK cannot comply without breaking the
  zero-dependency rule — owner's awareness required.

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
- Each answer must have a string `type`; the modeled set is {`noul`, `choice`, `score`,
  `box`} (v0.7.5 adds `box`); **unknown `type` values are dropped with a warning**
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

Raise `*TypeSafeError` (no request sent) when any of the following fail. Checks 1–4
and 9 predate v0.7.3; 5–8 are the v0.7.3 "more request validation checks" delta
(attributed by the Python changelog; the exact per-tag split is unverifiable — the
submodule carries only tag v0.7.5):

1. questions map is empty → `At least one question is required.`;
2. a value is neither a question object nor a dict with a non-empty string `type` →
   `Question "<name>" must be a question object or a dictionary with a nonempty string
   "type".`;
3. a raw choice/score question lacks a `criteria` key → `Question "<name>" requires
   "criteria".`;
4. score criteria (typed or raw) is empty → `Score question "<name>" has no criteria;
   at least one score is required.`;
5. choice criteria (typed or raw) is empty → `Choice question "<name>" has no criteria;
   at least one choice is required.` (v0.7.3);
6. noul criteria (typed or raw) is a non-null non-object → `Noul question "<name>"
   criteria must be an object.` (v0.7.3);
7. a noul question has neither instructions nor any non-null criteria value → `Noul
   question "<name>" has neither instructions nor criteria; at least one is required.`
   (v0.7.3);
8. `state` is nil → `State is required.` (v0.7.3);
9. timeout non-finite or ≤ 0 → `timeout must be a positive, finite number of seconds.`

Asset validation (v0.7.5) also happens pre-network, but **only when assets are actually
sent** (the resolve step) — it is skipped entirely when `ExtraBody` contains `assets`
(Python returns before `resolve_assets` runs): aliases must be non-empty strings;
values must be assets; image data must be non-empty and readable; each resolution error
is prefixed `Asset '<name>': …`. Messages and semantics: §1b.

For raw score criteria, follow ordinary pointer/interface chains with cycle
detection; a cycle raises `*TypeSafeError` before encoding. Custom JSON/text
marshalers are passed through for encoding and API validation, without inspecting
their underlying Go zero values or invoking them during validation. Nil pointers
still count as null. The request is encoded once per call and reused on retries.

Typed `Noul`/`Choice`/`Score`/`Box` structs are validated in the same pass (Go adaptation: one
code path for typed + raw, instead of Python's eager-construction validation).

Message contract for the body walk (Go): **only** cycle-guard failures reuse the generic
`The request body could not be encoded as JSON` text; every other walk/validation error
surfaces its own message verbatim. Read-error errno text uses Go's `*fs.PathError`
wording (lowercase `no such file or directory` vs Python's capitalized strerror) —
documented deviation; tests prefix-match exactly where Python's own assertion does
(`test_inline_images.py` stops at "could not be read").

## 7. Lifecycle & concurrency

- `Close()` closes idle connections of the underlying `*http.Client` — owned **and**
  user-supplied (`WithHTTPClient`).
- Using a closed client returns an error (`ErrClientClosed`).
- Client is safe for concurrent use; all per-call state (attempt counters, retry policy
  copy, budget deadline, headers) is call-local — concurrent calls with distinct
  model/timeout/retry overrides must not interfere (verified under `-race`).
- Attached assets (v0.7.5, §1b) are client-scoped registry state guarded by a mutex:
  attach/detach/snapshot are race-free, the snapshot is taken at call time, and
  refcounted holds keep an asset visible until its last scope detaches.

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

### v0.7.5 sync additions (ported from the Python suite)

- `assets_test.go` — reference shape/uniqueness/identity; placed images serialize as
  alias or reference everywhere (state, typed questions, raw dicts); alias + inline
  combine (alias part first, then reference); no assets key when none sent; buffers,
  paths (read at request preparation), base64 decode matrix; first failing alias
  reported; resolution cached (later data changes ignored); payload reused on retry;
  exact multipart part list and dispositions (`request` part without filename,
  `asset-N` with), assets map `{"type": "image", "part": "asset-N"}`, one part per key
  (Go asserts **sorted** alias order — deviation §1b/plan §6.14); octet-stream for unknown
  signatures; invalid assets before network.
- `inline_images_test.go` — references in formatted text; only referenced attached
  assets sent; attachments nest / are per-client / reach goroutines / hold until the
  last scope exits; unknown lookalike references stay text; ExtraBody `assets`
  suppresses reading; invalid attached value rejected at enter; placed-image errors
  named `Asset '<reference>': …`.
- `multipart_test.go` — byte-exact RFC 7578 framing, boundary shape, unescaped
  dispositions.
- Non-finite numbers: exact strings `"NaN"`/`"Infinity"`/`"-Infinity"`, nested and in
  `ExtraBody` (Python `test_clients.py`).
- Compression (Go deviation tests): `Content-Encoding` never set, bodies ≥ 1024 bytes
  sent uncompressed.
- Validation matrix incl. `State is required.` and the noul checks, before network.
- Config: early key validation, stripping, no env fallback for invalid explicit keys,
  key absent from error text (Python `test_config.py`).
- Keep-alive: SDK-owned transport tuned `MaxIdleConnsPerHost=100`,
  `IdleConnTimeout=30s`, `MaxConnsPerHost` unset; `WithHTTPClient` transports
  untouched.
- Box rows in the question/response matrices: `Box{}` → `{"type":"box"}`,
  instructions omitted when nil, unknown fields rejected on the typed struct; full
  `BoxAnswer` decode incl. extra fields ignored and `Boxes()` grouping;
  `answers.<name>.box.<edge>` FieldPaths; unknown answer types still dropped.
