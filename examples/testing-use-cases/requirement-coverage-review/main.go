// Command requirement-coverage-review annotates a claimed requirement-to-test
// link. Run separately from go test with TYPESAFE_API_KEY set.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/captain-corgi/typesafe-sdk-go"
)

func main() {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		log.Fatal("set TYPESAFE_API_KEY for this opt-in review command")
	}
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	requirementID := "AUTH-17"
	requirement := "An invalid API token returns ErrUnauthorized and never invokes the protected handler."
	testName := "TestInvalidToken"
	excerpt := `if err == nil { t.Fatal("expected error") }`
	if requirementID == "" || !strings.HasPrefix(testName, "Test") {
		log.Fatal("invalid review input")
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"requirement": requirement, "test_name": testName, "test_excerpt": excerpt},
		Questions: typesafe.Questions{"covers": typesafe.Noul{
			Instructions: "Does this test actually exercise the stated behavior, including its named outcome?",
			Criteria:     &typesafe.NoulCriteria{True: "Checks both the specified error and that the handler is not called", False: "Checks a weaker or different outcome"},
		}},
	})
	if err != nil {
		fmt.Printf("INCONCLUSIVE %s/%s: %v\n", requirementID, testName, err)
		return
	}
	p := resp.Nouls()["covers"].Noul
	switch {
	case p <= 0.10:
		fmt.Printf("REVIEW %s/%s: likely coverage mismatch (%.2f); inspect missing assertions\n", requirementID, testName, p)
	case p >= 0.90:
		fmt.Printf("REVIEW %s/%s: possible coverage (%.2f); execute exact Go assertions before marking verified\n", requirementID, testName, p)
	default:
		fmt.Printf("INCONCLUSIVE %s/%s: %.2f; reviewer must inspect\n", requirementID, testName, p)
	}
}
