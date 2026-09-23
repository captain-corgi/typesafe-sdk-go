// Command router-fixture-evaluation runs an opt-in live evaluation over labeled
// routing fixtures. It writes a JSON artifact and prints a confusion matrix.
// Set TYPESAFE_API_KEY and a pinned TYPESAFE_EVAL_MODEL before running.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/captain-corgi/typesafe-sdk-go"
)

const questionVersion = "support-router-v1"
const minConfidence = 0.85 // Replace with a threshold calibrated on local labels.
const instructions = "Which supported action does the user request? Choose other when none fits."

var criteria = typesafe.ChoiceCriteria{
	"refund":      "Request a refund for a paid order",
	"track_order": "Ask where an existing order is",
	"cancel":      "Cancel an existing order",
	"other":       "No supported action or unclear request",
}

type fixture struct{ ID, Text, Label string }
type result struct {
	Fixture       fixture                 `json:"fixture"`
	Model         string                  `json:"model"`
	Question      string                  `json:"question_version"`
	Instructions  string                  `json:"instructions"`
	Criteria      typesafe.ChoiceCriteria `json:"criteria"`
	Threshold     float64                 `json:"threshold"`
	Choice        string                  `json:"choice,omitempty"`
	Confidence    float64                 `json:"confidence,omitempty"`
	Probabilities map[string]float64      `json:"probabilities,omitempty"`
	Outcome       string                  `json:"outcome"`
	Error         string                  `json:"error,omitempty"`
}
type summary struct {
	Model       string                    `json:"requested_model"`
	Total       int                       `json:"total"`
	Abstentions int                       `json:"abstentions"`
	Matrix      map[string]map[string]int `json:"confusion_matrix"`
	Results     []result                  `json:"results"`
}

func main() {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		log.Fatal("set TYPESAFE_API_KEY for this opt-in evaluation command")
	}
	model := os.Getenv("TYPESAFE_EVAL_MODEL")
	if model == "" || strings.Contains(model, "latest") {
		log.Fatal("set TYPESAFE_EVAL_MODEL to a pinned model identifier")
	}
	client, err := typesafe.NewClient(typesafe.WithModel(model))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	fixtures := []fixture{
		{"R01", "Where is my parcel?", "track_order"},
		{"R02", "Has order 153 shipped yet?", "track_order"},
		{"R03", "Please refund the duplicate charge.", "refund"},
		{"R04", "I want my money back for this order.", "refund"},
		{"R05", "Cancel order 153 before it ships.", "cancel"},
		{"R06", "Stop my pending purchase.", "cancel"},
		{"R07", "How do I change my password?", "other"},
		{"R08", "Can I speak to an agent?", "other"},
	}
	s := summary{Model: model, Total: len(fixtures), Matrix: map[string]map[string]int{}, Results: make([]result, 0, len(fixtures))}
	for _, f := range fixtures {
		r := result{Fixture: f, Model: model, Question: questionVersion, Instructions: instructions, Criteria: criteria, Threshold: minConfidence}
		resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
			State:     f.Text,
			Questions: typesafe.Questions{"action": typesafe.Choice{Instructions: instructions, Criteria: criteria}},
		})
		if err != nil {
			r.Outcome, r.Error = "inconclusive", err.Error()
		} else if a, ok := resp.Choices()["action"]; !ok {
			r.Outcome, r.Error = "inconclusive", "missing typed Choice answer"
		} else {
			r.Model, r.Choice, r.Confidence, r.Probabilities = resp.Model, a.Choice, a.Confidence, a.Probabilities
			// A confident "other" is measurable against the human-labeled
			// out-of-scope fixtures; deployment routing may still review it.
			if a.Confidence < minConfidence {
				r.Outcome = "inconclusive"
			} else if a.Choice == f.Label {
				r.Outcome = "agreement_for_review"
			} else {
				r.Outcome = "disagreement_for_review"
			}
			if r.Outcome != "inconclusive" {
				if s.Matrix[f.Label] == nil {
					s.Matrix[f.Label] = map[string]int{}
				}
				s.Matrix[f.Label][a.Choice]++
			}
		}
		if r.Outcome == "inconclusive" {
			s.Abstentions++
		}
		fmt.Printf("%s expected=%s choice=%s confidence=%.2f outcome=%s\n", f.ID, f.Label, r.Choice, r.Confidence, r.Outcome)
		s.Results = append(s.Results, r)
	}
	path := fmt.Sprintf("router-evaluation-%d.json", time.Now().UTC().UnixNano())
	file, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(file).Encode(s); err != nil {
		file.Close()
		log.Fatal(err)
	}
	if err := file.Close(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("abstention rate: %d/%d = %.2f\nconfusion matrix: %v\nartifact: %s\n", s.Abstentions, s.Total, float64(s.Abstentions)/float64(s.Total), s.Matrix, path)
}
