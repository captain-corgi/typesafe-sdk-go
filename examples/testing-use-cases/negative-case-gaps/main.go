// Command negative-case-gaps reviews named failure requirements against a
// table-driven test plan. Run separately from go test with TYPESAFE_API_KEY.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

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

	cases := []string{"valid token and JSON creates item", "missing token returns unauthorized", "bad JSON returns validation error"}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"test_cases": cases},
		Questions: typesafe.Questions{
			"missing_credentials": typesafe.Noul{Instructions: "Do the case descriptions cover rejection when credentials are missing?"},
			"malformed_content":   typesafe.Noul{Instructions: "Do the case descriptions cover rejection when request content is malformed?"},
			"conflicting_state":   typesafe.Noul{Instructions: "Do the case descriptions cover rejection when the item already exists in a conflicting state?"},
		},
	})
	if err != nil {
		fmt.Println("INCONCLUSIVE: review all negative requirements;", err)
		return
	}
	suggestions := map[string]string{
		"missing_credentials": "missing token -> ErrUnauthorized, zero writes",
		"malformed_content":   "malformed JSON -> validation status, zero writes",
		"conflicting_state":   "duplicate item -> ErrConflict, unchanged record",
	}
	for _, key := range []string{"missing_credentials", "malformed_content", "conflicting_state"} {
		p := resp.Nouls()[key].Noul
		switch {
		case p <= 0.10:
			fmt.Printf("REVIEW missing row: %s (coverage %.2f)\n", suggestions[key], p)
		case p >= 0.90:
			fmt.Printf("REVIEW possible coverage: %s (%.2f); Go must assert exact outcome\n", key, p)
		default:
			fmt.Printf("INCONCLUSIVE %s: %.2f\n", key, p)
		}
	}
}
