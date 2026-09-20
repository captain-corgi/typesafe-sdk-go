// Command retries-errors demonstrates the SDK's operational surface:
// errors.As across the whole taxonomy (including RateLimitError.RetryAfterMs),
// a custom RetryPolicy (statuses and budget), per-call model/timeout/header
// overrides, and TYPESAFE_LOG_LEVEL=debug wire logging.
//
// It mirrors the SDK usage docs and covers the Operational category of the
// use-case map.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	TYPESAFE_LOG_LEVEL=debug go run ./examples/retries-errors
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

func main() {
	// Start from the defaults (2 retries on {408, 429, 500-599}, connection
	// errors and timeouts, Retry-After honored, 30s total budget), then
	// override: retry rate limits and overloads up to four times, with a
	// two-minute total budget per call.
	policy := typesafe.DefaultRetryPolicy()
	policy.MaxRetries = 4
	policy.BackoffInitial = 250 * time.Millisecond
	policy.HTTPStatuses = map[int]struct{}{429: {}, 502: {}, 503: {}, 504: {}}
	policy.Timeout = 2 * time.Minute

	client, err := typesafe.NewClient(typesafe.WithRetry(policy))
	if err != nil {
		if errors.Is(err, typesafe.ErrMissingAPIKey) {
			fmt.Fprintln(os.Stderr, "Set TYPESAFE_API_KEY before running this example.")
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Per-call overrides: a specific model, a shorter timeout, and one
	// custom header for request tracing.
	resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
		State: "The export button crashes the app every time.",
		Questions: typesafe.Questions{
			"is_bug": typesafe.Noul{Instructions: "Is this a bug report?"},
		},
		Model:        "jev-latest",
		Timeout:      15 * time.Second,
		ExtraHeaders: map[string]string{"X-Request-Source": "retries-errors-example"},
	})
	if err != nil {
		describe(err)
		os.Exit(1)
	}
	fmt.Printf("answered in one call: is_bug=%.2f (request id %s)\n",
		resp.Nouls()["is_bug"].Noul, resp.RequestID)

	models, err := client.Models.List(ctx, nil)
	if err != nil {
		describe(err)
		os.Exit(1)
	}
	fmt.Printf("%d models available (first: %s)\n", len(models.Models), firstModel(models))
}

// describe matches the full error taxonomy with errors.As.
func describe(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)

	// The shared root: like catching TypeSafeError in the Python SDK, one
	// catch-all handler sees every failure the SDK returns.
	var root *typesafe.TypeSafeError
	if errors.As(err, &root) {
		fmt.Fprintln(os.Stderr, "  a TypeSafe SDK failure (see details below)")
	}

	var rateLimit *typesafe.RateLimitError
	if errors.As(err, &rateLimit) && rateLimit.RetryAfterMs != nil {
		fmt.Fprintf(os.Stderr, "  rate limited; server asked to wait %.0fms\n", *rateLimit.RetryAfterMs)
	}

	var validation *typesafe.ResponseValidationError
	if errors.As(err, &validation) {
		fmt.Fprintf(os.Stderr, "  response invalid at %q\n", validation.FieldPath)
	}

	var timeoutErr *typesafe.TimeoutError
	if errors.As(err, &timeoutErr) {
		fmt.Fprintf(os.Stderr, "  timed out after %s (retried per policy)\n", timeoutErr.Duration)
	}

	var connectionErr *typesafe.ConnectionError
	if errors.As(err, &connectionErr) {
		fmt.Fprintf(os.Stderr, "  transport cause: %v\n", errors.Unwrap(connectionErr))
	}

	var apiErr *typesafe.APIError
	if errors.As(err, &apiErr) {
		fmt.Fprintf(os.Stderr, "  endpoint %s returned status %d (request id %s)\n",
			apiErr.Endpoint, apiErr.Status, apiErr.RequestID)
	}
}

func firstModel(models *typesafe.ListModelsResponse) string {
	if len(models.Models) == 0 {
		return "(none)"
	}
	return models.Models[0].Name
}
