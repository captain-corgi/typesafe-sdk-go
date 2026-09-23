// Command graphql-report-category models a Report.category enum resolver.
// Only declared GraphQL enum values can leave the resolver.
// Requires TYPESAFE_API_KEY: go run ./examples/api-use-cases/graphql-report-category
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

const (
	categoryIncident    = "INCIDENT"
	categoryMaintenance = "MAINTENANCE"
	categoryRequest     = "REQUEST"
	categoryUnknown     = "UNKNOWN"
)

type report struct {
	ID, Summary, StoredCategory string
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
	r := report{ID: "report-17", Summary: "A router failure caused a two-hour customer outage."}
	category, err := resolveCategory(ctx, client, r)
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolver used UNKNOWN:", err)
	}
	fmt.Printf("Report %s category: %s\n", r.ID, category)
}

func resolveCategory(ctx context.Context, client *typesafe.Client, r report) (string, error) {
	// A trusted, declared discriminator needs no model call.
	if declaredCategory(r.StoredCategory) {
		return r.StoredCategory, nil
	}
	if len(r.Summary) == 0 || len(r.Summary) > 1000 {
		return categoryUnknown, fmt.Errorf("report summary missing or too long")
	}
	resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
		State: r.Summary,
		Questions: typesafe.Questions{
			"category": typesafe.Choice{
				Instructions: "Which declared ReportCategory best describes this report summary?",
				Criteria: typesafe.ChoiceCriteria{
					categoryIncident:    "Unexpected service or security incident",
					categoryMaintenance: "Planned maintenance or repair",
					categoryRequest:     "Request for a future change or service",
					categoryUnknown:     "The summary supports none of these categories",
				},
			},
		},
	})
	if err != nil {
		return categoryUnknown, err
	}
	answer := resp.Choices()["category"]
	if answer.Confidence < 0.80 || !declaredCategory(answer.Choice) {
		return categoryUnknown, nil
	}
	return answer.Choice, nil
}

func declaredCategory(value string) bool {
	switch value {
	case categoryIncident, categoryMaintenance, categoryRequest, categoryUnknown:
		return true
	default:
		return false
	}
}
