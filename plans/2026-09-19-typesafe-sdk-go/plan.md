# Plan: `typesafe-sdk-go` — Go port of the TypeSafe AI SDK

- **Date:** 2026-09-19 · **v0.7.5 sync:** 2026-11-01
- **Status:** Approved to persist (implementation awaits explicit go-ahead)
- **Baseline:** Python SDK v0.7.5 (primary, feature parity) · TS SDK v0.7.0 (secondary, cross-check)
- **Decisions locked with user:**
  - Module path: `github.com/captain-corgi/typesafe-sdk-go`, package `typesafe` (import path ≠ package name, like `go-openai`)
  - Go version: 1.27.1
  - **Zero runtime dependencies** — Go stdlib only (mirrors the TS SDK's zero-dep philosophy)
  - **Full Python parity** feature set — v0.7.0 at first release; synced to v0.7.5 (images/Box/multipart, non-finite numbers, request validation, API-key validation, keep-alive; zstd is a documented deviation, §6.12)
  - Full repo scaffolding (README, LICENSE, CHANGELOG, CI)
  - Cookbook examples under `examples/*` (§8) so users can start fast

---

## 1. What we're building

A Go client SDK for the **TypeSafe AI API** (`https://api.typesafe.ai`) — an LLM service that
answers *typed, named questions* about a piece of "state" in a single request ("System One"):

- `POST /v1/systemone` — body `{"state", "model", "questions": {name: question}}` → typed answers + usage
- `GET /v1/models` — list available models
- Four question primitives:
  - **Noul** — yes/no → `noul: float` (probability of true, 0..1)
  - **Choice** — classification → `choice: string`, `confidence`, `probabilities: map[label]float`
  - **Score** — rubric scoring against ordered criteria → `score: float` (expected value), `confidence`, `legend: map[level]any`, `probabilities: map[level]float`
  - **Box** (v0.7.5) — locate what the instructions describe in the request's image → `box: {top, bottom, left, right}` fractions + `confidence`
- **Image assets** (v0.7.5) — images referenced from generated reference strings, an assets map, multipart upload, and `AttachAssets` scope attachment (contracts §1b)

"Type safety" = the question kind you ask determines the statically-known answer type you get back.
No codegen; the wire structs are hand-written (the set is small and stable).

## 2. Target user experience

```go
client, err := typesafe.NewClient() // reads TYPESAFE_API_KEY
if err != nil { ... }

resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
    State: map[string]any{"document": "I was charged twice. Please fix this ASAP."},
    Questions: typesafe.Questions{
        "category": typesafe.Choice{
            Instructions: "What is this ticket about?",
            Criteria:     typesafe.ChoiceCriteria{"billing": nil, "technical": nil, "other": nil},
        },
        "urgent": typesafe.Noul{Instructions: "Is this urgent?"},
        "tone": typesafe.Score{
            Instructions: "How polite is the tone?",
            Criteria:     typesafe.ScoreCriteria{"rude", "neutral", "polite"},
        },
    },
})
if err != nil { ... }

fmt.Println(resp.Choices()["category"].Choice) // "billing"
fmt.Println(resp.Nouls()["urgent"].Noul)       // 0.92

modelsResp, err := client.Models.List(ctx, nil) // *typesafe.ListModelsResponse
fmt.Println(modelsResp.Models[0].Name)
```

Per-call overrides, error handling, retries:

```go
resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
    State: ..., Questions: ...,
    Model:        "jev-latest",                    // per-call model
    Timeout:      5 * time.Second,                 // per-call timeout
    Retry:        &typesafe.RetryPolicy{MaxRetries: 4},
    ExtraHeaders: map[string]string{"X-Custom": "v"},
    ExtraBody:    map[string]any{"custom_field": 1}, // last-write-wins merge; "state", "model", "questions" are reserved and rejected
})

var rl *typesafe.RateLimitError
if errors.As(err, &rl) { fmt.Println(rl.RetryAfterMs) }
```

## 3. Source layout (single flat `typesafe` package at repo root)

| File | Contents |
| --- | --- |
| `doc.go` | Package overview doc |
| `version.go` | `Version = "0.7.5"` (signals parity baseline) |
| `constants.go` | Public consts: `EnvAPIKey`/`EnvBaseURL`/`EnvDefaultModel`/`EnvLogLevel` = `TYPESAFE_API_KEY`/`TYPESAFE_BASE_URL`/`TYPESAFE_DEFAULT_MODEL`/`TYPESAFE_LOG_LEVEL`, `DefaultBaseURL = "https://api.typesafe.ai"`, `DefaultModel = "jev-latest"`, `DefaultTimeout = 10 * time.Second`. Internal: paths (`/v1/systemone`, `/v1/models`), header names (`Authorization`, `Accept`, `Content-Type`, `User-Agent`, `X-TypeSafe-SDK`, `X-TypeSafe-Runtime`, `X-TypeSafe-Retry-Count`, `x-typesafe-request-id`, `retry-after`, `retry-after-ms`), `maxErrorBodyLength = 200`, `secretHeaders`; v0.7.5: PNG/JPEG signature bytes + `application/octet-stream` fallback, `multipart/form-data; boundary=` prefix, `request`/`asset-%d` part names, keep-alive tuning (`keepAliveConnsPerHost = 100`, `keepAliveIdleTimeout = 30s`), `maxLoggedBodyBytes = 16 KiB` |
| `json.go` | `type JSONContent = any` (documented: string \| map[string]any \| []any, nil allowed nested), `type JSONValue = any`; body marshaling that **preserves explicit nested `null`s** and omits nil question optionals; lenient unmarshal (invalid JSON → UTF-8 text fallback with replacement chars) |
| `config.go` | Config resolution: ctor arg > trimmed env (whitespace-only = unset) > default; base-URL trailing-slash strip; timeout validation (positive, finite); v0.7.1: both API-key sources edge-stripped, resolved key validated as printable ASCII without whitespace (static message, key never leaks), invalid explicit key errors with no env fallback |
| `questions.go` | `Noul`/`Choice`/`Score`/`Box` structs + `NoulCriteria{True, False JSONContent}` / `ChoiceCriteria map[string]JSONContent` / `ScoreCriteria []JSONContent`; sealed `Question` interface incl. `RawQuestion map[string]any` (wire passthrough — API validates, client checks only minimal shape); custom `MarshalJSON` per question type: always emit `"type"` first, omit nil `instructions`/`criteria`, preserve nested explicit nulls; `normalizeQuestions` pre-network validation → `*TypeSafeError` **before any I/O**: non-empty questions map; raw dicts need non-empty string `type`; raw choice/score need `criteria`; score **and** choice criteria non-empty (v0.7.3); noul criteria must be an object when present and instructions-or-criteria required (v0.7.3); question names visited in sorted order for deterministic failures |
| `assets.go` | v0.7.5: `Image` (alias `Asset`) — constructors `NewImageFromBytes` (lazy copy at first use), `NewImageFromFile` (lazy read at request preparation), `NewImageFromBase64` (immediate whitespace-tolerant decode); `Reference()`/`String()` return the mints-at-construction `<image:32-hex>` reference; `MarshalJSON` emits the reference (context-free fallback outside the client walk); memoized one-shot `resolve()` with the data checks (empty data, unreadable path, zero-value image); signature sniffing (PNG/JPEG/else octet-stream); `resolvePendingAssets` ordered validation naming the failing alias |
| `attached.go` | v0.7.5: per-reference refcounted attachment registry (`attach`/`detach`/`snapshot` under a mutex) + `(*Client).AttachAssets(assets ...*Image) (detach func(), error)` — enter-time nil rejection with Python's message, single-shot detach (`sync.Once`) so a double call cannot decrement another holder |
| `content.go` | v0.7.5: `rewriteContent` body walk — substitutes placed `*Image` values with their alias (Assets member by identity) or reference, stringifies non-finite floats (`NaN`/`Infinity`/`-Infinity`, v0.7.4), unwraps pointers, follows maps/slices/arrays with copy-on-write rebuilds (untouched subtrees pass by reference so image-free bodies encode byte-identically), Map/Slice/Ptr-keyed cycle guard (pointer-only cycles escape encoding/json's detector and would overflow the stack), marshaler/TextMarshaler passthrough, typed-nil `*Image` → `null`, bare `Image` value → verbatim error |
| `multipart.go` | v0.7.5: RFC 7578 multipart encoder — 32-hex random boundary, unescaped ASCII dispositions, `--{boundary}--\r\n` close; returns content type + bytes |
| `responses.go` | Sealed `Answer` interface: `NoulAnswer{Noul float64}`, `ChoiceAnswer{Choice string; Confidence float64; Probabilities map[string]float64}`, `ScoreAnswer{Score, Confidence float64; Legend map[int]any; Probabilities map[int]float64}` (JSON string keys → int coercion via `strconv.Atoi`), `BoxAnswer{Box BoxCoordinates; Confidence *float64}` (v0.7.5; nil confidence = unreported) with `BoxCoordinates{Top, Bottom, Left, Right float64}` fractions; `SystemOneResponse{Model string; Usage Usage; Answers map[string]Answer}` with `Nouls()/Choices()/Scores()/Boxes()` views (type-filtered, `sync.Once` memoized) + exported `RequestID string`, `Raw *http.Response` (body swapped for buffered `NopCloser`); `Usage{InputTokens, OutputTokens *int}`; `ListModelsResponse{Models []ModelMetadata}`, `ModelMetadata{Name, Description, ReleaseDate string}`. Strict decode producing `*ResponseValidationError` with **dotted `FieldPath`** (decode each answer independently from `json.RawMessage`, wrap stdlib type errors, prefix with `answers.<name>.`, list indices as `[i]`, e.g. `answers.tone.confidence`, `models[1].name`, `answers.where.box.top`); known answer types {noul, choice, score, box}; **unknown answer `type` dropped with `slog.Warn`** (forward compat; raw payload still on `Raw`) |
| `errors.go` | Taxonomy mirroring Python: `*TypeSafeError` base; `*APIError{Status int; Body string; Headers http.Header; Endpoint string; RequestID string}` with `Error()` = `"POST https://api.typesafe.ai/v1/systemone: 429 <msg> (request_id=req_123)"` (parts omitted when absent); typed subclasses embedding `*APIError` + `Unwrap() error` so `errors.As` matches both specific and base: `*RateLimitError` (+ `RetryAfterMs *float64`), `*BadRequestError` (400), `*AuthenticationError` (401), `*PermissionDeniedError` (403), `*NotFoundError` (404), `*UnprocessableEntityError` (422), `*InternalServerError` (≥500); `*ConnectionError` (cause chain preserved via `Unwrap`); `*TimeoutError` (satisfies `net.Error`, carries resolved timeout); `*ResponseValidationError{FieldPath string}`; sentinels `ErrClientClosed`, `ErrMissingAPIKey`. Status→type map {400,401,403,404,422,429}; unmapped <500 → base `*APIError`; ≥500 → `*InternalServerError`. `extractMessage(body)` precedence: string body → `error` (str) → `error.message` → `message` → `detail` (str) → `detail.message` → FastAPI `detail: [{loc, msg}]` joined as `"path.to.field: msg; …"` (all `"body"` loc elements dropped; integer segments joined with dots) → raw body truncated to 200 chars + `"…"` → `"status code (no body)"`. `parseRetryAfter(headers) *float64` (ms): `retry-after-ms` first, then `retry-after` (×1000; seconds numeric or HTTP-date via `http.ParseTime` → `max(0, date−now)`); empty string = 0; negative → none; non-finite → none |
| `retry.go` | `RetryPolicy` struct, `DefaultRetryPolicy()`, `Validate() error`: `MaxRetries int` (2; 0 disables), `BackoffInitial time.Duration` (500ms; 0 disables backoff), `BackoffMax` (5s), `BackoffJitter float64` (0.25, fraction subtracted, ∈[0,1]), `HTTPStatuses map[int]struct{}` ({408, 429, 500–599}), `RespectRetryAfter bool` (true), `APIConnectionError bool` (true), `APITimeoutError bool` (true), `RetryOn []error` (custom error set), `Predicate func(error) bool` (nil), `Timeout time.Duration` (total budget per SDK call, default 30s; 0 = unlimited). Backoff: `min(initial·2^(attempt−1), max) · (1 − rand()·jitter)` with exponent-overflow guard, rounded to 3 decimals, never above un-jittered cap. Retry-After honored regardless of length (even > budget). Budget semantics: **stop before** an attempt whose cumulative delay would reach the budget (`stop_before_delay`). Package-internal `jitterRand` hook for deterministic jitter (the sleep path needs no seam — timing tests run under `testing/synctest`) |
| `transport.go` | Request prep + retry loop. Header merge order: `config.defaultHeaders` → per-call `ExtraHeaders` (case-insensitive last-wins) → strip `X-TypeSafe-Retry-Count` → **force-set protected headers** (`Authorization: Bearer <key>`, `Accept: application/json`, `Content-Type: application/json` when body present — or the multipart content type when assets are sent (v0.7.5), via `prepareRequest`'s explicit contentType parameter, `User-Agent: typesafe-sdk/<version>`, `X-TypeSafe-SDK: typesafe-sdk/<version>`, `X-TypeSafe-Runtime: go/<goversion> (<GOOS>; <GOARCH>)`). `X-TypeSafe-Retry-Count: <n>` set on retries (n = attempt number). Per-attempt `context.WithTimeout` from resolved timeout: attempt-deadline exceeded → `*TimeoutError` (resolved timeout preserved); caller ctx canceled → propagate ctx error **unretried**. Retry dispatch: `*TimeoutError` → `APITimeoutError` flag; `*ConnectionError` → `APIConnectionError` flag; other `*APIError` → status ∈ `HTTPStatuses`; plus `RetryOn` membership (`errors.As`) or `Predicate`. `http.Client.Do` errors unwrapped (`*url.Error`) and classified (net timeouts → `*TimeoutError`; other transport errors → `*ConnectionError`; cause preserved). Logging is structured `log/slog` records (contracts §5): INFO `msg=response` (`method`, `url`, `status`, `elapsed_ms`, `request_id`), INFO `msg=retry` per re-attempt, INFO `msg=error` on transport failure, DEBUG `msg=wire` dumps with redacted headers |
| `logging.go` | `log/slog` logger (attribution `typesafe_sdk`), no-op handler by default (library best practice — silent unless configured), `TYPESAFE_LOG_LEVEL` (`debug | info | warn | warning | error | off`) applied at init; header redaction: names in`secretHeaders`(`authorization`,`proxy-authorization`,`x-api-key`,`api-key`,`cookie`,`set-cookie`) or containing`token`/`secret` (case-insensitive) → value `***` |
| `client.go` | `Client` + functional options `WithAPIKey`, `WithBaseURL`, `WithModel`, `WithTimeout`, `WithRetry`, `WithHeaders`, `WithHTTPClient(*http.Client)` (validated in `NewClient`; missing API key → `*TypeSafeError` wrapping `ErrMissingAPIKey`). `SystemOne(ctx, *SystemOneParams)` with per-call `Model/Timeout/Retry/ExtraHeaders/ExtraBody/Assets` — v0.7.5 flow: `State is required.` check → normalize questions → ExtraBody reserved-key check → body walk (`rewriteContent`: image substitution + non-finite stringification; cycle guard) → encode → reference scan of the encoded bytes (`collectPendingAssets`: every `Assets` entry in sorted-key order, then references in body order; attached snapshot consulted) → hand-encoded `assets` entries `{"type":"image","part":"asset-N"}` (type before part, immune to key sorting) → `resolvePendingAssets` (validation lives here and is **skipped entirely** when `ExtraBody` contains `assets` — Python parity) → multipart assembly (`request` part + one part per asset) when anything is pending. `AttachAssets` reaches the per-client registry. The SDK-owned default transport clone is tuned for keep-alive (`MaxIdleConnsPerHost=100`, `IdleConnTimeout=30s`; v0.7.3). `Models` resource: `client.Models.List(ctx, *ModelsListParams)` → `*ListModelsResponse`. `Close()` → `CloseIdleConnections()` on the underlying http client (owned **and** supplied; the owned client runs on a private `http.DefaultTransport` clone so its pool never shares with process-global users); closed-client reuse → `ErrClientClosed`. Safe for concurrent use; per-call state fully isolated (retry policy copied per call) |

The library package stays flat and single; `examples/*` (§8) are separate `package main`
programs in the same module — demonstrative, not part of the public API surface.

## 4. Tests (stdlib `testing` + `httptest` / mock `RoundTripper`, table-driven)

| File | Contract coverage (ported from the Python suite) |
| --- | --- |
| `client_test.go` | Exact round-trip wire body (`{state, model, questions}`; nil optionals omitted; nested explicit nulls kept); `ExtraBody` reserved-field rejection (`state`/`model`/`questions` error before I/O); raw-question passthrough (unknown fields sent untouched); error mapping table w/ exact `Error()` strings; transport-error classification (connect errors → Connection, timeouts → Timeout, cause preserved); header protection matrix (user can't override Authorization/Accept/UA/SDK/Runtime; Retry-Count always stripped from input); timeout precedence (call > client > default 10s); per-call override isolation; Close semantics (closes owned + supplied idle conns; reuse after close errors); concurrent calls w/ distinct overrides don't interfere (`-race`); v0.7.5: `State is required.`, multipart content-type switch, `assets` key omitted when none pending, ExtraBody-`assets` override skips reading |
| `questions_test.go` | Pre-network validation (empty questions, missing/invalid `type`, missing `criteria`, empty score **and** choice criteria, noul object-criteria and instructions-or-criteria checks → error before any request); wire-form omission/preservation rules; discriminator tags; `Box{}` → `{"type":"box"}` with optional instructions omitted |
| `assets_test.go` | v0.7.5: reference shape/uniqueness/identity-compare; constructors (lazy path read, cached resolution, base64 matrix); the data checks with exact messages; first failing alias named; multipart part list and dispositions (sorted alias order — deviation §6.14); octet-stream for unknown signatures; assets map wire form `{"type","part"}`; one part per key; payload reused on retry |
| `inline_images_test.go` | v0.7.5: placed images serialize as alias/reference in state, typed questions, raw dicts; alias + reference combine; formatted-text references; attachment scoping/nesting/per-client/goroutines/refcounted holds; single-shot detach; unknown lookalike references stay text; ExtraBody override; `Asset '<reference>': …` error naming |
| `multipart_test.go` | v0.7.5: byte-exact RFC 7578 framing, boundary shape (32 lowercase hex), unescaped dispositions, closing delimiter |
| `responses_test.go` | Malformed-response `FieldPath` matrix (`answers.n.noul`, `models[1].name`, `answers.s.legend.x`, `answers.where.box.top`); unknown answer type dropped + warned, still on `Raw`; legend/probabilities string→int key coercion; request ID extraction; usage optionality; `BoxAnswer` decode incl. extra fields ignored, nullable confidence, `Boxes()` grouping |
| `errors_test.go` | Message extraction matrix (all body shapes, 200-char truncation + `…`, FastAPI detail join w/ `body` loc dropped, empty body); retry-after parsing table (seconds, ms, HTTP-date, 0, negative, garbage); status→subclass mapping; `errors.Is/As` chains incl. `net.Error` |
| `retry_test.go` | Policy validation errors; backoff sequence 0.5→1→2→4→5→5… cap; jitter floor 0.375 at rand=1.0 (injected rand); overflow guard (1e-300/1e300 extremes); budget stop-before-delay math; default retry statuses {408,429,5xx} retried, {400,401,403,404,409,422,302} not; retry-count header sequence `[absent, 1, 2]`; `MaxRetries=n` → exactly n+1 attempts; Retry-After honored exactly (timing observed inside `testing/synctest` bubbles) |
| `config_test.go` | arg > env > default precedence (`t.Setenv`); whitespace-only env ignored; missing key error; invalid timeout rejected; v0.7.1: both key sources stripped, non-printable/non-ASCII/whitespace keys rejected with the static message, invalid explicit key does not fall back to env |
| `logging_test.go` | Secret-header redaction matrix (incl. mixed case, token/secret substring); level filtering; no output by default |
| `example_test.go` | Runnable `Example*` functions against `httptest` servers (Go's docs-as-tests, replacing Python's sybil); v0.7.5: `ExampleNewImageFromBase64`, `ExampleBox` |
| `integration_test.go` | Live API tests, skipped without `TYPESAFE_API_KEY`; v0.7.5: a live image round-trip (generated 512×512 PNG, `TypeSafe-Danger-Zone-Images: true` header sent explicitly) |

## 5. Scaffolding

- `README.md` — install, quickstart, config/env table, questions & responses reference, images, error handling, retry policy, per-call overrides, deviations note
- `LICENSE` — MIT
- `CHANGELOG.md` — 0.7.0 entry (parity with Python v0.7.0); 0.7.5 entry (the v0.7.5 sync)
- `examples/` — cookbook examples + `examples/README.md` index (§8)
- `.gitignore` — standard Go
- `.github/workflows/ci.yml` — gofmt check, `go vet`, `go test -race ./...` on latest stable + previous Go

## 6. Deliberate Go adaptations (documented deviations)

This port keeps Python v0.7.5 behavior, with deliberate Go adaptations (1–11 predate the
v0.7.5 sync and are unchanged in spirit; 12–23 were added by it):

1. **No `response_model` overload** — Go structs are the typed response;
   there is no runtime schema type to substitute.
2. **One timeout** — `time.Duration` per attempt via context, not
   httpx-style connect/read/write/pool splits.
3. **Typed questions validate in `SystemOne`** (pre-network) rather than at
   construction — Go constructors cannot raise validation errors; the same
   client-side guarantee holds either way. A typed [NoulCriteria] cannot
   express an explicit JSON `null` outcome (a nil field omits the key where
   Python emits `"true": null`) — use a [RawQuestion] for that wire form.
   Raw criteria with custom JSON/text marshalers are encoded once and left for
   API validation; their underlying Go zero values are not used to reject them.
4. **One client** — Go's context/goroutine model replaces the sync/async
   client split.
5. **Slightly laxer JSON decoding** — stdlib field matching is
   case-insensitive and integer literals decode into float fields (documented
   `encoding/json` behavior). Response bodies with `NaN`/`Infinity` literals
   or out-of-range numbers like `1e400` (which Python's parser accepts as
   infinity) are rejected, and usage counts beyond `int64` fail response
   validation.
6. **No `APIPromise`** — meaningless without Promise semantics;
   `resp.Raw *http.Response` covers raw access.
7. **Idiomatic Go zero values** — `resp.RequestID` is `""` when the header is
   absent (Python raises on access), and `*APIError.Body` keeps the raw wire
   text with the parsed value on `DecodedBody` (Python exposes only the
   parsed body as `body`). The SDK is fully silent by default; Python's
   stdlib logging happens to print WARNING+ records (such as the
   unknown-answer drop notice) to stderr out of the box via its last-resort
   handler — set `TYPESAFE_LOG_LEVEL` or use `SetLogger` to see them here.
8. **Cosmetic edge differences** — invalid UTF-8 in error bodies collapses to
   one replacement character per run, non-string FastAPI `loc` segments,
   `%q`-escaped identifiers in validation messages (Python interpolates
   names raw), and fallback re-serialization of unstructured error bodies
   use Go formatting, and HTTP-date coverage in `Retry-After` follows Go's
   `http.ParseTime` rather than Python's RFC 2822 parser. None of these are
   reachable with spec-conforming servers.
9. **Zero-value sentinels** — a zero `time.Duration` timeout means "unset"
   (inherit) where Python rejects `timeout=0`; an empty `Model` string means
   "use the default" where Python would send it verbatim. `WithHTTPClient`
   inherits the supplied client's `Timeout` — when positive — if
   `WithTimeout` is not set; the resolved timeout is then enforced per attempt
   through its context on an SDK-owned copy of the client (outer `Timeout`
   disabled, the caller's client never modified), so a longer SDK timeout is
   never capped by the injected client's own deadline.
10. **Wire bytes** — request/response JSON objects are emitted with sorted
   keys where Python preserves insertion order, and Go's encoder escapes
   U+2028/U+2029 that Python emits raw; parsed values are identical. Float
   number tokens may print differently (`1` vs Python's `1.0`, `1e+21` vs
   `1e21`) — identical parsed values, and Go matches JavaScript's
   `JSON.stringify` here. `Close` only closes idle connections (Go has no
   full-close API); the SDK-owned client pools them on a private transport,
   so closing it never evicts other code's connections, and reuse of a
   closed client returns a typed `ErrClientClosed` error rather than a
   transport panic.
11. **Reserved `ExtraBody` keys** — Python's `extra_body` shallow-merges
   last-write-wins, so a key colliding with `state`, `model`, or `questions`
   silently overrides the SDK-owned value. Go rejects the collision with a
   `*TypeSafeError` before network I/O (see README "Deviations from the
   Python SDK"), turning a silent wire-form corruption into a loud error.
   (v0.7.5 note: an `ExtraBody` key `assets` is *not* reserved — it is the
   documented user override of the asset machinery, Python parity.)
12. **zstd request compression is NOT implemented and gets no option**
   (v0.7.5) — Python compresses request bodies ≥ 1024 bytes with zstd by
   default when a compressor is available (`Content-Encoding: zstd`, body
   compressed once and reused for retries). Go's standard library has no zstd
   (`compress/` = flate, gzip, zlib, bzip2, lzw — verified via
   `ls $(go env GOROOT)/src/compress`); bodies ≥ 1024 bytes are sent
   uncompressed and `Content-Encoding` is never set. A `WithCompression`
   whose only non-default value could solely fail is API noise; ported
   callers get a docs pointer instead. **RISK FLAGGED**: if api.typesafe.ai
   ever *requires* `Content-Encoding: zstd` for large bodies, a stdlib-only
   Go SDK cannot comply without breaking the zero-dependency rule — the
   owner must stay aware.
13. **`attach_assets` context manager → `AttachAssets(assets ...*Image)
   (detach func(), error)`** (v0.7.5) — Go has no with-blocks; the
   CancelFunc-style detach (`defer detach()`) is the idiomatic scope
   release. The returned func is **single-shot** (`sync.Once`) because holds
   are per-REFERENCE refcounts (Python `attached.py:18-19`) — a double call
   would decrement another holder and silently detach an in-scope asset;
   Python's context manager makes double-exit effectively impossible.
   Refcounted overlapping holds, per-client scope, goroutine reach,
   snapshot-at-call, and enter-time validation are preserved; non-asset
   rejection is enforced by the `*Image` parameter type, with nil rejected
   using Python's message.
14. **Asset enumeration order** (v0.7.5) — pending assets enumerate sorted
   `Assets` keys then body-scan reference order, where Python uses mapping
   insertion order — Go maps are unordered and sorted keys are the accepted
   Go deviation. Part names (`asset-N`) are opaque to the server, so only
   numbering differs (`test_aliased_and_inline_images_combine` asserts Go's
   sorted order).
15. **`Image.from_pil` has no counterpart in this sync** (v0.7.5) — no
   Pillow in Go; stdlib `image/png` + `image/jpeg` could power a
   `NewImageFromImage` later, but the distilled contract marks `from_pil` a
   port note — users encode manually and call `NewImageFromBytes`.
16. **Explicit-empty API key still inherits `TYPESAFE_API_KEY`** (v0.7.1;
   extends item 9) — Python v0.7.1 errors on an explicit `""` without env
   fallback. A non-empty but invalid explicit key errors with no env
   fallback, matching Python.
17. **Keep-alive is a best-effort mapping** (v0.7.3) —
   `MaxIdleConnsPerHost=100` and `IdleConnTimeout=30s` on the SDK-owned
   transport clone. Go's nearest knob to httpx2 `Limits(max_connections=100)`
   is `Transport.MaxConnsPerHost`, which caps per-host connections by
   QUEUEING excess requests before dialing; it is deliberately left unset
   because silently queueing requests changes latency semantics versus
   httpx2's pool behavior, and the keep-alive goal (connection reuse) is met
   by the idle-conn settings alone (Python's cap is also per-client total,
   not per-host). `WithHTTPClient`-supplied transports are never mutated.
18. **Non-finite number and asset coverage boundary** (v0.7.4/v0.7.5),
   stated honestly — covered: `float64`/`float32` NaN/±Inf and `*Image`
   substitution anywhere inside maps/slices/arrays of any element type
   reachable through the rewrite walk, INCLUDING pointer indirection
   (non-nil `*T` is unwrapped and its pointee walked; JSON(`*T`) is
   byte-identical to JSON(`T`), verified for `*float64`, `*[2]any`,
   `*[]any`), with typed numeric containers (`[]float64`,
   `map[string]float64`, `[N]float64`) converted to heterogeneous containers
   only when a substitution occurs, producing Python's `[1,"Infinity"]`
   output. NOT covered: values inside arbitrary user struct FIELDS (the
   walk never opens structs — a non-finite float field still fails with
   encoding/json's `json: unsupported value: +Inf`, and an `*Image` held in
   a struct field serializes via `MarshalJSON` as an uncollected reference —
   dangling on the wire, no error) and anything behind custom
   `json.Marshaler`/`TextMarshaler` (never invoked, per the
   questions.go:255-259 rule). Python's Content type has no struct or
   pointer analog; Go's `JSONContent` does, so the docs state this exact
   boundary.
19. **Typed-nil and bare-value asset handling** (Go-specific; Python cannot
   express them) — a nil `*Image` inside content serializes as JSON `null`
   (content containers may hold null elements) instead of panicking; a bare
   non-pointer `Image` value in content is rejected with `Images must be
   passed as *Image.` (surfaced verbatim, never wrapped in the generic
   encode message) rather than silently encoding `{}`.
20. **Multipart-only asset transport** (v0.7.5) — the SDK never emits
   base64 `data_base64` assets; they stay reachable solely through
   `ExtraBody["assets"]`, exactly like Python. This resolves the sync's
   "multipart or base64" decision in Python's favor per AGENTS.md.
21. **DEBUG wire-body logging keeps the 16 KiB `<truncated>` cap** rather
   than Python v0.7.5's 4096 + `(+N bytes)` (transport.py:73-85) —
   pre-existing Go behavior; large multipart/image bodies truncate, keeping
   logs bounded; noted rather than churned.
22. **Read-error errno text** — Go's `*fs.PathError` carries lowercase
   errno strings (`no such file or directory`) where Python's strerror
   capitalizes (`No such file or directory`); the message format is
   otherwise identical and tests prefix-match exactly where Python's own
   test does (`test_inline_images.py:119` stops at "could not be read").
23. **No client-side image/reference/byte limits** (parity — negative
   finding: none exist in Python either); the docs' 16 MiB/pixel/32 MiB
   limits are server-side. `TypeSafe-Danger-Zone-Images: true` is documented
   and sent in examples/tests but never auto-set (parity with both reference
   SDKs).

## 7. Implementation order

1. Verify toolchain (`go version`; set `go` directive accordingly, target ≥1.27); scaffold `go.mod` + repo files
2. Foundations + tests: `constants.go`, `version.go`, `json.go`, `config.go`, `logging.go`
3. Pure logic + tests: `errors.go`, `retry.go` (deterministic via injected sleep/rand hooks)
4. `questions.go`, `responses.go` (+ tests)
5. `transport.go`, `client.go` (+ tests incl. race)
6. `example_test.go`, `integration_test.go`, README/LICENSE/CHANGELOG/CI
7. Examples cookbook per §8 (`examples/*` + `examples/README.md`); smoke-run each against the live API when a key is available
8. Full `gofmt -l`, `go vet`, `go test -race ./...` green; final review against `contracts.md`

2026-11-01 v0.7.5 sync (shipped): new `assets.go`/`attached.go`/`content.go`/`multipart.go`
+ tests; `Box` question and `BoxAnswer`; non-finite stringification; request-validation
and API-key deltas; keep-alive transport tuning; `Version = "0.7.5"`. zstd deliberately
absent (§6.12). A live-API image round-trip is pinned by `integration_test.go`.

`references/` stays untouched (untracked in git); all work happens in the repo root alongside it.

## 8. Examples / cookbook (`examples/*`)

Goal: copy-paste starting points so a user can go from `go get` to a working use case in
minutes. The set is curated from the official docs — each example mirrors a documented
page (quickstart, patterns, cookbooks) and maps to a category of the
[use-case map](https://docs.typesafe.ai/concepts/use-case-map), and exercises the SDK the
way real code would: typed questions, confidence-gated branching in plain Go, error
handling. Together they cover all three primitives, all four documented patterns
(fan-out, confidence routing, composite scoring, intent routing), and both state shapes.

Layout: one directory per example, `package main`, a single `main.go`, **stdlib only**
(matching the SDK's zero-dep philosophy). Run via `go run ./examples/<name>`; every
example hits the live API and requires `TYPESAFE_API_KEY` (unset → the SDK's
`ErrMissingAPIKey` error, plus a friendly hint line printed by the example). CI
compile-checks them for free via `go vet ./...` / `go test ./...`; no tests of their own —
demonstrative code, with docs-as-tests already covered by `example_test.go` (§4).

| # | Example | Mirrors (docs page) | Use-case-map category | Demonstrates |
| --- | --- | --- | --- | --- |
| 1 | `examples/quickstart` | Quickstart | Customer support | Minimal client from env; one call with all three primitives — department `Choice` (billing/technical/sales), frustration `Score` (3-level rubric), is_urgent `Noul`; print typed answers, probabilities, confidence, usage |
| 2 | `examples/automation-use-cases/customer-support` | Pattern: speculative fan-out | Customer support / AI automation | Five questions in **one** call (incl. one `RawQuestion` map to show wire passthrough); Go decision tree afterwards — escalate bugs on severity + repro steps, flag refund requests, priority-response on high frustration |
| 3 | `examples/task-categories/routing` | Patterns: intent routing + confidence-gated routing | AI automation | Voice-banking intent `Choice` (check_balance / approve_transfer / other); confidence gates in Go — < 0.6 → human agent, ≥ 0.85 auto-acts, in between asks the user to confirm |
| 4 | `examples/automation-use-cases/lead-generation` | Pattern: composite scoring | Lead generation | Structured map state (company profile + inbound message); multiple `Score` answers (ICP fit, maturity, buyer intent) merged into a weighted composite and priority buckets in Go |
| 5 | `examples/automation-use-cases/llm-guardrails` | Cookbook: LLM guardrails | Universal verification / Moderation | `Noul` hazard battery (jailbreak, harmful request, medical advice, self-harm) + harm-severity `Score`; threshold policies (strict/permissive); pass/review/block/support routing with precedence — same machinery for input and output batteries |
| 6 | `examples/task-categories/ranking` | Cookbook: re-ranking | Search & retrieval / Map-reduce over data | Score query↔candidate relevance per passage, sort by expected value in Go; map-reduce loop over a small corpus slice |
| 7 | `examples/retries-errors` | SDK usage docs | Operational | `errors.As` across the taxonomy (incl. `RateLimitError.RetryAfterMs`); custom `RetryPolicy` (statuses, budget); per-call `Model`/`Timeout`/`ExtraHeaders`; `TYPESAFE_LOG_LEVEL=debug` wire logging |

2026-09-19 expansion: the cookbook grew to mirror the
[use-case map](https://docs.typesafe.ai/concepts/use-case-map) one-for-one —
`examples/automation-use-cases/` holds one directory per docs "example
automation use case" (search & retrieval, scientific discovery, model
routing, semantic code linting, predictive features, recruiting, insurance
claims, financial crime, legal compliance, marketplaces, moderation,
advertising, gaming, risk assessment, demand forecasting, knowledge graphs,
plus the re-homed llm-guardrails / customer-support / lead-generation), and
`examples/task-categories/` one per "example task category" (classification,
detection, scoring, search, retrieval, verification, ML feature extraction,
structured data extraction, plus the re-homed routing / ranking).
`examples/README.md` is the authoritative catalog; the table above records
the original seven as first built.

Conventions for every example:

- Header comment: what it demonstrates, the docs page it mirrors, its use-case-map category.
- Deterministic, readable stdout (`label: value` lines) so users can compare against the
  numbers published in the docs pages.
- Plain-string states (quickstart, guardrails, rerank) and structured map states
  (ticket-triage, lead-scoring) both shown across the set.
- No network mocking in examples — they run against the real API (offline behavior is the
  test suite's job).
