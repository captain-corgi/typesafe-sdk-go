# Retries and timeouts

Every request runs under a `RetryPolicy`. Owner: `retry.go` (policy, backoff,
budget) and `transport.go` (the loop); contract in
[contracts.md §4](../plans/2026-09-19-typesafe-sdk-go/contracts.md), locked by
`retry_test.go` with injected sleep/rand for determinism.

## The default policy

`typesafe.DefaultRetryPolicy()`: two retries on statuses
{408, 429, 500–599}, connection errors, and timeouts; 0.5s→5s exponential
backoff with 25% jitter; server `Retry-After` honored; a 30s total budget per
SDK call. The `RetryPolicy` field reference lives on the struct itself and in
[README §Retries](../README.md#retries); `Validate()` rejects negative
counts/backoffs, jitter outside [0, 1], and a negative budget.

## The loop

```mermaid
flowchart TB
    s0["execute: snapshot the policy<br>(per-call override wins)"] --> a1{"Attempt n over HTTP"}
    a1 -->|success| done["Parse and return the typed response"]
    a1 -->|error err| c1{"ctx done and err is not<br>an APIError?"}
    c1 -->|yes| x1["Return ctx.Err() unwrapped<br>(cancellation is never retried)"]
    c1 -->|no| c2{"policy.retryable(err)?<br>builtin rule, RetryOn, or Predicate"}
    c2 -->|no| x2["Return err"]
    c2 -->|yes| c3{"attempt &gt; MaxRetries?"}
    c3 -->|yes| x3["Return err — always the last attempt's error"]
    c3 -->|no| d1["delay = server Retry-After,<br>else backoff(attempt)"]
    d1 --> b1{"Budget: delay &gt;= Timeout − elapsed?"}
    b1 -->|yes| x3
    b1 -->|no| sl["Sleep delay (context-aware)"]
    sl --> a2["Attempt n+1 carries X-TypeSafe-Retry-Count"]
    a2 --> a1
```

Semantics worth knowing:

- **Caller cancellation first.** A canceled context returns before the retry
  condition runs, so your `Predicate` never observes caller-side cancellation
  (a deliberate difference from Python's tenacity, which calls it with
  `CancelledError`).
- **The retry condition runs on every failed attempt**, including the final
  one that will not be retried — counting or logging predicates observe every
  failure, like the Python policy.
- **Stop-before-delay budget.** The budget (`Timeout`, default 30s, 0 =
  unlimited) does not interrupt an in-flight attempt; the loop simply refuses
  to *begin* a retry whose preceding delay would reach the budget, surfacing
  the last error. The budget resets per SDK call.
- The policy is **snapshotted per call** (deep-copied), so concurrent calls
  with different overrides cannot interfere; a per-call `Retry` replaces the
  client policy for that call only.

## A retry, end to end

```mermaid
sequenceDiagram
    participant App as Your code
    participant SDK as execute (retry loop)
    participant API as TypeSafe API

    App->>SDK: SystemOne(ctx, params)
    SDK->>API: POST /v1/systemone (attempt 1)
    API-->>SDK: 429 Too Many Requests, retry-after-ms: 750
    Note over SDK: RateLimitError{RetryAfterMs: 750}<br>RespectRetryAfter: server delay wins over backoff
    SDK->>SDK: sleep 750ms
    SDK->>API: POST /v1/systemone (attempt 2)<br>X-TypeSafe-Retry-Count: 1
    API-->>SDK: 200 OK
    SDK-->>App: *SystemOneResponse
```

## Delay math

When there is no server-requested delay (or `RespectRetryAfter` is false),
the delay is exponential backoff with subtractive jitter:

```
exponential = min(BackoffInitial · 2^(attempt−1), BackoffMax)
delay       = round(exponential · (1 − rand() · BackoffJitter), 3 decimals)
```

never exceeding the un-jittered cap. With defaults the un-jittered sequence
is:

| Attempt failed | Delay before next attempt |
|---|---|
| 1 | 0.5s |
| 2 | 1.0s |
| 3 | 2.0s |
| 4 | 4.0s |
| 5 | 5.0s (cap from here) |

At maximum jitter (`rand() = 1.0`) each delay is scaled to 75% of these
values (floor 0.375s on the first). `BackoffInitial: 0` disables backoff
entirely; `MaxRetries: 0` disables retries (one attempt total).

Server-requested delays parse from `retry-after-ms` first, then
`retry-after` (seconds ×1000, or an HTTP date → time remaining, floored at 0);
a negative or non-finite value is not usable.

```mermaid
flowchart TB
    p0{"retry-after-ms present?"}
    p0 -->|yes| p1{"finite and >= 0?"}
    p1 -->|yes| use["Use it (ms)"]
    p1 -->|no| p2
    p0 -->|no| p2{"retry-after present?"}
    p2 -->|no| none["nil — fall back to backoff"]
    p2 -->|yes| p3{"numeric?"}
    p3 -->|yes| p4{"finite and >= 0?"}
    p4 -->|yes| use
    p4 -->|no| none
    p3 -->|no| p5{"parses as HTTP date?"}
    p5 -->|yes| use2["Use time until date, floored at 0"]
    p5 -->|no| none
```

Empty header values count as zero. An exhausted 429 still surfaces
`RateLimitError.RetryAfterMs` so you can schedule your own retry.

## Customizing what gets retried

`RetryOn` entries come in two forms, and the difference is deliberate:

- **Type selectors** — an SDK error instance (`&NotFoundError{}`) or a typed
  nil (`(*MyError)(nil)`): matched via `errors.As`, so wrapped,
  `errors.Join`-ed, and custom-`As` errors all match.
- **Sentinels** — any other value: matched strictly by identity via
  `errors.Is`. Two distinct errors of the same concrete type never match each
  other; give a sentinel a custom `Is` method or use `Predicate` for
  class-based matching of custom errors.

`Predicate` covers anything else and runs once per failed attempt. The full
worked example lives in [`examples/retries-errors`](../examples/retries-errors).

## Timeouts vs the budget

Two independent clocks: the **per-attempt timeout** (resolution rules in
[Configuration](configuration.md#timeout-resolution)) bounds one HTTP
exchange and expires into `*TimeoutError`; the **policy budget** bounds the
whole call including all attempts and delays. Exceeding the budget surfaces
the last attempt's error, never a budget-specific error.
