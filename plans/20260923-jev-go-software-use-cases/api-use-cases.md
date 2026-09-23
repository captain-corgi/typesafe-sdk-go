# Jev in Go API development: 10 REST and GraphQL use cases

Research date: 2026-09-23. These are **proposed Go/API adaptations** of TypeSafe's documented `Choice`, `Noul`, and `Score` patterns. They are architecture ideas, not published Jev benchmarks or ready-made API features. The companion [Go software use cases](go-software-use-cases.md) cover general dispatch, search, linting, and other programming patterns. Here the focus is where a semantic judgment fits in an HTTP contract, webhook adapter, GraphQL resolver, subscription, or API contract workflow.

## Boundary for API code

Jev answers narrow questions about text or structured state. `Choice` selects from caller-defined labels, `Noul` returns a yes probability, and `Score` measures a described spectrum. The Go service owns HTTP methods and status codes, schema validation, authentication and authorization, GraphQL parsing and execution, side effects, and all database queries. [TypeSafe's primitive reference](https://docs.typesafe.ai/primitives) describes these answer shapes; the [GraphQL specification](https://spec.graphql.org/September2025/) defines validation and resolver execution. Pass the incoming Go request context through the SDK call, and set a deadline suitable for the API's latency budget; Go's [`net/http` documentation](https://pkg.go.dev/net/http#Request.Context) explains request cancellation.

Every option below needs an explicit outcome for Jev errors, low confidence, and `other`/`none`. `Choice` and `Score` have `confidence`; `Noul` has only its probability. Pick thresholds from labeled traffic at the cost of each mistake, as in [TypeSafe's confidence guidance](https://docs.typesafe.ai/confidence). Avoid sending secrets or unnecessary customer data to a model call.

## RESTful APIs

### 1. Check whether a narrative agrees with structured request fields

- **Placement:** `POST /deployments` or `PATCH /change-requests`, after JSON/schema validation and authorization, before a side effect.
- **Jev question:** `Noul`: “Does `reason` clearly describe the same environment and action as `target_environment` and `operation`?” State contains the short reason and the two validated fields.
- **Go behavior and fallback:** Go continues the normal path when the answer is strong, and routes a likely mismatch to an existing review/clarification flow. On uncertainty or API failure, it requires clarification or review. The server never changes the caller's typed fields based on the narrative; exact environment enum checks remain in Go.
- **Evidence:** **Adaptation** of TypeSafe's [independent yes/no verification question](https://docs.typesafe.ai/primitives) and [composition guidance](https://docs.typesafe.ai/concepts/how-to-build-with-system-one). [OpenAPI](https://spec.openapis.org/oas/v3.1.1.html) describes the request schema that still controls syntax and structure.

### 2. Translate an optional natural-language search hint into bounded filters

- **Placement:** `GET /logs?query=failed+deploys+last+week` or `POST /search` after ordinary query parsing. This is a specific API feature whose contract promises a natural-language hint, not a replacement for typed filter parameters.
- **Jev question:** Separate `Choice` questions choose `event_kind` (`deployment`, `build`, `other`) and `status` (`failed`, `succeeded`, `any`); a `Noul` asks whether the text mentions a time window. Parse dates and ranges in Go from bounded candidates or explicit parameters.
- **Go behavior and fallback:** Convert known labels into an allowlisted filter struct, enforce tenant scope and limits, and execute the existing search function. If the hint is ambiguous, return clarification or use the documented unfiltered search mode within normal bounds. Do not let Jev generate a query language string or SQL.
- **Evidence:** **Adaptation** of TypeSafe's [function calling pattern](https://docs.typesafe.ai/cookbooks/function_calling), [Choice design](https://docs.typesafe.ai/primitives), and [pre-parsed value extraction](https://docs.typesafe.ai/cookbooks/pre_parsed_value_extraction_cookbook).

### 3. Classify an underspecified inbound webhook payload

- **Placement:** A Go `POST /webhooks/legacy-provider` adapter when the verified provider sends a generic event name and a narrative change description, without a reliable subtype field.
- **Jev question:** `Choice`: “Which supported change does `description` report?” Criteria: `created`, `modified`, `removed`, `other`. A `Noul` can separately ask whether the event describes a reversal.
- **Go behavior and fallback:** Verify the webhook signature, parse its JSON, deduplicate its event ID, and honor any reliable typed event name first. For the underspecified payload, Jev's label selects an internal handler only when confidence is adequate. Hold or log `other` and uncertain events for reconciliation. Never infer authenticity from the narrative.
- **Evidence:** **Adaptation** of [TypeSafe intent routing](https://docs.typesafe.ai/patterns/intent-routing). [Stripe's webhook guidance](https://docs.stripe.com/webhooks) illustrates signature checks and typed event dispatch; a provider with an explicit `event.type` should use that field directly.

### 4. Map opaque upstream error prose to stable API problem types

- **Placement:** REST facade around an upstream service whose failure body contains human-readable text but no stable machine-readable error code.
- **Jev question:** `Choice`: “Which known upstream failure does this sanitized message describe?” Criteria: `quota_exhausted`, `temporary_unavailable`, `invalid_payload`, `other`.
- **Go behavior and fallback:** Map an accepted label through a fixed table of public problem `type` URIs and reviewed titles. Go chooses the HTTP status from trusted transport/upstream facts and writes a safe `application/problem+json` body. If uncertain, use a generic upstream-failure problem; do not copy raw stack traces or secrets into the response.
- **Evidence:** **Adaptation** of [TypeSafe classification](https://docs.typesafe.ai/primitives). [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457.html) defines problem type, status, and disclosure considerations for HTTP APIs.

### 5. Review semantic drift in an OpenAPI change

- **Placement:** A Go CI tool comparing old and new OpenAPI descriptions for an API PR, after a structural spec diff.
- **Jev question:** `Noul`: “Does the new description promise materially different behavior for this operation even though its method, path, and schema are unchanged?” Ask separately about changed error meaning or ordering guarantees when relevant.
- **Go behavior and fallback:** Point reviewers to the exact operation and changed prose; a positive or uncertain result requests review. Continue to use OpenAPI diffing and contract tests for exact shape and wire behavior. A model judgment does not establish compatibility by itself.
- **Evidence:** **Adaptation** of [TypeSafe verification questions](https://docs.typesafe.ai/primitives) and its [semantic code lint use case](https://docs.typesafe.ai/concepts/use-case-map). The [OpenAPI specification](https://spec.openapis.org/oas/v3.1.1.html) includes natural-language descriptions alongside machine-readable structure.

## GraphQL APIs

### 6. Resolve a semantic enum field from stored prose

- **Placement:** A `Report.category: ReportCategory` field resolver when a report has only a narrative and the schema exposes a bounded enum for clients.
- **Jev question:** `Choice`: “Which `ReportCategory` best describes `report.summary`?” Criteria match the GraphQL enum values plus `UNKNOWN` if the schema defines it.
- **Go behavior and fallback:** Map the selected label to a declared enum value; cache a stable classification if appropriate. On failure or low confidence, return `UNKNOWN` if it is a valid enum member, or a nullable field error/null according to the schema. Do not emit undeclared enum values. Batch/load source records using the GraphQL server's normal resolver strategy.
- **Evidence:** **Adaptation** of TypeSafe's [Choice primitive](https://docs.typesafe.ai/primitives). The [GraphQL specification](https://spec.graphql.org/September2025/) defines enum values and result coercion.

### 7. Resolve a union for legacy records without a trustworthy discriminator

- **Placement:** A `legacyRecord: LegacyRecord` query resolver where `LegacyRecord = Incident | MaintenanceNotice | UnknownRecord`, but older source records have only free-text descriptions.
- **Jev question:** `Choice`: “Which declared record type does this description support?” Criteria: `incident`, `maintenance_notice`, `unknown`.
- **Go behavior and fallback:** The GraphQL server's abstract-type resolver maps an accepted label to one of the union's declared object types, then its ordinary field resolvers populate that object. Unknown or uncertain records become `UnknownRecord`, or a field error if the schema has no safe member. Exact discriminators, when present, take priority.
- **Evidence:** **Adaptation** of [TypeSafe's bounded classification](https://docs.typesafe.ai/primitives). The [GraphQL specification](https://spec.graphql.org/September2025/) requires a union to resolve to a declared object type during execution.

### 8. Add a semantic predicate field to a query result

- **Placement:** `Document.supportsClaim(claim: String!): Boolean` resolver, after the caller has selected a specific document. This adds a typed semantic predicate without changing document retrieval.
- **Jev question:** `Noul`: “Does `document.excerpt` directly support `claim`, rather than merely mention similar terms?” State includes the claim and a bounded excerpt.
- **Go behavior and fallback:** Threshold the probability for the Boolean field only if the API contract documents that policy. Prefer a richer nullable result type with probability or review state when clients need uncertainty. On timeout or ambiguous probability, return a nullable field error instead of asserting `false`; GraphQL's executor handles partial data and `errors`.
- **Evidence:** **Adaptation** of [TypeSafe Noul](https://docs.typesafe.ai/primitives) and [citation checking](https://docs.typesafe.ai/cookbooks/citation_check). The [GraphQL specification](https://spec.graphql.org/September2025/) defines field errors and nullable results.

### 9. Filter a subscription stream by the meaning of event text

- **Placement:** `Subscription.eventUpdates(topic: String!): Event` source-stream adapter for events that have a verified source and free-text summary but no reliable topic tag.
- **Jev question:** `Noul`: “Does `event.summary` concern subscriber topic `topic`?” The topic is a validated, permitted subscription argument, preferably one of a bounded set.
- **Go behavior and fallback:** Apply tenant and authorization checks and cheap deterministic topic filters first. Call Jev only for remaining ambiguous events, and deliver an event only when the result passes the documented threshold. On failure or uncertainty, skip semantic delivery or use an explicit unfiltered mode chosen at subscription setup. Bound concurrency and retain event IDs for replay/reconciliation.
- **Evidence:** **Adaptation** of [TypeSafe Noul](https://docs.typesafe.ai/primitives) and its [classification pattern](https://docs.typesafe.ai/patterns/intent-routing). The [GraphQL subscription execution rules](https://spec.graphql.org/September2025/) define an application-specific source stream and execution per event.

### 10. Suggest replacements for deprecated GraphQL fields

- **Placement:** A Go developer tool that scans persisted queries after a GraphQL schema marks a field deprecated. It has the old field's description, the query's surrounding selection set, and a shortlist of current fields with their descriptions.
- **Jev question:** `Choice`: “Which current field has the closest documented meaning to this deprecated field in this query?” Criteria are the shortlist's field IDs plus `none`. A companion `Noul` asks whether the old and proposed fields describe the same result semantics.
- **Go behavior and fallback:** Show a migration suggestion and the two schema locations to a reviewer. Go checks field existence, return types, arguments, and selection-set compatibility before offering any mechanical rewrite; uncertain or `none` results remain manual. Runtime GraphQL validation still rejects invalid operations.
- **Evidence:** **Adaptation** of TypeSafe's [bounded selection](https://docs.typesafe.ai/primitives) and [hierarchical classification approach](https://docs.typesafe.ai/cookbooks/hierarchical_classification). The [GraphQL specification](https://spec.graphql.org/September2025/) defines deprecated schema elements and operation validation.

## Practical evaluation

For each proposed endpoint or resolver, collect real examples and labels, measure error rates and latency against the existing deterministic baseline, and specify the fallback in the API contract. Keep classifications observable with question/version identifiers, decision labels, and confidence or probability, while logging only data allowed by the service's privacy rules. For GraphQL lists and subscriptions, account for call volume and avoid one unbounded network call per item or event.
