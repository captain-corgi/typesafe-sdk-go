// Command rerank-search-shortlist reorders results from a deterministic lexical search.
// Requires TYPESAFE_API_KEY; run with: go run ./examples/go-software-use-cases/05-rerank-search-shortlist
package main

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/captain-corgi/typesafe-sdk-go"
)

type document struct {
	id, text string
	lexical  int
	semantic float64
}

func main() {
	client, err := typesafe.NewClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer client.Close()
	query := "How do I set an SDK request timeout?"
	docs := []document{
		{"config-timeout", "Use WithTimeout when constructing a client to set the per-attempt request timeout.", 0, 0},
		{"context-timeout", "Pass a context with a deadline to SystemOne to cap the whole operation.", 0, 0},
		{"retry-policy", "Retry policy controls attempts after transient request failures.", 0, 0},
		{"response-fields", "SystemOne returns answer fields and a request ID.", 0, 0},
	}
	// A real index would provide this shortlist. This tiny lexical index keeps the boundary visible.
	terms := []string{"sdk", "request", "timeout", "set"}
	shortlist := make([]document, 0, len(docs))
	for _, doc := range docs {
		for _, term := range terms {
			if strings.Contains(strings.ToLower(doc.text), term) {
				doc.lexical++
			}
		}
		if doc.lexical > 0 {
			shortlist = append(shortlist, doc)
		}
	}
	slices.SortStableFunc(shortlist, func(a, b document) int { return cmp.Compare(b.lexical, a.lexical) })
	for i := range shortlist {
		resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
			State: map[string]any{"query": query, "passage": shortlist[i].text},
			Questions: typesafe.Questions{
				"answers":   typesafe.Noul{Instructions: "Does this passage directly answer the query?"},
				"relevance": typesafe.Score{Instructions: "How relevant is the passage?", Criteria: typesafe.ScoreCriteria{"Unrelated", "Related", "Direct answer"}},
			},
		})
		if err != nil {
			fmt.Println("Semantic reranking failed; keep lexical order:", err)
			printResults(shortlist)
			return
		}
		score := resp.Scores()["relevance"]
		if score.Confidence < 0.65 || resp.Nouls()["answers"].Noul < 0.65 {
			continue // Uncertain passages keep a zero semantic score.
		}
		shortlist[i].semantic = score.Score
	}
	slices.SortStableFunc(shortlist, func(a, b document) int { return cmp.Compare(b.semantic, a.semantic) })
	printResults(shortlist)
}

func printResults(docs []document) {
	for i, doc := range docs {
		if i == 2 {
			break
		}
		fmt.Printf("%d. %s (lexical=%d, semantic=%.2f)\n", i+1, doc.id, doc.lexical, doc.semantic)
	}
}
