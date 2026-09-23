// Command graphql-topic-subscription demonstrates a bounded subscription
// source adapter that checks ambiguous event summaries before delivery.
// Requires TYPESAFE_API_KEY: go run ./examples/api-use-cases/graphql-topic-subscription
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type event struct {
	ID, TenantID, TypedTopic, Summary string
	Verified                          bool
}

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		if errors.Is(err, typesafe.ErrMissingAPIKey) {
			fmt.Fprintln(os.Stderr, "Set TYPESAFE_API_KEY before running this example.")
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer client.Close()

	// The source stream is finite here so the example is runnable. A real
	// subscription uses the same check in a worker with bounded concurrency.
	events := []event{
		{"evt-11", "tenant-42", "", "Checkout requests fail after the latest release.", true},
		{"evt-12", "tenant-42", "billing", "A new invoice is ready.", true},
		{"evt-13", "tenant-other", "", "Checkout service is down.", true},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	delivered, reconcile, err := filterStream(ctx, client, "tenant-42", "outages", events)
	if err != nil {
		fmt.Fprintln(os.Stderr, "subscription:", err)
		os.Exit(1)
	}
	for _, e := range delivered {
		fmt.Println("deliver:", e.ID)
	}
	for _, id := range reconcile {
		fmt.Println("reconcile:", id)
	}
}

func filterStream(ctx context.Context, client *typesafe.Client, tenantID, topic string, events []event) ([]event, []string, error) {
	if tenantID == "" || (topic != "outages" && topic != "billing") {
		return nil, nil, fmt.Errorf("unauthorized tenant or unsupported topic")
	}
	if len(events) > 50 {
		return nil, nil, fmt.Errorf("event batch exceeds concurrency budget")
	}
	var delivered []event
	var reconcile []string
	for _, e := range events {
		if !e.Verified || e.TenantID != tenantID || e.ID == "" {
			continue // source verification and tenant checks precede semantic work
		}
		if e.TypedTopic != "" {
			if e.TypedTopic == topic {
				delivered = append(delivered, e)
			}
			continue
		}
		if len(e.Summary) == 0 || len(e.Summary) > 500 {
			reconcile = append(reconcile, e.ID)
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		resp, err := client.SystemOne(callCtx, &typesafe.SystemOneParams{
			State: map[string]any{"summary": e.Summary, "subscriber_topic": topic},
			Questions: typesafe.Questions{
				"matches": typesafe.Noul{Instructions: "Does the verified event summary concern the subscriber topic?"},
			},
		})
		cancel()
		if err != nil {
			reconcile = append(reconcile, e.ID)
			continue
		}
		p := resp.Nouls()["matches"].Noul
		if p >= 0.80 {
			delivered = append(delivered, e)
		} else if p > 0.20 {
			reconcile = append(reconcile, e.ID)
		}
	}
	return delivered, reconcile, nil
}
