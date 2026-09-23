// Command surviving-mutation-review prioritizes one mutation that compiled
// and survived the Go suite. Jev cannot declare the mutant equivalent.
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
		log.Fatal("set TYPESAFE_API_KEY for this opt-in mutation review command")
	}
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	requirement := "A retry stops after at most three attempts."
	before := "if attempts >= 3 { return err }"
	after := "if attempts > 3 { return err }"
	compiled, suitePassed := true, true // Results from a separate deterministic run.
	if !compiled || !suitePassed || before == after {
		log.Fatal("this is not a surviving mutation")
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"requirement": requirement, "before": before, "after": after},
		Questions: typesafe.Questions{"material": typesafe.Noul{
			Instructions: "Does this small mutation appear to change the behavior promised by the requirement?",
			Criteria:     &typesafe.NoulCriteria{True: "Allows a fourth attempt despite the stated cap", False: "No apparent change to the stated cap"},
		}},
	})
	if err != nil {
		fmt.Println("INCONCLUSIVE mutation; survivor remains open for review;", err)
		return
	}
	answer, ok := resp.Nouls()["material"]
	if !ok {
		fmt.Println("INCONCLUSIVE mutation; requested answer is missing, survivor remains open for review")
		return
	}
	p := answer.Noul
	if p >= 0.90 {
		fmt.Printf("REVIEW likely material survivor (%.2f): add an exact fourth-attempt regression assertion\n", p)
	} else if p > 0.10 {
		fmt.Printf("INCONCLUSIVE survivor (%.2f): developer must inspect\n", p)
	} else {
		fmt.Printf("REVIEW possible equivalent mutant (%.2f); equivalence is unproven\n", p)
	}
}
