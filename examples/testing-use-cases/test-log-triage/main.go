// Command test-log-triage adds a tentative label to a failed go test -json
// excerpt. It preserves the exit code and never waives a failing test.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/captain-corgi/typesafe-sdk-go"
)

func main() {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		log.Fatal("set TYPESAFE_API_KEY for this opt-in CI triage command")
	}
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	raw := `{"Action":"output","Test":"TestRetry","Output":"retry_test.go:88: expected 3 calls, got 2\n"}` + "\n" +
		`{"Action":"fail","Test":"TestRetry","Elapsed":0.02}`
	exitCode := 1 // The CI process exit code is captured separately.
	type event struct{ Action, Test, Output string }
	dec := json.NewDecoder(strings.NewReader(raw))
	var excerpt strings.Builder
	failed := false
	for {
		var e event
		if err := dec.Decode(&e); err == io.EOF {
			break
		} else if err != nil {
			log.Fatal("invalid go test -json fixture: ", err)
		}
		if e.Action == "fail" {
			failed = true
		}
		if e.Action == "output" {
			excerpt.WriteString(e.Output)
		}
	}
	if exitCode == 0 || !failed || excerpt.Len() == 0 {
		log.Fatal("deterministic test-run assertion failed")
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: excerpt.String(),
		Questions: typesafe.Questions{"kind": typesafe.Choice{
			Instructions: "What does this short Go test failure excerpt most clearly indicate?",
			Criteria:     typesafe.ChoiceCriteria{"assertion": "A value assertion failed", "timeout": "Test exceeded a time limit", "external_dependency": "An external service failed", "race_report": "Race detector found a data race", "other": "None of these"},
		}},
	})
	if err != nil {
		fmt.Printf("INCONCLUSIVE; CI exit %d remains failed; %v\n", exitCode, err)
		return
	}
	a := resp.Choices()["kind"]
	if a.Confidence < 0.85 || a.Choice == "other" {
		fmt.Printf("INCONCLUSIVE label (%s, %.2f); CI exit %d remains failed\n", a.Choice, a.Confidence, exitCode)
	} else {
		fmt.Printf("REVIEW tentative %s label (%.2f); CI exit %d remains failed; retain raw logs\n", a.Choice, a.Confidence, exitCode)
	}
}
