# TypeSafe AI SDK for Go — Examples

Copy-paste starting points: from `go get` to a working use case in minutes.
Every example is a single `package main` program using **only the Go standard
library plus this SDK**, and every one runs against the **live API** — set
`TYPESAFE_API_KEY` first:

```sh
export TYPESAFE_API_KEY=ts_live_...
go run ./examples/quickstart
```

Without a key, each example prints a friendly hint and exits. Offline
behavior is the test suite's job, not the examples'.

The catalog mirrors the docs
[use-case map](https://docs.typesafe.ai/concepts/use-case-map) one-for-one:
[`automation-use-cases/`](automation-use-cases) covers every entry of its
[example automation use cases](https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases)
list, and [`task-categories/`](task-categories) covers every row of its
[example task categories](https://docs.typesafe.ai/concepts/use-case-map#example-task-categories)
table. Two basics stay at the root.

## Basics

| Example | Mirrors (docs page) | Demonstrates |
|---|---|---|
| [`quickstart`](quickstart) | Quickstart | Minimal client from env; one call with all three primitives; typed answers, probabilities, confidence, usage |
| [`retries-errors`](retries-errors) | SDK usage docs | `errors.As` across the taxonomy (incl. `RateLimitError.RetryAfterMs`); custom `RetryPolicy`; per-call `Model`/`Timeout`/`ExtraHeaders`; `TYPESAFE_LOG_LEVEL=debug` |

## Automation use cases

One directory per entry of the docs
[example automation use cases](https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases)
list, under [`automation-use-cases/`](automation-use-cases).

| Example | Docs use case | Demonstrates |
|---|---|---|
| [`search-and-retrieval`](automation-use-cases/search-and-retrieval) | Search and retrieval | Per-passage `Noul` answer gate + relevance `Score`; ranked context window for RAG |
| [`scientific-discovery`](automation-use-cases/scientific-discovery) | Scientific discovery | Inclusion/exclusion `Noul` battery + reporting check + relevance `Score`; include / exclude / review verdict |
| [`model-routing`](automation-use-cases/model-routing) | Model routing | Intent `Choice` + difficulty `Score` + reasoning `Noul`; cheapest-model routing table with a confidence gate |
| [`llm-guardrails`](automation-use-cases/llm-guardrails) | LLM guardrails | `Noul` hazard battery + harm-severity `Score`; strict/permissive threshold policies; block/review/support/pass routing with precedence |
| [`semantic-code-linting`](automation-use-cases/semantic-code-linting) | Semantic code linting | Team-convention `Noul` battery + severity `Score`; CI-style findings with a nonzero exit on violation |
| [`predictive-features`](automation-use-cases/predictive-features) | Feature extraction for predictive modeling | Free text → feature map of `Noul`s + normalized sentiment `Score`; deterministic feature row for a classical model |
| [`recruiting`](automation-use-cases/recruiting) | Recruiting | Competency `Score`s + hard-requirement `Noul`s + recommendation `Choice`; rubric and model merged conservatively |
| [`lead-generation`](automation-use-cases/lead-generation) | Lead generation | Structured map state; multiple `Score` answers merged into a weighted composite and priority buckets |
| [`customer-support`](automation-use-cases/customer-support) | Customer support | Six questions in one call (incl. a `RawQuestion` passthrough); Go decision tree — escalate bugs, flag refunds, priority SLA |
| [`insurance-claims`](automation-use-cases/insurance-claims) | Insurance claims | FNOL `Choice` + complexity `Score` + missing-info/fraud `Noul`s; straight-through / adjuster review / SIU referral |
| [`financial-crime`](automation-use-cases/financial-crime) | Financial crime | Typology `Noul`, entity-resolution `Noul`, alert `Choice`, risk `Score`; prioritized investigator routing |
| [`legal-compliance`](automation-use-cases/legal-compliance) | Legal and compliance | Prohibited-claim `Noul` battery + missing-disclaimer check + severity `Score`; approve / revise / escalate to legal |
| [`ecommerce-marketplaces`](automation-use-cases/ecommerce-marketplaces) | E-commerce marketplaces | Category/condition normalization `Choice`s, prohibited/counterfeit `Noul`s; publish / hold / remove |
| [`moderation-trust-safety`](automation-use-cases/moderation-trust-safety) | Moderation / trust & safety | Company-specific T&S `Noul` battery + severity `Score`; allow/warn/review/block policy matrix |
| [`advertising`](automation-use-cases/advertising) | Advertising | Brand-safety, claims, and ad-page-alignment `Noul`s + creative `Score` + audience-fit `Choice` |
| [`gaming`](automation-use-cases/gaming) | Gaming | Credible-cheating and toxic-chat `Noul`s + frustration `Score` + churn `Noul`; sorted action list |
| [`risk-assessment`](automation-use-cases/risk-assessment) | Risk assessment | Risk-type `Choice`, four-level severity `Score`, control/recurrence `Noul`s; priority register entry |
| [`demand-forecasting`](automation-use-cases/demand-forecasting) | Demand forecasting | Per-note demand signals map-reduced into an aggregate forecast adjustment with risk flags |
| [`knowledge-graphs`](automation-use-cases/knowledge-graphs) | Graphs and knowledge graphs | Relation `Choice` + contradiction `Noul` per subject-object triple; commit / flag / queue with a confidence gate |

## Task categories

One directory per row of the docs
[example task categories](https://docs.typesafe.ai/concepts/use-case-map#example-task-categories)
table, under [`task-categories/`](task-categories).

| Example | Docs category | When to use | Demonstrates |
|---|---|---|---|
| [`classification`](task-categories/classification) | Classification | One category wins | Topic `Choice` with sorted probabilities; confidence gate between auto-tagging and a human queue |
| [`detection`](task-categories/detection) | Detection | Probability one property is present | Spam/phishing/promotional `Noul` battery with per-signal thresholds |
| [`scoring`](task-categories/scoring) | Scoring | Ordered rubric answer | Response-quality `Score` (with rubric probabilities) + resolves `Noul`; ticket next step |
| [`routing`](task-categories/routing) | Routing | Category picks next code path | Voice-banking intent `Choice`; confidence gates — < 0.6 human agent, ≥ 0.85 auto-acts, in between confirm |
| [`search`](task-categories/search) | Search | Find items matching an NL query | Per-item match `Noul` with explicit criteria; threshold turns probabilities into a matched set |
| [`retrieval`](task-categories/retrieval) | Retrieval | Workflow needs relevant context | Per-passage relevance `Score`; best-first packing of a RAG context block under a word budget |
| [`ranking`](task-categories/ranking) | Ranking | Order by semantic relevance/quality | Map-reduce re-ranking: per-passage relevance `Score`, sort, top-k reduce |
| [`verification`](task-categories/verification) | Verification | Check artifact for failure modes | Citation-support `Noul` + support-strength `Score` + overstatement check; verified / review / FAIL |
| [`ml-feature-extraction`](task-categories/ml-feature-extraction) | ML feature extraction | Downstream classical ML needs semantic signals | Review → feature map emitted as one deterministic model-input row |
| [`structured-data-extraction`](task-categories/structured-data-extraction) | Structured data extraction | Recover known fields from unstructured input | Enum fields as `Choice`s, booleans as `Noul`s, buckets as `Score`s; low-confidence fields flagged for review |

Conventions shared by every example:

- a header comment naming what it demonstrates, the docs entry it mirrors,
  and its run command;
- deterministic, readable stdout (`label: value` lines) so you can compare
  against the numbers published in the docs;
- both state shapes are covered across the set: plain strings (quickstart,
  detection, moderation, reranking, ...) and structured maps (recruiting,
  lead generation, gaming, ...);
- no network mocking — they exercise the SDK exactly the way real code
  would, including error paths.

The use-case map: <https://docs.typesafe.ai/concepts/use-case-map>.
