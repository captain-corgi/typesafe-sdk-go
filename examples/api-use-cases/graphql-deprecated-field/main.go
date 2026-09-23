// Command graphql-deprecated-field suggests a replacement field in a
// persisted query only after exact signature checks and two semantic gates.
// Requires TYPESAFE_API_KEY: go run ./examples/api-use-cases/graphql-deprecated-field
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type field struct {
	ID, Description, ReturnType, Arguments string
	Leaf                                   bool
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

	old := field{ID: "Report.summary", Description: "Short human-readable overview of the report.", ReturnType: "String!", Leaf: true}
	current := []field{
		{ID: "Report.abstract", Description: "Short human-readable overview of the report.", ReturnType: "String!", Leaf: true},
		{ID: "Report.details", Description: "Full report body.", ReturnType: "String", Leaf: true},
		{ID: "Report.headline", Description: "One-line title of the report.", ReturnType: "String!", Leaf: true},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	suggestion, err := suggestField(ctx, client, old, current, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migration review:", err)
	}
	if suggestion == "" {
		fmt.Printf("%s: manual migration review required\n", old.ID)
		return
	}
	fmt.Printf("review suggestion: %s -> %s (verify at both schema locations)\n", old.ID, suggestion)
}

func suggestField(ctx context.Context, client *typesafe.Client, old field, current []field, hasSelectionSet bool) (string, error) {
	if old.ID == "" || len(old.Description) == 0 || len(old.Description) > 500 {
		return "", fmt.Errorf("invalid deprecated field metadata")
	}
	criteria := typesafe.ChoiceCriteria{"none": "No compatible current field has the same documented meaning"}
	eligible := make(map[string]field)
	for _, candidate := range current {
		// Schema lookup, return type, arguments, and selection-set compatibility
		// are exact Go checks before Jev sees the shortlist.
		if candidate.ID == old.ID || candidate.ReturnType != old.ReturnType ||
			candidate.Arguments != old.Arguments || candidate.Leaf != old.Leaf ||
			hasSelectionSet == candidate.Leaf || len(candidate.Description) == 0 || len(candidate.Description) > 500 {
			continue
		}
		criteria[candidate.ID] = candidate.Description
		eligible[candidate.ID] = candidate
	}
	if len(eligible) == 0 {
		return "", nil
	}
	first, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
		State: map[string]any{"deprecated_field": old.ID, "deprecated_description": old.Description, "query_has_selection_set": hasSelectionSet},
		Questions: typesafe.Questions{
			"replacement": typesafe.Choice{
				Instructions: "Which current field has the closest documented meaning to this deprecated field in this query?",
				Criteria:     criteria,
			},
		},
	})
	if err != nil {
		return "", err
	}
	answer := first.Choices()["replacement"]
	proposed, ok := eligible[answer.Choice]
	if !ok || answer.Confidence < 0.80 {
		return "", nil
	}
	second, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
		State: map[string]any{
			"old_field": old.ID, "old_description": old.Description,
			"proposed_field": proposed.ID, "proposed_description": proposed.Description,
		},
		Questions: typesafe.Questions{
			"same_semantics": typesafe.Noul{Instructions: "Do the old and proposed fields describe the same result semantics?"},
		},
	})
	if err != nil {
		return "", err
	}
	if second.Nouls()["same_semantics"].Noul < 0.80 {
		return "", nil
	}
	return proposed.ID, nil // suggestion for review, never an automatic rewrite
}
