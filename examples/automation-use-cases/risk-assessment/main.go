// Command risk-assessment demonstrates turning an unstructured incident
// report into a risk-register entry: a risk-type classification, a
// four-level severity rubric, and control-failure / recurrence checks,
// composed into a priority in plain Go.
//
// It mirrors the "Risk assessment" entry of the docs use-case map's
// example automation use cases
// (https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/risk-assessment
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

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

	incidents := []string{
		"A vendor support engineer was emailed a production access link by an intern. " +
			"The link worked — single sign-on did not require a second factor for vendor " +
			"accounts. No data was viewed; we revoked the link after 40 minutes. Third " +
			"similar near-miss this quarter.",
		"Office kitchen flooding from a failed dishwasher hose damaged cabinet doors. " +
			"Facilities replaced the hose the same day; no equipment affected.",
	}

	for i, report := range incidents {
		fmt.Printf("incident %d: %s\n", i+1, report)
		assess(client, fmt.Sprintf("RSK-%04d", i+1), report)
		fmt.Println()
	}
}

func assess(client *typesafe.Client, id, report string) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: report,
		Questions: typesafe.Questions{
			"risk_type": typesafe.Choice{
				Instructions: "What type of risk does this incident represent?",
				Criteria: typesafe.ChoiceCriteria{
					"security":    "Unauthorized access, data exposure, or credential weakness",
					"operational": "Process, equipment, or facility failure disrupting work",
					"financial":   "Monetary loss, billing error, or fraud exposure",
					"compliance":  "Regulatory, contractual, or policy breach",
				},
			},
			"severity": typesafe.Score{
				Instructions: "Rate the severity of this incident.",
				Criteria:     typesafe.ScoreCriteria{"low", "medium", "high", "critical"},
			},
			"control_failure": typesafe.Noul{
				Instructions: "Did a control that should have prevented this incident fail?",
				Criteria: &typesafe.NoulCriteria{
					True:  "An existing policy, gate, or alarm failed to stop or flag it",
					False: "No control was expected to catch this",
				},
			},
			"recurrence_likely": typesafe.Noul{
				Instructions: "Without action, is a similar incident likely to recur?",
			},
		},
	})
	if err != nil {
		fmt.Println("  error:", err)
		return
	}

	riskType := resp.Choices()["risk_type"]
	severity := resp.Scores()["severity"]
	controlFailure := resp.Nouls()["control_failure"].Noul
	recurrence := resp.Nouls()["recurrence_likely"].Noul

	fmt.Printf("  risk type: %s (confidence %.2f)\n", riskType.Choice, riskType.Confidence)
	fmt.Printf("  severity: %.2f/3 (confidence %.2f)\n", severity.Score, severity.Confidence)
	fmt.Printf("  control failure: %.2f\n", controlFailure)
	fmt.Printf("  recurrence likely: %.2f\n", recurrence)

	priority := "P4"
	switch {
	case severity.Score >= 2.5:
		priority = "P1"
	case severity.Score >= 1.5:
		priority = "P2"
	case severity.Score >= 0.5:
		priority = "P3"
	}
	if recurrence >= 0.5 && controlFailure >= 0.5 && (priority == "P3" || priority == "P4") {
		priority = "P2" // a failing control plus recurrence bumps the review
	}

	fmt.Printf("  register entry: %s | %s | severity %.2f | %s\n",
		id, riskType.Choice, severity.Score, priority)
}
