# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Security hardening options and errors: `WithMaxResponseBodySize`,
  `WithAllowInsecureHTTP`, `SetLogBodyMode`/`TYPESAFE_LOG_BODY`,
  `ErrResponseTooLarge`/`ResponseTooLargeError`, and `ErrInvalidBaseURL`.

- Cookbook examples now mirror the docs use-case map one-for-one:
  `examples/automation-use-cases/` (one program per example automation use
  case — search & retrieval, scientific discovery, model routing, semantic
  code linting, recruiting, insurance claims, financial crime, legal
  compliance, marketplaces, moderation, advertising, gaming, risk,
  forecasting, knowledge graphs, ...) and `examples/task-categories/` (one
  per example task category — classification, detection, scoring, routing,
  search, retrieval, ranking, verification, ML feature extraction,
  structured data extraction). The former `guardrails`, `ticket-triage`,
  `lead-scoring`, `intent-routing`, and `rerank` examples moved into these
  groups as `llm-guardrails`, `customer-support`, `lead-generation`,
  `routing`, and `ranking`; `quickstart` and `retries-errors` stay at the
  `examples/` root.

### Security

- Deliberate hardening behavior changes: wire body logging is redacted by
  default, response buffering is bounded to 16 MiB, base URLs require HTTPS
  (with loopback-only HTTP opt-in), and `ExtraBody` cannot override `state`,
  `model`, or `questions`.

### Changed

- Go 1.27 idiom refresh across the SDK and examples (no public API change):
  map-key sorting uses `slices.Sorted(maps.Keys(m))` instead of
  `sort.Strings`/`sort.SliceStable`, retry jitter uses `math/rand/v2`,
  counted loops use `for i := range n`, and tests use `t.Context()` and
  `sync.WaitGroup.Go`.
- Retry and backoff tests now run inside `testing/synctest` bubbles against
  the production sleep path — observed by wrapping the round tripper, so the
  real `time.Sleep` is exercised — cutting the suite's wall time roughly 3×.
  Test servers use the Go 1.27 `httptest.NewTestServer(t, handler)` +
  `Start()` form.
