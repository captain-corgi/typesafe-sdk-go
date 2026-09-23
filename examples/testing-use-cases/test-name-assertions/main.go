// Command test-name-assertions flags a test name that promises more than its
// assertion checks. This review command runs only with TYPESAFE_API_KEY.
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

	testName := "TestRetryStopsAfterThreeAttempts"
	setup := "A transport always returns HTTP 503."
	assertion := `if err == nil { t.Fatal("expected failure") }`
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"test_name": testName, "setup": setup, "assertions": assertion},
		Questions: typesafe.Questions{"overpromises": typesafe.Noul{
			Instructions: "Does the test name promise a behavior that the assertions do not check?",
			Criteria:     &typesafe.NoulCriteria{True: "Name promises retry count, but assertion omits the attempt count", False: "Assertions check the named retry behavior"},
		}},
	})
	if err != nil {
		fmt.Println("INCONCLUSIVE: inspect name and assertion manually;", err)
		return
	}
	p := resp.Nouls()["overpromises"].Noul
	if p >= 0.90 {
		fmt.Printf("REVIEW %s: add an attempt-count assertion or rename (%.2f)\n", testName, p)
	} else if p <= 0.10 {
		fmt.Printf("REVIEW %s: no mismatch detected (%.2f); passing code is not proven correct\n", testName, p)
	} else {
		fmt.Printf("INCONCLUSIVE %s: %.2f\n", testName, p)
	}
}
