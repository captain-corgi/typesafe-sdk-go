// Command existing-foreign-key selects among existing, SQL-shortlisted
// deployment rows and prints a scoped association insert. It never asks the
// model to invent a foreign key.
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/relational-db-use-cases/existing-foreign-key
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type deployment struct {
	id      int64
	service string
	started string
}

func main() {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		log.Fatal("set TYPESAFE_API_KEY")
	}
	client, err := typesafe.NewClient()
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	note := "The gateway rollout on Tuesday caused the incident."
	incidentID, tenantID := int64(810), int64(42)
	// An application's indexed, tenant-scoped search returns only these rows.
	fmt.Println("shortlist SQL: SELECT id, service, started_at FROM deployments WHERE tenant_id = $1 AND service = $2 AND started_at >= $3 AND started_at < $4 ORDER BY started_at DESC LIMIT 5")
	fmt.Printf("shortlist args: %#v\n", []any{tenantID, "gateway", "2026-09-22T00:00:00Z", "2026-09-23T00:00:00Z"})
	candidates := []deployment{{id: 501, service: "gateway", started: "2026-09-22T09:00:00Z"}, {id: 502, service: "gateway", started: "2026-09-22T18:00:00Z"}}
	if len(candidates) == 0 {
		fmt.Println("no candidate: save note unattached for human linking")
		return
	}
	byLabel := make(map[string]deployment, len(candidates))
	visibleCandidates := make([]map[string]any, 0, len(candidates))
	criteria := typesafe.ChoiceCriteria{"none": "The note refers to none of these deployments"}
	for i, c := range candidates {
		label := fmt.Sprintf("candidate_%d", i+1)
		byLabel[label] = c
		visibleCandidates = append(visibleCandidates, map[string]any{"label": label, "service": c.service, "started_at": c.started})
		criteria[label] = fmt.Sprintf("%s started at %s", c.service, c.started)
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"note": note, "candidates": visibleCandidates},
		Questions: typesafe.Questions{"deployment": typesafe.Choice{
			Instructions: "Which listed deployment does the note reference? Choose none if ambiguous or absent.", Criteria: criteria,
		}},
	})
	if err != nil {
		log.Fatalf("save note unattached for review: %v", err)
	}
	answer := resp.Choices()["deployment"]
	chosen, ok := byLabel[answer.Choice]
	if !ok || answer.Confidence < 0.90 { // Calibrate on labeled links.
		fmt.Printf("leave note unattached: choice %q confidence %.2f\n", answer.Choice, answer.Confidence)
		return
	}
	for label, probability := range answer.Probabilities {
		if label != answer.Choice && answer.Confidence-probability < 0.20 {
			fmt.Println("competing candidate: request human linking")
			return
		}
	}
	// This INSERT rechecks both row scopes before the FK-constrained write.
	fmt.Printf("SQL: INSERT INTO incident_deployments (tenant_id, incident_id, deployment_id) SELECT $1, i.id, d.id FROM incidents i JOIN deployments d ON d.tenant_id = i.tenant_id WHERE i.tenant_id = $1 AND i.id = $2 AND d.id = $3 ON CONFLICT DO NOTHING\nargs: %#v\n", []any{tenantID, incidentID, chosen.id})
}
