package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRepositoryExamples guards the published site: every example in the
// repository must have a README entry and a valid flow.mmd.
func TestRepositoryExamples(t *testing.T) {
	site, err := build(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if site.Total == 0 {
		t.Fatal("no examples found")
	}
	for _, c := range site.Catalogs {
		for _, ex := range c.Examples {
			if ex.Summary == "" {
				t.Errorf("%s: empty summary; add a package doc comment", ex.ID)
			}
			if ex.Description == "" {
				t.Errorf("%s: empty README description", ex.ID)
			}
			if !strings.Contains(ex.Source, "package main") {
				t.Errorf("%s: source not loaded", ex.ID)
			}
		}
	}
}

func TestSummarize(t *testing.T) {
	tests := []struct {
		name, doc, want string
	}{
		{
			name: "one paragraph with run line",
			doc:  "Command semantic-command-switch routes a command to a handler.\nRequires TYPESAFE_API_KEY; run with: go run ./x\n",
			want: "Routes a command to a handler.",
		},
		{
			name: "later paragraphs dropped",
			doc:  "Command customer-support demonstrates fan-out:\nsix questions.\n\nIt mirrors a docs page.\n\nRequires TYPESAFE_API_KEY; run with:\n\n\tgo run ./x\n",
			want: "Demonstrates fan-out: six questions.",
		},
		{
			name: "no command prefix",
			doc:  "Package docs here.",
			want: "Package docs here.",
		},
		{name: "empty", doc: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := summarize(tt.doc); got != tt.want {
				t.Errorf("summarize() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInspectSourceCountsKinds(t *testing.T) {
	src := `// Command demo does things.
package main

import "github.com/captain-corgi/typesafe-sdk-go"

var q = typesafe.Questions{
	"a": typesafe.Noul{Instructions: "a"},
	"b": typesafe.Noul{Instructions: "b"},
	"c": typesafe.Choice{Instructions: "c"},
	"d": typesafe.Score{Instructions: "d"},
	"e": typesafe.RawQuestion{"type": "noul"},
}
`
	summary, kinds, err := inspectSource(src)
	if err != nil {
		t.Fatal(err)
	}
	if summary != "Does things." {
		t.Errorf("summary = %q", summary)
	}
	want := map[string]int{"Noul": 2, "Choice": 1, "Score": 1, "Raw": 1}
	for k, v := range want {
		if kinds[k] != v {
			t.Errorf("kinds[%s] = %d, want %d", k, kinds[k], v)
		}
	}
}

func TestCheckDiagram(t *testing.T) {
	tests := []struct {
		name, diagram, wantErr string
	}{
		{name: "ok", diagram: "flowchart TD\n  A[\"x\"]:::sdk --> B(\"y\"):::outcome\n"},
		{name: "wrong type", diagram: "graph TD\n  A:::sdk\n", wantErr: "flowchart"},
		{name: "unknown class", diagram: "flowchart TD\n  A:::call\n", wantErr: "unknown node class"},
		{name: "classDef", diagram: "flowchart TD\n  A:::sdk\n  classDef sdk fill:#fff\n", wantErr: "classDef"},
		{name: "no classes", diagram: "flowchart TD\n  A --> B\n", wantErr: "no node"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkDiagram(tt.diagram)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestBuildReportsMissingPieces(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := "// Command x does x.\npackage main\n\nfunc main() {}\n"
	write("examples/README.md", "| Example | Demonstrates |\n|---|---|\n| [`quickstart`](quickstart) | Basics |\n| [`gone`](gone) | Missing dir |\n")
	write("examples/quickstart/main.go", main)
	write("examples/task-categories/unlisted/main.go", main)
	write("examples/task-categories/unlisted/flow.mmd", "flowchart TD\n  A:::sdk\n")

	_, err := build(root)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{
		"quickstart/flow.mmd is missing",
		"README lists gone",
		"task-categories/unlisted is missing from examples/README.md",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%v", want, err)
		}
	}
}
