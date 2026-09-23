// Command classify-ci-failures selects a diagnostic runbook from a CI log excerpt.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/06-classify-ci-failures
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer client.Close()
	logExcerpt := "go test ./...: TestRetryBackoff failed: got 3 attempts, want 2; exit status 1"
	if len(os.Args) > 1 {
		logExcerpt = os.Args[1]
	}
	const jobID = "ci-4821" // The original log and exit status remain the source of truth.
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"job_id": jobID, "exit_status": 1, "log_excerpt": logExcerpt},
		Questions: typesafe.Questions{
			"kind": typesafe.Choice{Instructions: "What best explains this failing CI job?", Criteria: typesafe.ChoiceCriteria{
				"compile": "Compiler or type-check failure", "test_assertion": "A test assertion failed",
				"dependency": "Dependency resolution failed", "environment": "Runner or service environment failed",
				"timeout": "Job or step timed out", "other": "No category clearly fits",
			}},
			"intermittent": typesafe.Noul{Instructions: "Does the excerpt provide evidence that this failure is intermittent?"},
		},
	})
	if err != nil {
		fmt.Printf("%s: manual triage; classification unavailable: %v\n", jobID, err)
		return
	}
	kind := resp.Choices()["kind"]
	runbooks := map[string]string{
		"compile": "check compiler diagnostics", "test_assertion": "inspect failing assertion and test data",
		"dependency": "inspect module downloads", "environment": "inspect runner and services",
		"timeout": "inspect duration and resource limits",
	}
	runbook, known := runbooks[kind.Choice]
	if kind.Confidence < 0.70 || !known {
		fmt.Printf("%s: manual triage; original log: %s\n", jobID, logExcerpt)
		return
	}
	fmt.Printf("%s: %s; runbook: %s; inspect intermittent evidence=%t\n", jobID, kind.Choice, runbook, resp.Nouls()["intermittent"].Noul >= 0.80)
}
