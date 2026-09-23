// Command compare-acceptance-tests points reviewers to likely requirement coverage gaps.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/08-compare-acceptance-tests
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type testCase struct{ path, body string }

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer client.Close()
	requirement := "When the API returns 429, retry only within the configured attempt limit and stop when the context is canceled."
	tests := []testCase{
		{"retry_test.go:42", "TestRetryLimit: server returns 429 repeatedly; assert exactly three HTTP attempts."},
		{"retry_test.go:81", "TestCancelBackoff: cancel context during retry delay; assert request exits with context.Canceled."},
	}
	behaviorCovered, failureCovered := false, false
	uncertain := false
	for _, test := range tests {
		resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
			State: map[string]any{"requirement": requirement, "test": test.body},
			Questions: typesafe.Questions{
				"behavior":     typesafe.Noul{Instructions: "Does this test meaningfully exercise at least one behavior in the requirement?"},
				"failure_path": typesafe.Noul{Instructions: "Does this test exercise the cancellation or retry failure path?"},
			},
		})
		if err != nil {
			fmt.Printf("%s: review manually (%v)\n", test.path, err)
			uncertain = true
			continue
		}
		behavior, failure := resp.Nouls()["behavior"].Noul, resp.Nouls()["failure_path"].Noul
		fmt.Printf("%s: behavior p=%.2f, failure path p=%.2f\n", test.path, behavior, failure)
		behaviorCovered = behaviorCovered || behavior >= 0.80
		failureCovered = failureCovered || failure >= 0.80
		uncertain = uncertain || (behavior > 0.20 && behavior < 0.80) || (failure > 0.20 && failure < 0.80)
	}
	if uncertain {
		fmt.Println("Coverage evidence is uncertain; reviewer should inspect the linked tests.")
	} else if !behaviorCovered || !failureCovered {
		fmt.Println("Likely acceptance gap; add or inspect a test for the missing behavior.")
	} else {
		fmt.Println("Tests appear relevant; run them and review actual assertions before accepting coverage.")
	}
}
