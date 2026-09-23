// Command incident-semantic-features joins semantic probabilities with measured incident facts.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/20-incident-semantic-features
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type featureRow struct {
	Schema                string   `json:"schema"`
	IncidentID            string   `json:"incident_id"`
	Service               string   `json:"service"`
	DurationMinutes       int      `json:"duration_minutes"`
	MeasuredErrorRate     float64  `json:"measured_error_rate"`
	DependencyRegressionP *float64 `json:"dependency_regression_p"`
	CustomerVisibleP      *float64 `json:"customer_visible_p"`
	NormalizedImpact      *float64 `json:"normalized_impact"`
	NeedsReview           bool     `json:"needs_review"`
}

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer client.Close()
	narrative := "Checkout requests failed for customers for 42 minutes after a payment gateway SDK upgrade."
	row := featureRow{
		Schema: "incident-features-v1", IncidentID: "INC-482", Service: "checkout",
		DurationMinutes: 42, MeasuredErrorRate: 0.18, NeedsReview: true,
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: narrative,
		Questions: typesafe.Questions{
			"dependency_regression": typesafe.Noul{Instructions: "Does this incident narrative mention a dependency regression?"},
			"customer_visible":      typesafe.Noul{Instructions: "Does the narrative describe a customer-visible failure?"},
			"impact": typesafe.Score{Instructions: "How severe is the described impact?", Criteria: typesafe.ScoreCriteria{
				"Internal or negligible", "Degraded experience", "Customer-facing outage",
			}},
		},
	})
	if err == nil {
		impact := resp.Scores()["impact"]
		if impact.Confidence >= 0.70 {
			dependency := resp.Nouls()["dependency_regression"].Noul
			visible := resp.Nouls()["customer_visible"].Noul
			normalized := impact.Score / 2 // The three-level rubric ranges from 0 to 2.
			row.DependencyRegressionP = &dependency
			row.CustomerVisibleP = &visible
			row.NormalizedImpact = &normalized
			row.NeedsReview = false
		}
	} else {
		fmt.Fprintln(os.Stderr, "semantic feature extraction unavailable:", err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(row); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
