// Command semantic-tags asks independent yes/no questions, then maps positive
// answers to existing tag IDs for a many-to-many PostgreSQL join table.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/relational-db-use-cases/semantic-tags
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
		log.Fatal("set TYPESAFE_API_KEY")
	}
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	note := "After the upgrade, retries fail with 503 and the old client no longer connects."
	recordID := int64(208) // ID returned by the application's INSERT INTO records.
	tags := map[string]int64{"retry_failure": 31, "compatibility_break": 32}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: note,
		Questions: typesafe.Questions{
			"retry_failure": typesafe.Noul{Instructions: "Does the note report failed retries?", Criteria: &typesafe.NoulCriteria{
				True: "A retry attempt fails", False: "No retry failure is described",
			}},
			"compatibility_break": typesafe.Noul{Instructions: "Does the note describe a compatibility break?", Criteria: &typesafe.NoulCriteria{
				True: "Previously compatible client behavior no longer works", False: "No compatibility break is described",
			}},
		},
	})
	if err != nil {
		log.Fatalf("leave tags absent and queue record %d for review: %v", recordID, err)
	}
	// These example thresholds must be calibrated on labeled application data.
	for _, name := range []string{"retry_failure", "compatibility_break"} {
		probability := resp.Nouls()[name].Noul
		if probability < 0.90 {
			fmt.Printf("%s: %.2f; leave absent or review\n", name, probability)
			continue
		}
		fmt.Printf("%s: %.2f\nSQL: INSERT INTO record_tags (record_id, tag_id) VALUES ($1, $2) ON CONFLICT (record_id, tag_id) DO NOTHING\nargs: %#v\n", name, probability, []any{recordID, tags[name]})
	}
}
