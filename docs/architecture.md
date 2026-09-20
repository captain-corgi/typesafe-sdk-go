# Architecture

The SDK is a single flat Go package (`package typesafe`) at the repo root —
no `pkg/`, no `internal/`, no subpackages. One file per concern. It has **zero
runtime dependencies**: the Go standard library only.

## System context

```mermaid
flowchart TB
    subgraph app["Your application"]
        code["Application code<br>(any number of goroutines)"]
        ctx["context.Context<br>(cancellation and deadlines)"]
    end
    subgraph sdk["typesafe package (this repo)"]
        client["Client<br>(safe for concurrent use)"]
        api["SystemOne / Models.List<br>(bounded response buffering)"]
        loopc["prepareRequest + execute<br>(retry loop)"]
        httpc["*http.Client<br>(SDK-owned, or injected via WithHTTPClient)"]
    end
    subgraph tsapi["TypeSafe AI API"]
        ep1["POST /v1/systemone"]
        ep2["GET /v1/models"]
    end
    code -->|"NewClient(options...)"| client
    client --> api --> loopc --> httpc
    code -->|"ctx per call"| loopc
    httpc -->|"HTTPS, JSON, Bearer auth"| ep1
    httpc --> ep2
    ep1 -->|"typed answers + usage"| httpc
```

Design intent: your code holds one `Client` for the process lifetime; all
per-call variation (model, timeout, retry policy, extra headers) is passed per
call and never leaks between calls.

## Module map

```mermaid
flowchart LR
    client["client.go<br>Client, functional options,<br>SystemOne / Models params"] --> transport["transport.go<br>request prep + retry loop"]
    client --> config["config.go<br>option &gt; env &gt; default"]
    client --> questions["questions.go<br>Noul / Choice / Score / Raw,<br>pre-network validation"]
    client --> responses["responses.go<br>strict decode, typed answers"]
    transport --> retry["retry.go<br>RetryPolicy, backoff, budget"]
    transport --> errors["errors.go<br>error taxonomy"]
    transport --> logging["logging.go<br>slog, silent by default"]
    retry --> errors
    responses --> errors
    responses --> json["json.go<br>marshal preserving<br>explicit nulls"]
    questions --> json
    questions --> errors
    config --> errors
```

## Life of a call

```mermaid
sequenceDiagram
    participant App as Your code
    participant C as Client.SystemOne
    participant V as normalizeQuestions
    participant P as prepareRequest
    participant X as execute (retry loop)
    participant H as http.Client
    participant API as TypeSafe API

    App->>C: SystemOne(ctx, params)
    C->>V: validate questions
    V-->>C: normalized map, or *TypeSafeError (zero HTTP attempts)
    C->>P: method, path, body, timeout, extra headers
    Note over P: merge headers, force-set protected headers,<br>resolve per-attempt timeout
    P-->>C: preparedRequest (immutable)
    C->>X: execute(ctx, req, retry override, parse)
    loop attempt 1 .. MaxRetries+1
        X->>H: Do(request under attempt context)
        H->>API: HTTPS
        API-->>H: HTTP response
        H-->>X: response + buffered body
        alt non-2xx
            X->>X: map status to typed error (errors.go)
        else 2xx
            X->>X: parse into *SystemOneResponse (responses.go)
        end
    end
    X-->>App: typed response, or the last attempt's error
```

Two properties worth knowing:

- **Encode once.** The request body is marshaled a single time per call and
  reused verbatim on retries — including raw criteria with custom JSON/text
  marshalers, which must not be invoked repeatedly.
- **Fail before dialing.** Question validation, option validation, and body
  encoding all happen before any network I/O.

## Concurrency model

The client is safe for concurrent use. Everything mutable about a call lives
in that call's stack; the shared `Client` is read-only after construction
(verified under `-race` in CI).

```mermaid
flowchart TB
    subgraph shared["Client — shared, read-only after NewClient"]
        cfg["config: base URL, model, resolved headers"]
        pol["client-level RetryPolicy"]
        hc["*http.Client (shared Transport, Jar, CheckRedirect)"]
        closed["closed atomic.Bool"]
    end
    subgraph callA["goroutine A: SystemOne"]
        pa["retry policy snapshot (deep copy)"]
        ha["merged header set (local clone)"]
        ta["attempt context + budget clock"]
    end
    subgraph callB["goroutine B: Models.List"]
        pb["retry policy snapshot (deep copy)"]
        tb["attempt context"]
    end
    callA -.->|"reads"| shared
    callB -.->|"reads"| shared
    callA -.->|"sends on"| hc
    callB -.->|"sends on"| hc
```

## Client lifecycle

```mermaid
stateDiagram-v2
    [*] --> Active: NewClient(options...) — validates options, resolves config
    Active --> Active: SystemOne / Models.List, any number, concurrently
    Active --> Closed: Close() — closes idle connections, idempotent
    Closed --> Closed: further calls return an error wrapping ErrClientClosed
    Closed --> [*]
```

`Close` closes idle connections of the underlying `*http.Client` — the
SDK-owned one *and* one you supplied via `WithHTTPClient`. Using a closed
client is a typed error (`ErrClientClosed`), not a transport panic. Go has no
full-close API, so `Close` means *idle* connections only
([README deviation 10](../README.md#deviations-from-the-python-sdk)).

## Decision ledger

Decisions that shape everything above, with their rationale:

- **Zero runtime dependencies** — mirrors the TypeScript SDK's zero-dep
  philosophy; the HTTP, JSON, and logging machinery is all stdlib
  (`net/http`, `encoding/json`, `log/slog`).
- **Flat single package** — the surface is small and stable; subpackages would
  add import-path ceremony without a real boundary.
- **Hand-written wire structs, no codegen** — the question/answer set is
  small and stable; codegen would add a build step and a dependency for
  nothing.
- **Python v0.7.0 is the parity baseline** — where Python and JS references
  disagree, Python wins; deliberate Go deviations are ledgered in
  [README §Deviations](../README.md#deviations-from-the-python-sdk)
- **Validate before network I/O** — the same client-side guarantee Python
  gets from eager construction, adapted to Go constructors, which cannot
  return errors
- **Silent-by-default `log/slog`** — libraries must not log unless asked;
  see [Observability](observability.md).
- **Security defaults** — wire bodies are redacted by default, response
  buffering is bounded, HTTPS base URLs are required, and SDK-owned body
  fields cannot be overridden by `ExtraBody`; loopback HTTP is an explicit
  opt-in.

## Where to read next

- How options, env vars, and timeouts resolve: [Configuration](configuration.md)
- What the retry loop does exactly: [Retries and timeouts](retries.md)
- What bytes go on the wire: [Wire protocol](wire-protocol.md)
