// Package typesafe is a Go client SDK for the TypeSafe AI API
// (https://typesafe.ai).
//
// The API answers typed, named questions about a piece of "state" in a single
// request ("System One"): you POST a state plus a map of questions, and each
// answer's type is determined statically by the kind of question you asked.
//
// # Questions
//
// There are three question primitives:
//
//   - [Noul] asks a yes/no question and answers with [NoulAnswer], a
//     probability between 0 and 1.
//   - [Choice] classifies state into named labels and answers with
//     [ChoiceAnswer], the selected label plus a confidence and per-label
//     probabilities.
//   - [Score] rates state against an ordered rubric and answers with
//     [ScoreAnswer], the expected score plus a confidence, a legend mapping
//     score levels to rubric entries, and per-level probabilities.
//
// Questions may also be supplied as raw dictionaries ([RawQuestion]) for
// fields this SDK does not model; raw questions are passed through to the API
// untouched.
// Raw score criteria implementing JSON or text marshaling are encoded once per
// call and left for API validation. Ordinary empty criteria and cyclic
// pointer/interface chains are rejected before network I/O.
//
// # Quick start
//
//	client, err := typesafe.NewClient() // reads TYPESAFE_API_KEY
//	if err != nil {
//		return err
//	}
//	defer client.Close()
//
//	resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
//	    State: map[string]any{"document": "I was charged twice. Please fix this ASAP."},
//	    Questions: typesafe.Questions{
//	        "urgent": typesafe.Noul{Instructions: "Is this urgent?"},
//	    },
//	})
//	fmt.Println(resp.Nouls()["urgent"].Noul)
//
// # Configuration
//
// Explicit client options take precedence over environment variables, which in
// turn take precedence over defaults. Empty or whitespace-only environment
// values are ignored.
//
//	[TYPESAFE_API_KEY]       API key (required unless WithAPIKey is used)
//	[TYPESAFE_BASE_URL]      API root (default "https://api.typesafe.ai")
//	[TYPESAFE_DEFAULT_MODEL] default model (default "jev-latest")
//	[TYPESAFE_LOG_LEVEL]     debug | info | warn | warning | error | off
//	[TYPESAFE_LOG_BODY]      off | redacted | full (default off)
//
// Base URLs must use HTTPS. WithAllowInsecureHTTP permits HTTP only for
// loopback hosts, which is useful for local development and tests. Responses
// are limited to 16 MiB by default; WithMaxResponseBodySize changes that
// per-client limit. Wire request and response bodies are redacted by default;
// use SetLogBodyMode(LogBodyFull) only in controlled environments.
//
// # Errors and retries
//
// SDK-classified failures share the [TypeSafeError] root: a catch-all
// `errors.As(err, &root)` with a *TypeSafeError target matches them, like
// catching TypeSafeError in the Python SDK. Caller cancellation and deadlines
// return context.Canceled or context.DeadlineExceeded directly, without the SDK
// root; note that an SDK attempt timeout — [TimeoutError] — also satisfies
// errors.Is(err, context.DeadlineExceeded) through its preserved transport
// cause, so match SDK types with errors.As before attributing
// DeadlineExceeded to your own deadline. Failed HTTP responses map to
// typed subclasses of [APIError] such as [RateLimitError], matched with
// [errors.As]. Every request runs under a [RetryPolicy] (connection errors,
// timeouts, and the statuses {408, 429, 500–599} by default). RetryOn
// entries either select a type — an SDK error instance such as
// &NotFoundError{}, or a typed nil like (*MyError)(nil), matched with
// errors.As — or name a sentinel matched strictly by identity with
// errors.Is; [RetryPolicy.Predicate] covers custom matching.
//
// # Logging
//
// The SDK is silent by default. Set TYPESAFE_LOG_LEVEL (debug | info | warn |
// warning | error | off) for diagnostics on stderr, or take full control of
// output and format with [SetLogger]. Secret headers and request/response
// bodies are redacted from wire log output by default.
package typesafe
