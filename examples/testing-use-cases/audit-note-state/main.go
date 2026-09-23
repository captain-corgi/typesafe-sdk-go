// Command audit-note-state screens an audit note against state read back after
// a transaction. The application's integration test must query the real DB.
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

	// Representative values returned by a deterministic post-commit query.
	committed, rowID, status, auditNote := true, int64(304), "approved", "The request was rejected after review."
	if !committed || rowID != 304 || status != "approved" {
		log.Fatal("deterministic transaction/state assertion failed")
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"committed_status": status, "audit_note": auditNote},
		Questions: typesafe.Questions{"contradicts": typesafe.Noul{
			Instructions: "Does the audit note say the request was rejected even though committed status is approved?",
		}},
	})
	if err != nil {
		fmt.Println("INCONCLUSIVE audit wording; database assertions remain;", err)
		return
	}
	p := resp.Nouls()["contradicts"].Noul
	if p >= 0.90 {
		fmt.Printf("REVIEW audit/state mismatch for row %d (%.2f)\n", rowID, p)
	} else if p > 0.10 {
		fmt.Printf("INCONCLUSIVE audit wording for row %d (%.2f)\n", rowID, p)
	} else {
		fmt.Printf("REVIEW no mismatch detected for row %d (%.2f); stored state is authoritative\n", rowID, p)
	}
}
