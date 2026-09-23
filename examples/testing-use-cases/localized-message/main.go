// Command localized-message screens one translation for semantic drift after
// Go verifies placeholder preservation. It does not certify translation quality.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

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

	source := "The operation failed. Please retry later. Reference {id}."
	translation := "La operación falló. Vuelve a intentarlo más tarde. Referencia {id}."
	if strings.Count(source, "{id}") != 1 || strings.Count(translation, "{id}") != 1 {
		log.Fatal("deterministic placeholder assertion failed")
	}
	resp, err := client.SystemOne(context.Background(), &typesafe.SystemOneParams{
		State: map[string]any{"source_language": "English", "translation_language": "Spanish", "source": source, "translation": translation},
		Questions: typesafe.Questions{"preserves": typesafe.Noul{
			Instructions: "Does the translation preserve the instruction to retry later without promising success?",
			Criteria:     &typesafe.NoulCriteria{True: "Says the operation failed and to retry later", False: "Drops retry advice or claims success"},
		}},
	})
	if err != nil {
		fmt.Println("INCONCLUSIVE translation; bilingual review needed;", err)
		return
	}
	answer, ok := resp.Nouls()["preserves"]
	if !ok {
		fmt.Println("INCONCLUSIVE translation; requested answer is missing, bilingual review needed")
		return
	}
	p := answer.Noul
	if p <= 0.10 {
		fmt.Printf("REVIEW likely semantic drift (%.2f)\n", p)
	} else if p < 0.90 {
		fmt.Printf("INCONCLUSIVE translation (%.2f)\n", p)
	} else {
		fmt.Printf("REVIEW possible meaning preservation (%.2f); bilingual reviewer decides\n", p)
	}
}