- Duplicate object names in API bodies are now pinned by tests to
  last-wins decoding (matching Python's `json.loads`), guarding both error
  message extraction and response-answer decoding against a strict-parser
  regression.
- CI now covers ubuntu (Go 1.27.x and stable, with `-race`), Windows, and
  macOS, and adds `staticcheck`, `govulncheck`, `go mod tidy -diff`, and
  `go mod verify` to keep the zero-dependency claim proven.

### Removed

- The package-internal `sleep` and `now` test seams on `Client`, along with
  the fake-clock harness: retry timing in tests is now measured inside
  `synctest` bubbles instead. Only the `rand` jitter seam remains.

### Fixed

- Parity fixes from the third 2026-09-19 SDK parity review (Python v0.7.0
  baseline; no `Version` bump — the parity baseline is unchanged):
- `X-TypeSafe-Runtime` no longer carries a doubled toolchain prefix: it now
  reads `go/<goversion> (<GOOS>; <GOARCH>)` (e.g. `go/1.27.1 (linux;
  amd64)`), the `<lang>/<semver>` shape the Python and JS SDKs send and the
  documented contract specifies, instead of `go/go1.27.1 …`.
- The retry condition — `RetryPolicy.Predicate` and `RetryOn` selectors —
  now runs on every failed attempt, including the final attempt that
  exceeds `MaxRetries` and is never retried, matching the Python policy
  where tenacity evaluates the retry condition before the stop strategy.
  Outcomes are unchanged; counting/logging predicates now observe every
  failure. Caller-cancellation still returns before the predicate runs.
- The SDK-owned default HTTP client now runs on a private clone of
  `http.DefaultTransport`, so `Client.Close` no longer evicts idle
  connections pooled on the process-global transport by other code, and the
  SDK's pool is no longer shared with `http.DefaultClient` users.
- Error-handling and responses documentation now state the
  `errors.Is(err, context.DeadlineExceeded)` matching order (SDK attempt
  timeouts match it too — rule out SDK types with `errors.As` first) and
  the comma-ok pattern for per-kind answer lookups (Python raises `KeyError`
  where Go maps return zero values; single-answer lookup helpers are
  deliberately not provided).
- Raw score validation detects cyclic pointer/interface chains and returns a
  typed SDK error before encoding or network I/O instead of looping indefinitely.
- Custom JSON/text marshalers in raw score criteria are preserved for encoding
  and API validation, even when their underlying Go values are empty. Validation
  does not invoke marshalers, and retries reuse the already-encoded body.
- Configuration and error documentation now distinguish preserved explicit
  strings from blank environment values and unwrapped caller-context errors from
  SDK-classified failures.

- Parity fixes from the 2026-09-19 SDK parity review (Python v0.7.0 baseline;
  no `Version` bump — the parity baseline is unchanged):
- `WithHTTPClient` timeout inheritance no longer caps longer SDK timeouts:
  the resolved per-attempt timeout (explicit or inherited from the injected
  client's `Timeout` when `WithTimeout` is unset) is enforced through the
  attempt context on an SDK-owned copy of the client with its outer
  `Timeout` disabled, so the injected client's own deadline never shortens
  an explicitly selected timeout. The caller's client is never modified.
- Raw score-question validation now rejects empty criteria of ordinary Go
  container types (`[]string{}`, `map[string]string{}`, typed and named
  containers, typed nils) before any network I/O, matching the Python
  truthiness check instead of only the decoded `[]any`/`map[string]any`
  forms.
- `RetryPolicy.RetryOn` sentinel entries now match strictly by identity via
  `errors.Is`: an unrelated error of the same concrete type (for example,
  another `errors.New` value) no longer triggers a retry. SDK error
  instances (`&NotFoundError{}`) and typed-nil entries
  (`(*MyError)(nil)`) remain type selectors, and type matching now follows
  the full error tree via `errors.As` — wrapping, `errors.Join`, and custom
  `As` methods all match. Use `Predicate` for custom class-based matching.
- All SDK errors — `APIError` subclasses, `ResponseValidationError`,
  `ConnectionError`, and `TimeoutError` — now match the shared
  `*TypeSafeError` root through `errors.As` (the counterpart of the Python
  SDK's `TypeSafeError` base class), so one catch-all handler sees every
  SDK failure. Specific-type, `*APIError`, timeout-to-connection, and
  `net.Error` matches are unchanged, as are error strings and metadata.

## [0.7.5.0] - 2026-11-01

Sync to Python SDK v0.7.5 parity (`Version = "0.7.5"`). Carries the v0.7.1 →
v0.7.5 deltas: multimodal image support, the Box question type, non-finite
number serialization, extra request validation, early API-key validation,
30 s connection keep-alive, and zstd request compression as a documented
deviation — zstd is **not implemented and gets no option**: Go's standard
library has no zstd, so bodies ≥ 1024 bytes are always sent uncompressed and
`Content-Encoding` is never set (README "Deviations from the Python SDK"
§12 flags the risk if the API ever requires it).

### Added

- Multimodal image support. `Image` (alias `Asset`) is created with
  `NewImageFromBytes` (bytes copied once, at first use), `NewImageFromFile`
  (read lazily, when the request is prepared), or `NewImageFromBase64`
  (whitespace-tolerant, decoded immediately). Every image mints a stable
  reference — `<image:` + 32 hex characters + `>` — returned by
  `Reference()`/`String()` and interpolatable with `fmt.Sprintf`. Images may
  be placed anywhere in state or question content (serialized as their alias
  when a member of the call's `Assets` map, else as their reference) or
  referenced from any text; references are found by scanning the encoded
  body. A request carrying assets becomes `multipart/form-data`: a `request`
  JSON part first, then one part per asset (`asset-N`, content type sniffed
  from the PNG/JPEG signature, else `application/octet-stream`), with an
  `assets` map added to the JSON body. `Asset = Image` mirrors Python's
  alias.
- `Client.AttachAssets(assets ...*Image) (detach func(), error)` — the Go
  shape of `attach_assets`: attaches images for every call on the client
  until the returned single-shot detach runs (`defer detach()`). Holds are
  per-reference refcounts, so overlapping scopes keep an image attached
  until the last one detaches; attachments are per-client, nest, and reach
  concurrent goroutines.
- `Box` question (`{"type": "box", "instructions": ...}`, instructions
  optional) answered by `BoxAnswer{Box BoxCoordinates, Confidence *float64}`
  with `BoxCoordinates{Top, Bottom, Left, Right float64}` as fractions of
  image height/width; `SystemOneResponse.Boxes()` groups box answers by
  question name like `Nouls`/`Choices`/`Scores`.
- Multimodal cookbook programs under `examples/multimodal/`:
  `image-in-state`, `box-locate-and-crop`, `image-sources`,
  `attached-assets`, and `image-in-choice-criteria`.

### Changed

- Requests carrying assets are sent as `multipart/form-data` (RFC 7578,
  random 32-hex boundary); the JSON body rides the first part named
  `request`. The SDK never emits base64 assets — `data_base64` stays
  reachable only through a user `ExtraBody["assets"]` override, which also
  suppresses reading and validating any passed/placed/attached images
  (Python parity).
- Non-finite numbers (`NaN`, `±Inf`) anywhere in a request body — including
  `ExtraBody` — serialize as the strings `"NaN"`, `"Infinity"`, and
  `"-Infinity"` instead of failing to encode. Coverage is the body walk
  (maps, slices, arrays, pointers); struct fields and custom
  `json.Marshaler`/`TextMarshaler` values are outside it (documented).
- The SDK-owned default HTTP transport now keeps 100 idle connections per
  host alive for 30 s (`MaxIdleConnsPerHost=100`, `IdleConnTimeout=30s`),
  mirroring the Python SDK's connection pool. `WithHTTPClient`-supplied
  transports are never mutated.

### Fixed

- Parity with Python v0.7.1–v0.7.5:
- The resolved API key is validated at construction — printable ASCII
  without whitespace — with a static message that never interpolates the
  key; a non-empty but invalid explicit key no longer falls back to the
  environment. Both API-key sources (option and `TYPESAFE_API_KEY`) are
  edge-stripped first.
- Additional pre-network request validation, all raising
  `*TypeSafeError` with zero HTTP attempts: a nil state (`State is
  required.`), empty choice criteria, noul criteria that is not an object,
  and noul questions with neither instructions nor a described outcome.
- Image and asset errors surface their own messages verbatim before any
  network I/O (`Asset aliases must be non-empty strings.`, `Asset '<name>'
  must be an asset.`, `Asset '<name>': <error>`, `Image data is empty.`,
  base64 decode errors, `Image file '<path>' could not be read: <error>.`,
  `Images must be passed as *Image.`); only body-walk cycle failures reuse
  the generic `The request body could not be encoded as JSON` text.

## [0.7.0] - 2026-09-19

Initial release, at feature parity with the Python SDK v0.7.0.

### Added

- `Client.SystemOne`: typed answers to named questions about text or
  structured state in a single request, with per-call model, timeout, retry
  policy, extra headers, and extra body overrides.
- Question primitives: `Noul` (yes/no probability), `Choice`
  (classification with per-label probabilities), `Score` (rubric scoring with
  legend and per-level probabilities), plus `RawQuestion` wire passthrough.
- `Client.Models.List`: the models available to the account.
- Typed response objects with grouped views (`Nouls`, `Choices`, `Scores`),
  token usage, request ID, and a buffered raw HTTP response.
- Error taxonomy matched with `errors.As`: `APIError` subclasses for
  400/401/403/404/422/429/5xx, `ResponseValidationError` with a dotted
  `FieldPath`, `ConnectionError`, and `TimeoutError` (satisfies `net.Error`).
- Configurable retries: exponential backoff with jitter, `Retry-After`
  support, per-call budget, custom statuses, `RetryOn`, and predicates.
- `log/slog` logging (silent by default) with `TYPESAFE_LOG_LEVEL` and
  secret-header redaction.
- Environment configuration: `TYPESAFE_API_KEY`, `TYPESAFE_BASE_URL`,
  `TYPESAFE_DEFAULT_MODEL`, `TYPESAFE_LOG_LEVEL`.
- Cookbook examples under `examples/` (stdlib only).

[0.7.0]: https://github.com/captain-corgi/typesafe-sdk-go/releases/tag/v0.7.0
[0.7.5.0]: https://github.com/captain-corgi/typesafe-sdk-go/releases/tag/v0.7.5.0
