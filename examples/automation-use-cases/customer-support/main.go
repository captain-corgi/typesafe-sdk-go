// Command customer-support demonstrates the speculative fan-out pattern: six
// questions about one ticket in a single request (including a raw question
// passthrough), followed by a plain Go decision tree over the typed answers.
//
// It mirrors the docs "Pattern: speculative fan-out" page and the Customer
// Support entry of the use-case map's example automation use cases.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/customer-support
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

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

	// Structured state: everything the triage decision needs, in one payload.
	state := map[string]any{
		"subject": "App crashes when I export a report",
		"message": "The export button crashes the app every time. I need this report today. " +
			"Also, I think my last invoice was wrong — charge me once, not twice!",
		"account_tier": "pro",
	}

	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: state,
		Questions: typesafe.Questions{
			"category": typesafe.Choice{
				Instructions: "What is this ticket about?",
				Criteria: typesafe.ChoiceCriteria{
					"bug":         "Broken functionality",
					"billing":     "Charges, invoices, or subscriptions",
					"how-to":      "Asking how to use a feature",
					"feature_req": "Asking for new functionality",
				},
			},
			"severity": typesafe.Score{
				Instructions: "How severe is the impact described?",
				Criteria:     typesafe.ScoreCriteria{"cosmetic", "workaround exists", "blocked"},
			},
			"has_repro_steps": typesafe.Noul{
				Instructions: "Did the customer describe steps that reproduce the problem?",
			},
			"requests_refund": typesafe.Noul{
				Instructions: "Is the customer asking for money back?",
			},
			"frustration": typesafe.Score{
				Instructions: "How frustrated does the customer sound?",
				Criteria:     typesafe.ScoreCriteria{"calm", "annoyed", "upset"},
			},
			// Raw passthrough: a field this SDK version does not model,
			// sent to the API untouched.
			"satisfaction_risk": typesafe.RawQuestion{
				"type":         "score",
				"instructions": "How likely is churn without prompt help?",
				"criteria":     []any{"low", "medium", "high"},
				"weight":       2,
			},
		},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	category := resp.Choices()["category"]
	severity := resp.Scores()["severity"]
	frustration := resp.Scores()["frustration"]
	churnRisk := resp.Scores()["satisfaction_risk"]
	hasRepro := resp.Nouls()["has_repro_steps"].Noul
	requestsRefund := resp.Nouls()["requests_refund"].Noul

	fmt.Println("category:", category.Choice)
	fmt.Printf("category confidence: %.2f\n", category.Confidence)
	fmt.Printf("severity: %.2f\n", severity.Score)
	fmt.Printf("has repro steps: %.2f\n", hasRepro)
	fmt.Printf("requests refund: %.2f\n", requestsRefund)
	fmt.Printf("frustration: %.2f\n", frustration.Score)
	fmt.Printf("churn risk: %.2f\n", churnRisk.Score)
	fmt.Println()

	// Decision tree in plain Go — no extra LLM calls needed.
	var actions []string
	if category.Choice == "bug" && severity.Score >= 1.5 && hasRepro >= 0.5 {
		actions = append(actions, "escalate to engineering (bug with repro steps and real impact)")
	}
	if requestsRefund >= 0.5 {
		actions = append(actions, "flag for billing review (refund requested)")
	}
	if frustration.Score >= 1.5 || churnRisk.Score >= 1.5 {
		actions = append(actions, "priority response SLA (high frustration / churn risk)")
	}
	if category.Confidence < 0.6 {
		actions = append(actions, "route to human triage (low category confidence)")
	}
	if len(actions) == 0 {
		actions = append(actions, "standard queue")
	}
	slices.Sort(actions)
	for _, action := range actions {
		fmt.Println("action:", action)
	}
}
