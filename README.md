# TypeSafe AI SDK for Go

> **Disclaimer:** This project is an independent, community-maintained Go SDK. It is not officially affiliated with, maintained, or endorsed by TypeSafe AI.

[![CI](https://github.com/captain-corgi/typesafe-sdk-go/actions/workflows/ci.yml/badge.svg)](https://github.com/captain-corgi/typesafe-sdk-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/captain-corgi/typesafe-sdk-go.svg)](https://pkg.go.dev/github.com/captain-corgi/typesafe-sdk-go)

A Go client SDK for the [TypeSafe AI API](https://typesafe.ai) — typed answers
to named questions about any state, including images, in a single request.
**Zero runtime dependencies** (Go standard library only), with feature parity
to the Python SDK v0.7.5.

```go
client, err := typesafe.NewClient() // reads TYPESAFE_API_KEY
if err != nil { return err }
defer client.Close()

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
if err != nil { return err }

fmt.Println(resp.Choices()["category"].Choice) // "billing"
fmt.Println(resp.Nouls()["urgent"].Noul)       // 0.92
fmt.Println(resp.Scores()["tone"].Score)       // 1.4
```

The question kind you ask determines the statically-known answer type you get
back — that is the "type safety":

| Primitive | Question | Answer |
| --- | --- | --- |
| **Noul** | yes/no | `NoulAnswer{Noul float64}` — probability of true, 0..1 |
| **Choice** | classification | `ChoiceAnswer{Choice, Confidence, Probabilities}` |
| **Score** | rubric scoring | `ScoreAnswer{Score, Confidence, Legend, Probabilities}` |
| **Box** | locate in an image | `BoxAnswer{Box BoxCoordinates, Confidence}` — edges as fractions |

## Installation

```sh
go get github.com/captain-corgi/typesafe-sdk-go
```

Requires Go 1.27 or newer. The import path ends in `typesafe-sdk-go` but the
package is named `typesafe` (like `go-openai`):

```go
import "github.com/captain-corgi/typesafe-sdk-go" // package typesafe
```

## Configuration

Explicit options beat environment variables, which beat defaults. Environment
values are trimmed and ignored when blank. Empty explicit strings inherit;
whitespace-only explicit API keys also inherit. Other explicit strings, including
base URLs, model names, and nonblank API keys, are preserved verbatim. The
resolved API key is validated at construction — printable ASCII without
whitespace — and a non-empty but invalid explicit key never falls back to the
environment.

| Setting | Option | Environment | Default |
| --- | --- | --- | --- |
| API key | `WithAPIKey` | `TYPESAFE_API_KEY` | — (required) |
| Base URL | `WithBaseURL` | `TYPESAFE_BASE_URL` | `https://api.typesafe.ai` |
| Model | `WithModel` | `TYPESAFE_DEFAULT_MODEL` | `jev-latest` |
| Timeout (per attempt) | `WithTimeout` | — | `10s` |
| Maximum response body | `WithMaxResponseBodySize` | — | `16 MiB` |
| Allow loopback HTTP | `WithAllowInsecureHTTP` | — | disabled |
| Retry policy | `WithRetry` | — | `DefaultRetryPolicy()` |
| Default headers | `WithHeaders` | — | — |
| Underlying HTTP client | `WithHTTPClient` | — | new `http.Client` |

```go
client, err := typesafe.NewClient(
    typesafe.WithAPIKey("ts_live_..."),
    typesafe.WithBaseURL("https://api.typesafe.ai"),
    typesafe.WithModel("jev-latest"),
    typesafe.WithTimeout(10*time.Second),
)
```

## Questions

Questions are values — build them inline. Optional fields left at their zero
value are omitted from the wire; nested values (including explicit `nil`s
inside maps and slices) are preserved.

```go
// Yes/no, with outcome descriptions.
typesafe.Noul{
    Instructions: "Is this message spam?",
    Criteria: &typesafe.NoulCriteria{
        True:  "Unsolicited advertising",
        False: "A legitimate conversation",
    },
}

// Classification; criteria labels map to descriptions (nil = label alone).
typesafe.Choice{
    Instructions: "What is the tone?",
    Criteria: typesafe.ChoiceCriteria{
        "angry":   "An upset or hostile message",
        "calm":    "A neutral or polite message",
        "excited": nil,
    },
}

// Rubric scoring; each entry is one level, scored from zero.
typesafe.Score{
    Instructions: "How urgent is this message?",
    Criteria:     typesafe.ScoreCriteria{"can wait", "this week", "today"},
}

// Locate something in the request's image (see Images below).
typesafe.Box{Instructions: "The dog"}
```

Raw dictionaries pass through to the API untouched (unknown fields included)
— the API is the schema validator for anything this SDK does not model:

```go
"custom": typesafe.RawQuestion{
    "type": "noul", "instructions": "Spam?", "weight": 3,
},
```

Questions are validated **before any network I/O**: an empty question map, a
raw question without a non-empty string `type`, a choice/score question
without `criteria`, empty score **or** choice criteria, a noul question whose
criteria is not an object or that has neither instructions nor a described
outcome, or a missing state all fail fast with a `*typesafe.TypeSafeError`.

Raw score criteria may use custom JSON or text marshalers. Their output is left
for the API to validate; the SDK encodes them once per call and reuses the body
on retries. Ordinary empty criteria and cyclic pointer/interface chains are
rejected before network I/O.

## Images

State and question content may include images. An `*typesafe.Image` (aliased
as `Asset`) is created from bytes, a file path, or base64 text:

```go
photo, err := typesafe.NewImageFromBase64(b64) // decodes now
if err != nil { return err }
chart  := typesafe.NewImageFromFile("chart.png") // read at request preparation
avatar := typesafe.NewImageFromBytes(pngBytes)   // copied once, at first use
```

Data is read and validated lazily, once, on first use — the cached resolution
is reused by every later request, and image payloads are identical across
retries. There are no client-side size or count limits; only format sniffing
happens here (PNG/JPEG magic, else `application/octet-stream`) and the API
validates image contents (its documented 16 MiB/pixel/32 MiB limits are
server-side).

Two ways to use an image:

1. **Place it in content** — anywhere in state or question
   instructions/criteria, at any nesting: `map[string]any{"photo": photo,
   "caption": "before"}`. A placed image serializes as its alias when it is
   a member of the call's `Assets` map, else as its reference.
2. **Reference it from text** — every image mints a stable reference,
   `<image:` + 32 hex characters + `>`, returned by `photo.Reference()` and
   `photo.String()` (so `fmt.Sprintf` interpolates it). Reference text is
   sent verbatim; references are found by scanning the encoded body, inside
   any string anywhere. The reference must name an image the request knows
   — one placed in content, passed in `Assets`, or attached to the client
   (below); a lookalike reference naming nothing known is sent as plain
   text with no asset.

```go
resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
    State: map[string]any{
        "photo": photo, // placed — serialized as the alias "product"
        "note":  "The product photo on file",
    },
    Assets: map[string]*typesafe.Image{"product": photo}, // alias → image; nil/empty sends none
    Questions: typesafe.Questions{
        "dog":   typesafe.Box{Instructions: "The dog"},
        "breed": typesafe.Choice{Instructions: "Which breed is the dog?",
            Criteria: typesafe.ChoiceCriteria{"labrador": nil, "poodle": nil}},
    },
})
```

Every `Assets` entry is uploaded (under its alias) even when the body never
names it; each is one multipart part. A request carrying any assets is sent
as `multipart/form-data` — a first JSON part named `request`, then one part
per asset, with an `assets` map (`{"<alias>": {"type": "image", "part":
"asset-N"}}`) added to the JSON body. The SDK never emits base64 assets; that
wire form stays reachable only through `ExtraBody["assets"]`, which replaces
the asset machinery entirely (assets are then neither read nor validated).

For a scope wider than one call, attach images to the client — the idiomatic
home for text references:

```go
detach, err := client.AttachAssets(chart)
if err != nil { return err }
defer detach() // single-shot: releases this scope's hold
// now any call whose text mentions chart.Reference() uploads the chart,
// without placing it in each request
```

Until `detach` runs, every call on this client sends each attached image whose
reference appears in the request. Attachments nest, are refcounted (an image
attached by overlapping scopes stays attached until the last one detaches),
are per-client, and are safe for concurrent use. The returned function is
single-shot — a second call is a no-op, because holds are counted per
reference and an unguarded double call would silently detach an asset still
held by another scope.

Images must be passed as `*Image` (a bare `Image` value in content is
rejected); images compare by identity, so two images built from identical
bytes are distinct assets. Images are experimental on the API side: every
image request must opt in with a `TypeSafe-Danger-Zone-Images: true` header —
the SDK never auto-sets it; pass it via `WithHeaders` or per-call
`ExtraHeaders`. Runnable multimodal programs live under
[`examples/multimodal/`](examples/multimodal/).

Non-finite numbers (`NaN`, `±Inf`) anywhere in the body — including
`ExtraBody` — are serialized as the strings `"NaN"`, `"Infinity"`, and
`"-Infinity"`, matching the Python SDK. The boundary: this covers values the
SDK's body walk reaches (string-keyed maps, slices, arrays, pointers); it
does not cover fields of your own structs (still rejected by `encoding/json`),
values behind custom `json.Marshaler`/`TextMarshaler` implementations (never
invoked, per the marshaler-passthrough rule), or maps whose key type is not
exactly `string` (`map[int]float64`, defined string-key types) — those pass
through untouched, so a non-finite value inside one fails encoding instead of
being stringified.

## Responses

`SystemOne` groups answers by kind, so the Go types line up with the questions
you asked:

```go
resp.Model                       // "jev-latest"
resp.Usage.InputTokens           // *int (nil when unreported)
resp.Answers["category"]         // typesafe.Answer interface
resp.Nouls()["urgent"].Noul      // float64
resp.Choices()["category"].Choice          // string
resp.Choices()["category"].Confidence      // float64
resp.Choices()["category"].Probabilities   // map[string]float64
resp.Scores()["tone"].Score                // float64 (expected value; may be fractional)
resp.Scores()["tone"].Legend               // map[int]any  — level → rubric entry
resp.Scores()["tone"].Probabilities        // map[int]float64
resp.Boxes()["dog"].Box.Top                // float64 — fraction of image height
resp.Boxes()["dog"].Box.Left               // float64 — fraction of image width
resp.Boxes()["dog"].Confidence             // *float64 (nil when unreported)
resp.RequestID                   // from the x-typesafe-request-id header
resp.Raw                         // *http.Response with a buffered, readable body
```

The per-kind views are plain maps, so reading a name with no answer of that
kind — the server never answered it, or (only with a misbehaving server)
answered a different kind, since the API guarantees answer kinds match
question kinds — yields the Go zero value, where Python's
`result.nouls["urgent"]` raises `KeyError`. Detect absence with the comma-ok
form:

```go
if a, ok := resp.Nouls()["urgent"]; ok {
    fmt.Println(a.Noul)
}
```

Separate single-answer lookup helpers (`resp.Noul("urgent")`) are
deliberately not provided; the map views plus comma-ok are the idiomatic Go
analog of the Python accessors.

Answer kinds this SDK version does not model are dropped from `Answers` (with
a warning) but remain readable through `resp.Raw`.

`client.Models.List(ctx, nil)` returns `*typesafe.ListModelsResponse` with
each model's `Name`, `Description`, and `ReleaseDate`.

A 2xx body that violates the schema fails with a
`*typesafe.ResponseValidationError` whose `FieldPath` names the first bad
field, e.g. `answers.tone.confidence` or `models[1].name`.

Responses captured outside the client (your own transport, a replayed
recording) can be decoded with the same taxonomy:
`typesafe.ParseSystemOneResponse(resp)` and
`typesafe.ParseListModelsResponse(resp)` buffer the body, map non-2xx
statuses to the typed error classes, and parse 2xx bodies — the counterpart
of the Python SDK's `Response.from_http_response`.

## Error handling

Every failure is matched with `errors.As`:

| Error type | Meaning |
| --- | --- |
| `*typesafe.TypeSafeError` | shared root of **every** SDK error below; used directly for SDK-side failures (missing key, bad params), wrapping sentinels like `ErrMissingAPIKey`, `ErrClientClosed` |
| `*typesafe.APIError` | any non-2xx response (`Status`, `Body` raw wire text, `DecodedBody` parsed JSON, `Headers`, `Endpoint`, `RequestID`) |
| `*typesafe.BadRequestError` … `*typesafe.RateLimitError` | 400 / 401 / 403 / 404 / 422 / 429 subclasses |
| `*typesafe.InternalServerError` | any status ≥ 500 |
| `*typesafe.ResponseValidationError` | 2xx body that failed schema validation (`FieldPath`) |
| `*typesafe.ConnectionError` | no HTTP response; transport cause preserved via `Unwrap` |
| `*typesafe.TimeoutError` | attempt exceeded its timeout; satisfies `net.Error` |

Subclasses match both their specific type and `*typesafe.APIError`; a
`*typesafe.TimeoutError` also matches `*typesafe.ConnectionError` (like the
Python SDK's class hierarchy):

```go
var rl *typesafe.RateLimitError
if errors.As(err, &rl) && rl.RetryAfterMs != nil {
    fmt.Println("retry after ms:", *rl.RetryAfterMs)
}
```

Every SDK error — API subclasses, response validation, connection, and
timeout failures alike — also matches the shared `*typesafe.TypeSafeError`
root, so one catch-all handler sees them all (the counterpart of catching
`TypeSafeError` in the Python SDK):

```go
var root *typesafe.TypeSafeError
if errors.As(err, &root) {
    log.Printf("typesafe call failed: %v", root)
}
```

Caller cancellation and deadlines return `context.Canceled` or
`context.DeadlineExceeded` directly; these are not SDK errors and do not match
`*TypeSafeError`. The reverse direction is not symmetric: because an SDK
attempt timeout preserves its transport cause chain, it **also** satisfies
`errors.Is(err, context.DeadlineExceeded)`. When you need to tell the two
apart, match SDK types with `errors.As` first (e.g. `*TimeoutError`) and only
treat `context.DeadlineExceeded` as your own deadline once no SDK type
matched.

Error strings follow `<METHOD> <url>: <status> <message> (request_id=…)`,
with parts omitted when absent — e.g.

```
POST https://api.typesafe.ai/v1/systemone: 429 Too many requests (request_id=req_123)
```

## Retries

Every request runs under a `RetryPolicy`. `typesafe.DefaultRetryPolicy()`
retries twice on statuses {408, 429, 500–599}, connection errors, and
timeouts, with 0.5s→5s exponential backoff (25% jitter), honoring
`Retry-After` / `retry-after-ms`, all inside a 30s per-call budget:

```go
policy := typesafe.DefaultRetryPolicy()
policy.MaxRetries = 4
policy.HTTPStatuses = map[int]struct{}{429: {}, 502: {}, 503: {}}
client, _ := typesafe.NewClient(typesafe.WithAPIKey(key), typesafe.WithRetry(policy))
```

- `MaxRetries: 0` disables retries; `BackoffInitial: 0` disables backoff.
- `Timeout` is the total budget per SDK call: the loop stops **before** a
  retry whose delay would reach it, surfacing the last error. `0` disables.
- `RetryOn` adds selectors: an SDK error instance (`&NotFoundError{}`) or a
  typed nil (`(*MyError)(nil)`) selects by **type** via `errors.As` — wrapped,
  `errors.Join`-ed, and custom-`As` errors all match; any other entry is a
  **sentinel** matched strictly by identity via `errors.Is`, so two distinct
  errors of the same type never match each other. `Predicate` covers anything
  else.
- The policy is snapshotted per call; per-call `Retry` replaces the client
  policy for that call only.

## Per-call overrides

```go
resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
    State: ..., Questions: ...,
    Model:        "jev-latest",                    // per-call model
    Timeout:      5 * time.Second,                 // per-call timeout
    Retry:        &policy,                         // per-call retry policy
    ExtraHeaders: map[string]string{"X-Custom": "v"},
    ExtraBody:    map[string]any{"custom_field": 1}, // state/model/questions cannot be overridden
})
```

Protected headers (`Authorization`, `Accept`, `Content-Type`, `User-Agent`,
`X-TypeSafe-*`) are always set by the SDK and cannot be overridden. Retries
carry `X-TypeSafe-Retry-Count: <n>` (never on the first attempt).

`ExtraBody` is a shallow merge for additional top-level fields. The SDK-owned
`state`, `model`, and `questions` fields are reserved and attempting to
override any of them returns a `*typesafe.TypeSafeError` before network I/O.
An `ExtraBody` key `assets` is *not* reserved: it replaces the SDK's asset
machinery entirely — images (passed, placed, or attached) are neither read nor
validated, and the body stays plain JSON with your value verbatim.

## Logging

The SDK logs through `log/slog`, tagged `typesafe_sdk` (the same
attribution as the Python SDK's logger), and is silent by
default. Set `TYPESAFE_LOG_LEVEL=debug` for wire dumps (secret headers
redacted), or take full control of destination, format, and level with
`typesafe.SetLogger(yourLogger)`. Request and response bodies are redacted by
default. Set `TYPESAFE_LOG_BODY=redacted` to preserve JSON structure while
masking string values, or `TYPESAFE_LOG_BODY=full` (or call
`typesafe.SetLogBodyMode(typesafe.LogBodyFull)`) only in controlled
environments; full body logging can expose sensitive payloads. Logged bodies
are capped at 16 KiB.

## Security

- To report a vulnerability, see [SECURITY.md](SECURITY.md) — please do not
  open a public issue.
- Responses are bounded to 16 MiB by default. Use
  `WithMaxResponseBodySize` to select a positive per-client limit; oversized
  bodies return `ErrResponseTooLarge` / `*ResponseTooLargeError` and are not
  retried.
- Base URLs must be absolute `https` URLs without userinfo, query, or
  fragments. `WithAllowInsecureHTTP` permits `http` only for `localhost` and
  loopback IP addresses, such as `127.0.0.1` and `::1`, and is intended for
  local development and tests.
- `Authorization` and other secret headers remain redacted in wire logs.
- `ExtraBody` cannot override the reserved `state`, `model`, or `questions`
  fields.

## Examples

Runnable cookbook programs live in [`examples/`](examples/README.md).
The `automation-use-cases/` and `task-categories/` catalogs mirror the docs
[use-case map](https://docs.typesafe.ai/concepts/use-case-map). Additional
`go-software-use-cases/`, `api-use-cases/`, `relational-db-use-cases/`, and
`testing-use-cases/` catalogs cover Go API and software use cases, and
`multimodal/` covers the experimental images feature (state images,
`Box` locating and cropping, the three constructors, attached assets, images
as choice criteria). Two basics — `quickstart` and `retries-errors` — sit at
the root. Each example runs against the live API with `TYPESAFE_API_KEY`
set:

```sh
TYPESAFE_API_KEY=... go run ./examples/quickstart
```

## Deviations from the Python SDK

This port keeps Python v0.7.5 behavior, with deliberate Go adaptations
(1–11 predate the v0.7.5 sync; 12–23 were added by it):

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
11. **Security hardening** — unlike Python, wire bodies are redacted by
   default, response bodies have a 16 MiB limit, base URLs require HTTPS
   (with loopback-only opt-in for HTTP), and `ExtraBody` cannot override
   SDK-owned fields (`assets` is not reserved — it is the documented user
   override of the asset machinery, Python parity).
12. **zstd request compression is NOT supported and gets no option** —
   Python v0.7.5 compresses request bodies ≥ 1024 bytes with zstd by default
   when a compressor is available; Go's standard library has no zstd
   (`compress/` provides flate, gzip, zlib, bzip2, lzw only), so bodies are
   always sent uncompressed and `Content-Encoding` is never set. A
   `WithCompression` option whose only non-default value could solely fail
   would be API noise; ported callers get this pointer instead. **Risk
   flagged**: if the API ever *requires* zstd for large bodies, a
   stdlib-only Go SDK cannot comply without breaking the zero-dependency
   rule.
13. **`attach_assets` context manager → `AttachAssets(assets ...*Image)
   (detach func(), error)`** — Go has no with-blocks; `defer detach()` is
   the idiomatic scope release. The returned function is **single-shot**
   (`sync.Once`) because holds are per-reference refcounts — a double call
   would decrement another holder's count and silently detach an in-scope
   asset; Python's context manager makes double-exit effectively impossible.
   Refcounted overlapping holds, per-client scope, goroutine reach,
   snapshot-at-call, and enter-time validation are preserved; non-asset
   rejection is enforced by the `*Image` parameter type, with `nil` rejected
   using Python's message.
14. **Asset enumeration order** — pending assets enumerate sorted `Assets`
   keys, then references in order of first appearance in the encoded body,
   where Python uses mapping insertion order (Go maps are unordered; sorted
   keys are the accepted Go deviation). Part names (`asset-N`) are opaque to
   the server, so only numbering differs.
15. **`Image.from_pil` has no counterpart** — no Pillow in Go. Stdlib
   `image/png`/`image/jpeg` could power a `NewImageFromImage` later; for
   now, encode the image yourself and call `NewImageFromBytes`.
16. **Explicit-empty API key still inherits `TYPESAFE_API_KEY`** (extends
   deviation 9) — Python v0.7.1 errors on an explicit `""` without env
   fallback. A non-empty but invalid explicit key errors with no env
   fallback, matching Python.
17. **Keep-alive is a best-effort mapping** — the SDK-owned default
   transport gets `MaxIdleConnsPerHost=100` and `IdleConnTimeout=30s`
   (Python keeps 100 pooled connections alive 30 s). Go's nearest knob to a
   max-connections cap, `Transport.MaxConnsPerHost`, is deliberately left
   unset: it caps by queueing excess requests before dialing, silently
   changing latency semantics versus Python's pool; connection reuse is met
   by the idle-conn settings alone. Transports supplied via
   `WithHTTPClient` are never mutated.
18. **Non-finite numbers and image substitution cover the walk, not struct
   fields, custom marshalers, or non-string map keys** — covered:
   `float64`/`float32` NaN/±Inf stringification and `*Image` substitution
   anywhere inside string-keyed maps, slices, and arrays reachable through
   the body walk, including through pointers (JSON of `*T` is byte-identical
   to `T`). Not covered: fields of your own structs (a non-finite field still
   fails with `encoding/json`'s unsupported-value error; an `*Image` in a
   struct field serializes as an uncollected reference), anything behind
   custom `json.Marshaler`/`TextMarshaler` implementations (never invoked),
   and maps whose key type is not exactly `string` (`map[int]float64`,
   defined string-key types), which pass through untouched so their contents
   are neither stringified nor substituted (rebuilding them would drop
   `TextMarshaler` key formatting). Python's content type has no struct,
   pointer, or typed-key analog; Go's `JSONContent` does, so this is the
   exact documented boundary.
19. **Typed-nil and bare-value image handling** (Go-specific) — a nil
   `*Image` inside content serializes as JSON `null`; a bare non-pointer
   `Image` value is rejected with `Images must be passed as *Image.`
   (surfaced verbatim) rather than silently encoding `{}`.
20. **Multipart-only asset transport** — the SDK never emits base64
   `data_base64` assets; they stay reachable solely through
   `ExtraBody["assets"]`, exactly like Python.
21. **DEBUG wire-body logging keeps the 16 KiB `<truncated>` cap** rather
   than Python v0.7.5's 4096 bytes + `(+N bytes)` — pre-existing Go
   behavior; large multipart/image bodies truncate, keeping logs bounded.
22. **Read-error errno text** — image-file read errors carry Go's
   `*fs.PathError` wording (lowercase `no such file or directory`) where
   Python's strerror capitalizes; the message format is otherwise identical.
23. **No client-side image/reference/byte limits** (parity — none exist in
   Python either); the API's 16 MiB/pixel/32 MiB limits are server-side.
   `TypeSafe-Danger-Zone-Images: true` is documented and sent in examples
   but never auto-set (parity with both reference SDKs).

## License

[MIT](LICENSE)
