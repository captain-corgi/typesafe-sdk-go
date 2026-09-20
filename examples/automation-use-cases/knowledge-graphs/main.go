// Command knowledge-graphs demonstrates annotating a knowledge graph with
// typed semantic decisions: for each subject-object-sentence triple, one
// call classifies the relation and checks it against the edge already in
// the graph; a confidence gate in plain Go commits the edge, flags the
// contradiction, or queues the triple for human curation.
//
// It mirrors the "Graphs and knowledge graphs" entry of the docs use-case
// map's example automation use cases
// (https://docs.typesafe.ai/concepts/use-case-map#example-automation-use-cases).
//
// Requires TYPESAFE_API_KEY; run with:
//
//	go run ./examples/automation-use-cases/knowledge-graphs
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/captain-corgi/typesafe-sdk-go"
)

// commitBelow is the confidence an edge needs before it enters the graph.
const commitBelow = 0.70

type triple struct {
	subject     string
	object      string
	sentence    string
	currentEdge string // relation currently stored between the pair, "" if none
}

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		if errors.Is(err, typesafe.ErrMissingAPIKey) {
			fmt.Fprintln(os.Stderr, "Set TYPESAFE_API_KEY before running this example.")
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer client.Close()

	triples := []triple{
		{
			subject:  "Ingrid Bergman",
			object:   "Roberto Rossellini",
			sentence: "Bergman starred in Rossellini's 1954 film 'Journey to Italy'.",
		},
		{
			subject:     "Northwind Software",
			object:      "PayFlow",
			sentence:    "Northwind Software completed its acquisition of PayFlow in March 2021.",
			currentEdge: "competes_with",
		},
		{
			subject:  "Atlas Robotics",
			object:   "Cortex Systems",
			sentence: "The two companies announced a joint warehouse-pilot program for 2027.",
		},
	}

	for _, t := range triples {
		fmt.Printf("triple: %s — ? — %s\n", t.subject, t.object)
		fmt.Printf("  source: %s\n", t.sentence)
		annotate(client, t)
		fmt.Println()
	}
}

func annotate(client *typesafe.Client, t triple) {
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{
			"subject":      t.subject,
			"object":       t.object,
			"sentence":     t.sentence,
			"current_edge": t.currentEdge,
		},
		Questions: typesafe.Questions{
			"relation": typesafe.Choice{
				Instructions: "Which relation does the sentence assert between subject and object?",
				Criteria: typesafe.ChoiceCriteria{
					"employs":       "Subject works for or is contracted by object (or vice versa)",
					"acquired":      "Subject acquired object (or vice versa)",
					"partners_with": "Joint project, alliance, or collaboration",
					"competes_with": "Competition or rivalry in a market",
					"created":       "Subject created a work performed, directed, or built by object",
					"no_relation":   "None of the above fits",
				},
			},
			"contradicts_existing": typesafe.Noul{
				Instructions: "If the graph already stores an edge between these entities, " +
					"does the sentence contradict it?",
				Criteria: &typesafe.NoulCriteria{
					True:  "The asserted relation is incompatible with the stored edge",
					False: "Consistent with the stored edge, or no edge exists",
				},
			},
		},
	})
	if err != nil {
		fmt.Println("  error:", err)
		return
	}

	relation := resp.Choices()["relation"]
	contradicts := resp.Nouls()["contradicts_existing"].Noul

	fmt.Printf("  relation: %s (confidence %.2f)\n", relation.Choice, relation.Confidence)
	fmt.Printf("  contradicts existing edge: %.2f\n", contradicts)

	switch {
	case contradicts >= 0.5:
		fmt.Printf("  action: flag contradiction on (%s) — keep both, open curation task\n", t.currentEdge)
	case relation.Choice == "no_relation" || relation.Confidence < commitBelow:
		fmt.Println("  action: queue for human curation (low confidence or no fitting type)")
	default:
		fmt.Printf("  action: commit edge %s -[%s]-> %s\n", t.subject, relation.Choice, t.object)
	}
}
