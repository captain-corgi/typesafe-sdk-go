// Command narrative-field-agreement checks a deployment reason against typed
// fields before an API handler performs a deployment.
// Requires TYPESAFE_API_KEY: go run ./examples/api-use-cases/narrative-field-agreement
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type deploymentRequest struct {
	Environment string
	Operation   string
	Reason      string
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

	// In a real POST /deployments handler, req.Context() carries cancellation
	// from the HTTP caller. Validation and authorization happen before this call.
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	input := deploymentRequest{
		Environment: "production",
		Operation:   "rollback",
		Reason:      "Roll back the production deployment because the new build fails startup checks.",
	}
	decision, err := reviewDeployment(ctx, client, input)
	if err != nil {
		fmt.Fprintln(os.Stderr, "semantic check:", err)
	}
	fmt.Printf("environment=%s operation=%s decision=%s\n", input.Environment, input.Operation, decision)
}

func reviewDeployment(ctx context.Context, client *typesafe.Client, input deploymentRequest) (string, error) {
	if input.Environment != "staging" && input.Environment != "production" {
		return "reject_invalid_environment", nil
	}
	if input.Operation != "deploy" && input.Operation != "rollback" {
		return "reject_invalid_operation", nil
	}
	if len(input.Reason) == 0 || len(input.Reason) > 500 {
		return "reject_invalid_reason", nil
	}

	resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
		State: map[string]any{
			"target_environment": input.Environment,
			"operation":          input.Operation,
			"reason":             input.Reason,
		},
		Questions: typesafe.Questions{
			"agrees": typesafe.Noul{
				Instructions: "Does the reason clearly describe the same environment and action as target_environment and operation?",
				Criteria: &typesafe.NoulCriteria{
					True:  "Both the environment and action agree with the typed fields",
					False: "Either conflicts, is missing, or is too unclear to tell",
				},
			},
		},
	})
	if err != nil {
		return "clarification_or_review", err
	}
	if resp.Nouls()["agrees"].Noul >= 0.80 {
		return "continue_validated_request", nil
	}
	return "clarification_or_review", nil
}
