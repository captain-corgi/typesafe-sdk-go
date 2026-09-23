// Command structured-update-guard checks whether a proposed resolution note
// contradicts the structured status before proposing an atomic database update.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/relational-db-use-cases/structured-update-guard
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

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

	tenantID, incidentID := int64(42), int64(810)
	oldStatus, proposedStatus := "investigating", "resolved"
	note := "Mitigation deployed at 14:05 UTC; the failure no longer reproduces."
	if proposedStatus != "investigating" && proposedStatus != "resolved" {
		log.Fatal("invalid status: enforce the application enum before inference")
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"proposed_status": proposedStatus, "resolution_note": note},
		Questions: typesafe.Questions{"contradicts_status": typesafe.Noul{
			Instructions: "Does the resolution note contradict the proposed structured status?",
			Criteria: &typesafe.NoulCriteria{
				True:  "The note says the incident remains unresolved or failure still reproduces",
				False: "The note is consistent with the incident being resolved",
			},
		}},
	})
	if err != nil {
		queue(tenantID, incidentID, proposedStatus, note, "API failure")
		return
	}
	probability := resp.Nouls()["contradicts_status"].Noul
	if probability >= 0.10 { // A low acceptance threshold; calibrate for status changes.
		queue(tenantID, incidentID, proposedStatus, note, fmt.Sprintf("contradiction probability %.2f", probability))
		return
	}
	// The application executes this in a transaction alongside its audit insert.
	// Old status is an optimistic concurrency predicate; a zero-row update
	// means the authoritative incident changed while the API call was pending.
	resolvedAt := time.Now().UTC()
	fmt.Printf("BEGIN;\nSQL: UPDATE incidents SET status = $1, resolution_note = $2, resolved_at = $3 WHERE tenant_id = $4 AND id = $5 AND status = $6\nargs: %#v\nCOMMIT only after checking RowsAffected == 1 and writing the audit row.\n",
		[]any{proposedStatus, note, resolvedAt, tenantID, incidentID, oldStatus})
}

func queue(tenantID, incidentID int64, status, note, reason string) {
	fmt.Printf("keep current incident unchanged; review reason: %s\n", reason)
	fmt.Printf("SQL: INSERT INTO incident_update_reviews (tenant_id, incident_id, proposed_status, proposed_note, reason) VALUES ($1, $2, $3, $4, $5)\nargs: %#v\n",
		[]any{tenantID, incidentID, status, note, reason})
}
