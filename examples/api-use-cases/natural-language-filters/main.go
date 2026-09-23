// Command natural-language-filters converts a bounded search hint into
// allowlisted filters; Go still enforces tenant scope, time, and result limits.
// Requires TYPESAFE_API_KEY: go run ./examples/api-use-cases/natural-language-filters
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type filters struct {
	TenantID string
	Kind     string
	Status   string
	Since    time.Time
	Limit    int
}

type logEvent struct {
	TenantID, Kind, Status, ID string
	At                         time.Time
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

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	// Tenant identity comes from authenticated API context, never from Jev.
	query := "failed deployments in the last 7 days"
	f, err := parseHint(ctx, client, "tenant-42", query, time.Now())
	if err != nil {
		fmt.Fprintln(os.Stderr, "clarify search hint:", err)
		return
	}
	events := []logEvent{
		{"tenant-42", "deployment", "failed", "evt-101", time.Now().Add(-2 * time.Hour)},
		{"tenant-42", "build", "failed", "evt-102", time.Now().Add(-time.Hour)},
		{"tenant-other", "deployment", "failed", "evt-103", time.Now().Add(-time.Hour)},
	}
	fmt.Printf("filters: tenant=%s kind=%s status=%s since=%s limit=%d\n",
		f.TenantID, f.Kind, f.Status, f.Since.Format(time.RFC3339), f.Limit)
	for _, event := range search(f, events) {
		fmt.Println("result:", event.ID)
	}
}

func parseHint(ctx context.Context, client *typesafe.Client, tenantID, hint string, now time.Time) (filters, error) {
	if tenantID == "" || len(hint) == 0 || len(hint) > 200 {
		return filters{}, fmt.Errorf("invalid tenant or hint")
	}
	resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
		State: hint,
		Questions: typesafe.Questions{
			"kind": typesafe.Choice{
				Instructions: "Which event kind does this search hint request?",
				Criteria: typesafe.ChoiceCriteria{
					"deployment": "Deployment events", "build": "Build events", "other": "Neither kind is clear",
				},
			},
			"status": typesafe.Choice{
				Instructions: "Which status does this search hint request?",
				Criteria: typesafe.ChoiceCriteria{
					"failed": "Failed events", "succeeded": "Successful events", "any": "No status restriction",
				},
			},
			"has_time_window": typesafe.Noul{
				Instructions: "Does this hint request a time window?",
			},
		},
	})
	if err != nil {
		return filters{}, err
	}
	kind, status := resp.Choices()["kind"], resp.Choices()["status"]
	if kind.Confidence < 0.80 || status.Confidence < 0.80 || kind.Choice == "other" {
		return filters{}, fmt.Errorf("event kind or status needs clarification")
	}
	if kind.Choice != "deployment" && kind.Choice != "build" {
		return filters{}, fmt.Errorf("unsupported event kind")
	}
	if status.Choice != "failed" && status.Choice != "succeeded" && status.Choice != "any" {
		return filters{}, fmt.Errorf("unsupported status")
	}

	// Parse only documented phrases in Go. Relative dates are never model output.
	window := resp.Nouls()["has_time_window"].Noul
	f := filters{TenantID: tenantID, Kind: kind.Choice, Status: status.Choice, Limit: 20}
	switch {
	case window >= 0.80 && strings.Contains(strings.ToLower(hint), "last 7 days"):
		f.Since = now.AddDate(0, 0, -7)
	case window >= 0.80 && strings.Contains(strings.ToLower(hint), "today"):
		f.Since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	case window <= 0.20:
		f.Since = now.AddDate(0, 0, -30) // documented default range
	default:
		return filters{}, fmt.Errorf("time window needs clarification")
	}
	return f, nil
}

func search(f filters, events []logEvent) []logEvent {
	result := make([]logEvent, 0, f.Limit)
	for _, event := range events {
		if len(result) == f.Limit {
			break
		}
		if event.TenantID != f.TenantID || event.Kind != f.Kind || event.At.Before(f.Since) {
			continue
		}
		if f.Status != "any" && event.Status != f.Status {
			continue
		}
		result = append(result, event)
	}
	return result
}
