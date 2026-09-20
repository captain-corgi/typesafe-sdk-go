# Wire protocol

What the SDK actually sends and reads on HTTP. Owners: `transport.go`
(request prep, header merge), `json.go` (encoding), `constants.go` (paths,
header names, defaults); contract in
[contracts.md §1](../plans/2026-09-19-typesafe-sdk-go/contracts.md), locked by
the exact round-trip and header-protection matrices in `client_test.go` and
`contracts_test.go`.

## Endpoints

| | |
|---|---|
| `POST /v1/systemone` | body `{"state", "model", "questions": {name: question}}` plus any `ExtraBody` fields |
| `GET /v1/models` | no body |

## A call on the wire

```mermaid
sequenceDiagram
    participant App as Your code
    participant SDK as typesafe Client
    participant API as api.typesafe.ai

    App->>SDK: SystemOne(ctx, params)
    Note over SDK: validate questions (pre-network),<br>encode body once (reused on retries)
    SDK->>API: POST /v1/systemone
    Note over SDK,API: Authorization: Bearer key<br>Accept: application/json<br>Content-Type: application/json (body present)<br>User-Agent: typesafe-sdk/0.7.0<br>X-TypeSafe-SDK / X-TypeSafe-Runtime
    API-->>SDK: 200 OK
    Note over API,SDK: x-typesafe-request-id: req_123
    SDK-->>App: SystemOneResponse (RequestID set, Raw buffered)
```

## Header merge order

```mermaid
flowchart TB
    h0["Clone client default headers<br>(WithHeaders)"] --> h1["Apply per-call ExtraHeaders<br>case-insensitive, last-wins,<br>sorted by name for determinism"]
    h1 --> h2["Strip X-TypeSafe-Retry-Count<br>(never accepted from user input)"]
    h2 --> h3["Force-set protected headers last"]
    h3 --> h4["Authorization: Bearer key<br>Accept: application/json<br>Content-Type: application/json — only when a body exists<br>User-Agent: typesafe-sdk/Version<br>X-TypeSafe-SDK: typesafe-sdk/Version<br>X-TypeSafe-Runtime: go/Version (GOOS; GOARCH)"]
```

Consequences: user headers can never override the protected set (checked
case-insensitively by the header-protection test matrix), and a first attempt
never carries `X-TypeSafe-Retry-Count` — retries set it to the retry number
(attempt 2 → `1`).

Headers the SDK **consumes** from responses: `x-typesafe-request-id` →
`resp.RequestID`; `retry-after` and `retry-after-ms` → retry delay
([Retries](retries.md)).

## Request body composition

```mermaid
flowchart TB
    b0["body = {}"] --> b1["state = params.State"]
    b1 --> b2["model = client default,<br>or params.Model when non-empty"]
    b2 --> b3["questions = normalized map"]
    b3 --> b4{"ExtraBody keys validated<br>against reserved state/model/questions"}
    b4 -->|allowed| b5["Additional keys applied in sorted order,<br>shallow merge"]
    b4 -->|reserved| bx["*TypeSafeError before network I/O"]
    b5 --> b6["Marshal once; the encoded bytes are<br>reused verbatim on every retry"]
```

`ExtraBody` adds top-level fields in sorted order. `state`, `model`, and
`questions` are reserved; attempting to override one fails before encoding or
network I/O. Other object values are replaced, not deep-merged.

## Encoding rules

The custom marshalers in `questions.go` and `json.go` enforce the wire rules
from [Questions](questions.md#wire-form-rules):

- `"type"` first in every question object; nil optionals omitted; explicit
  `null`s nested inside values preserved; raw questions pass through
  untouched.
- Objects are emitted with **sorted keys** (Python preserves insertion
  order), and Go's encoder escapes U+2028/U+2029 — parsed values are
  identical; see [README deviation 10](../README.md#deviations-from-the-python-sdk).
- Unencodable bodies fail before any network I/O with a
  `*TypeSafeError`.

## Response headers and redirects

The SDK's default HTTP client does not follow redirects: a 3xx response
surfaces as a plain `*APIError`, matching the other TypeSafe SDKs. Supply
your own client via `WithHTTPClient` to change that
([Configuration](configuration.md#timeout-resolution)).

Defaults (base URL, model, timeout, environment variables) are owned by
[Configuration](configuration.md) and `constants.go`.

Responses are buffered with a 16 MiB default limit (configurable through
`WithMaxResponseBodySize`). Oversized responses return
`*ResponseTooLargeError` and are not retried.
