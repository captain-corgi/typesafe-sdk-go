// Command openapi-description-drift flags semantic prose changes only after
// an exact operation and schema comparison has passed.
// Requires TYPESAFE_API_KEY: go run ./examples/api-use-cases/openapi-description-drift
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type operation struct {
	Method, Path, SchemaHash, Description, ErrorDescription string
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

	old := operation{"GET", "/v1/events", "sha256:demo-shape-17", "Returns events in no guaranteed order.", "404 means the collection does not exist."}
	current := operation{"GET", "/v1/events", "sha256:demo-shape-17", "Returns events newest first.", "404 means the collection does not exist."}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	decision, err := assessDrift(ctx, client, old, current)
	if err != nil {
		fmt.Fprintln(os.Stderr, "assessment:", err)
	}
	fmt.Printf("%s %s: %s\nold: %s\nnew: %s\n", old.Method, old.Path, decision, old.Description, current.Description)
}

func assessDrift(ctx context.Context, client *typesafe.Client, old, current operation) (string, error) {
	if old.Method != current.Method || old.Path != current.Path || old.SchemaHash != current.SchemaHash {
		return "structural_diff_requires_review", nil
	}
	if old.Description == current.Description && old.ErrorDescription == current.ErrorDescription {
		return "no_prose_change", nil
	}
	if len(old.Description)+len(current.Description)+len(old.ErrorDescription)+len(current.ErrorDescription) > 4000 {
		return "review_prose_change", fmt.Errorf("operation prose exceeds review limit")
	}
	resp, err := client.SystemOne(ctx, &typesafe.SystemOneParams{
		State: map[string]any{
			"old_description": old.Description, "new_description": current.Description,
			"old_error_description": old.ErrorDescription, "new_error_description": current.ErrorDescription,
		},
		Questions: typesafe.Questions{
			"behavior": typesafe.Noul{Instructions: "Does the new description promise materially different operation behavior?"},
			"ordering": typesafe.Noul{Instructions: "Do the old and new descriptions give materially different ordering guarantees?"},
			"errors":   typesafe.Noul{Instructions: "Do the old and new error descriptions give materially different error meanings?"},
		},
	})
	if err != nil {
		return "review_prose_change", err
	}
	for _, name := range []string{"behavior", "ordering", "errors"} {
		// Positive or uncertain answers both warrant a human contract review.
		if resp.Nouls()[name].Noul > 0.20 {
			return "review_prose_change", nil
		}
	}
	return "no_semantic_drift_detected_run_contract_tests", nil
}
