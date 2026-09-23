// Command verify-proposed-tool-call checks semantic alignment after exact Go validation.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/18-verify-proposed-tool-call
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type proposedCall struct{ name, packagePath, testPattern string }

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer client.Close()
	request := "Run only the Retry tests in the root Go package."
	call := proposedCall{"go_test", ".", ".*"} // Broadens the requested test pattern.
	// Function, path, argument type, and permission checks are deterministic.
	allowedPackages := map[string]bool{".": true}
	allowedPatterns := map[string]bool{"Retry": true, ".*": true}
	if call.name != "go_test" || !allowedPackages[call.packagePath] || !allowedPatterns[call.testPattern] {
		fmt.Println("reject: invalid function or arguments")
		return
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{
			"user_request":      request,
			"proposed_function": call.name,
			"arguments":         map[string]any{"package": call.packagePath, "test_pattern": call.testPattern},
		},
		Questions: typesafe.Questions{
			"addresses":     typesafe.Noul{Instructions: "Does the proposed call address the user's test request?"},
			"expands_scope": typesafe.Noul{Instructions: "Does any argument expand the scope beyond the user's request?"},
			"mismatch": typesafe.Choice{Instructions: "What is the main mismatch?", Criteria: typesafe.ChoiceCriteria{
				"none": "Call matches request", "wrong_tool": "Wrong function", "broader_scope": "Arguments run more than requested", "other": "Another mismatch",
			}},
		},
	})
	if err != nil {
		fmt.Println("pause tool call for review:", err)
		return
	}
	mismatch := resp.Choices()["mismatch"]
	if resp.Nouls()["addresses"].Noul >= 0.80 && resp.Nouls()["expands_scope"].Noul <= 0.20 && mismatch.Confidence >= 0.75 && mismatch.Choice == "none" {
		fmt.Printf("approved proposal: go test -run %q %s\n", call.testPattern, call.packagePath)
		return
	}
	fmt.Printf("pause for review: mismatch=%s (confidence %.2f); retain full trace\n", mismatch.Choice, mismatch.Confidence)
}
