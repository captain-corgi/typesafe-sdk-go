// Command pagegen builds the example catalog for the GitHub Pages site.
//
// It reads examples/README.md (catalog order and descriptions), every
// examples/**/main.go (source, summary, question kinds), and the flow.mmd
// Mermaid diagram next to each program, then writes one JSON file the page
// loads at runtime. It fails when an example lacks a diagram or a README
// entry, so the site can never silently drop an example.
//
// Run from the repository root:
//
//	go run ./tools/pagegen
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Site is the JSON document consumed by docs/github-page/index.html.
type Site struct {
	Total    int       `json:"total"`
	Catalogs []Catalog `json:"catalogs"`
}

// Catalog groups examples that share a top-level directory.
type Catalog struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Blurb    string    `json:"blurb"`
	Examples []Example `json:"examples"`
}

// Example is one runnable program under examples/.
type Example struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Catalog     string         `json:"catalog"`
	Description string         `json:"description"`
	Summary     string         `json:"summary"`
	Kinds       map[string]int `json:"kinds"`
	Run         string         `json:"run"`
	Source      string         `json:"source"`
	Diagram     string         `json:"diagram"`
}

type catalogMeta struct {
	id, title, blurb string
}

// catalogs fixes the order and copy of the site's catalog groups. A new
// top-level examples directory must be added here before it can ship.
var catalogs = []catalogMeta{
	{"basics", "Basics", "Start here: the minimal client, and the error and retry toolkit."},
	{"go-software-use-cases", "Go software", "Go keeps dispatch, validation, and side effects; the API answers bounded questions about meaning."},
	{"api-use-cases", "REST & GraphQL APIs", "Where a semantic decision fits after the usual request, schema, and authorization checks."},
	{"relational-db-use-cases", "Relational databases", "PostgreSQL patterns: answers become allowlisted SQL and arguments for your own driver."},
	{"testing-use-cases", "Testing", "Opt-in live evaluation and review. A semantic answer can flag a case, but never makes a failing test pass."},
	{"automation-use-cases", "Automation", "One program per entry of the docs use-case map's example automation list."},
	{"task-categories", "Task categories", "One program per row of the docs use-case map's task category table."},
}

// diagramClasses are the node classes the page styles; see flow.mmd files.
var diagramClasses = []string{"state", "sdk", "noul", "choice", "score", "raw", "policy", "outcome", "fail"}

var questionKinds = map[string]string{
	"Noul":        "Noul",
	"Choice":      "Choice",
	"Score":       "Score",
	"RawQuestion": "Raw",
}

func main() {
	root := flag.String("root", ".", "repository root")
	out := flag.String("out", filepath.Join("docs", "github-page", "examples.json"), "output file, relative to -root unless absolute")
	flag.Parse()

	site, err := build(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pagegen:", err)
		os.Exit(1)
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(site); err != nil {
		fmt.Fprintln(os.Stderr, "pagegen:", err)
		os.Exit(1)
	}
	dst := *out
	if !filepath.IsAbs(dst) {
		dst = filepath.Join(*root, dst)
	}
	if err := os.WriteFile(dst, buf.Bytes(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "pagegen:", err)
		os.Exit(1)
	}
	fmt.Printf("pagegen: wrote %d examples in %d catalogs to %s\n", site.Total, len(site.Catalogs), dst)
}

func build(root string) (*Site, error) {
	examplesDir := filepath.Join(root, "examples")
	entries, err := parseReadme(filepath.Join(examplesDir, "README.md"))
	if err != nil {
		return nil, err
	}
	dirs, err := findExamples(examplesDir)
	if err != nil {
		return nil, err
	}

	var problems []string
	for _, e := range entries {
		if !slices.Contains(dirs, e.id) {
			problems = append(problems, fmt.Sprintf("README lists %s but examples/%s/main.go does not exist", e.id, e.id))
		}
	}
	listed := make(map[string]readmeEntry, len(entries))
	for _, e := range entries {
		listed[e.id] = e
	}

	byCatalog := make(map[string][]Example)
	for _, id := range dirs {
		entry, ok := listed[id]
		if !ok {
			problems = append(problems, fmt.Sprintf("examples/%s is missing from examples/README.md", id))
			continue
		}
		ex, err := loadExample(examplesDir, id, entry.description)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		byCatalog[ex.Catalog] = append(byCatalog[ex.Catalog], ex)
	}

	known := make(map[string]bool, len(catalogs))
	for _, c := range catalogs {
		known[c.id] = true
	}
	for id := range byCatalog {
		if !known[id] {
			problems = append(problems, fmt.Sprintf("catalog %q has no entry in pagegen's catalogs table", id))
		}
	}
	if len(problems) > 0 {
		slices.Sort(problems)
		return nil, errors.New(strings.Join(problems, "\n  "))
	}

	order := make(map[string]int, len(entries))
	for i, e := range entries {
		order[e.id] = i
	}
	site := &Site{}
	for _, c := range catalogs {
		exs := byCatalog[c.id]
		if len(exs) == 0 {
			continue
		}
		slices.SortFunc(exs, func(a, b Example) int { return order[a.ID] - order[b.ID] })
		site.Catalogs = append(site.Catalogs, Catalog{ID: c.id, Title: c.title, Blurb: c.blurb, Examples: exs})
		site.Total += len(exs)
	}
	return site, nil
}

type readmeEntry struct {
	id, description string
}

var readmeRow = regexp.MustCompile("^\\|.*?\\[`([^`]+)`\\]\\(([^)\\s]+)\\)")

// parseReadme returns the example table rows of examples/README.md in
// document order. The description is the row's last cell.
func parseReadme(file string) ([]readmeEntry, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var entries []readmeEntry
	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		m := readmeRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id := path.Clean(strings.TrimPrefix(m[2], "./"))
		if seen[id] {
			return nil, fmt.Errorf("examples/README.md lists %s twice", id)
		}
		seen[id] = true
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		entries = append(entries, readmeEntry{id: id, description: strings.TrimSpace(cells[len(cells)-1])})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("%s: no example table rows found", file)
	}
	return entries, nil
}

// findExamples returns slash-separated directories (relative to examplesDir)
// that contain a main.go, sorted.
func findExamples(examplesDir string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(examplesDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || d.Name() != "main.go" {
			return nil
		}
		rel, err := filepath.Rel(examplesDir, filepath.Dir(p))
		if err != nil {
			return err
		}
		dirs = append(dirs, filepath.ToSlash(rel))
		return nil
	})
	slices.Sort(dirs)
	return dirs, err
}

func loadExample(examplesDir, id, description string) (Example, error) {
	dir := filepath.Join(examplesDir, filepath.FromSlash(id))
	src, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		return Example{}, err
	}
	diagram, err := os.ReadFile(filepath.Join(dir, "flow.mmd"))
	if errors.Is(err, fs.ErrNotExist) {
		return Example{}, fmt.Errorf("examples/%s/flow.mmd is missing (every example needs a diagram)", id)
	} else if err != nil {
		return Example{}, err
	}
	diagramText := normalize(diagram)
	if err := checkDiagram(diagramText); err != nil {
		return Example{}, fmt.Errorf("examples/%s/flow.mmd: %w", id, err)
	}
	source := normalize(src)
	summary, kinds, err := inspectSource(source)
	if err != nil {
		return Example{}, fmt.Errorf("examples/%s/main.go: %w", id, err)
	}

	catalog, name := "basics", id
	if i := strings.IndexByte(id, '/'); i >= 0 {
		catalog, name = id[:i], id[i+1:]
	}
	return Example{
		ID:          id,
		Name:        name,
		Catalog:     catalog,
		Description: description,
		Summary:     summary,
		Kinds:       kinds,
		Run:         "go run ./examples/" + id,
		Source:      source,
		Diagram:     diagramText,
	}, nil
}

func normalize(b []byte) string {
	return strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") + "\n"
}

var diagramClassRef = regexp.MustCompile(`:::([A-Za-z0-9_-]+)`)

// checkDiagram catches the mistakes Mermaid would only report in the
// browser: a wrong diagram type, unknown node classes, and local classDefs
// that would fight the page's theme-aware styling.
func checkDiagram(d string) error {
	first, _, _ := strings.Cut(d, "\n")
	if !strings.HasPrefix(first, "flowchart ") {
		return fmt.Errorf("first line must be a flowchart declaration, got %q", first)
	}
	for _, line := range strings.Split(d, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "classDef") {
			return errors.New("classDef lines are not allowed; the page injects theme-aware classes")
		}
	}
	matches := diagramClassRef.FindAllStringSubmatch(d, -1)
	if len(matches) == 0 {
		return errors.New("no node uses a :::class")
	}
	for _, m := range matches {
		if !slices.Contains(diagramClasses, m[1]) {
			return fmt.Errorf("unknown node class %q (allowed: %s)", m[1], strings.Join(diagramClasses, ", "))
		}
	}
	return nil
}

// inspectSource extracts the package doc summary and counts typesafe
// question literals by kind.
func inspectSource(src string) (string, map[string]int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", src, parser.ParseComments)
	if err != nil {
		return "", nil, err
	}
	kinds := make(map[string]int)
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		sel, ok := lit.Type.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "typesafe" {
			if kind, ok := questionKinds[sel.Sel.Name]; ok {
				kinds[kind]++
			}
		}
		return true
	})
	var doc string
	if file.Doc != nil {
		doc = file.Doc.Text()
	}
	return summarize(doc), kinds, nil
}

// summarize returns the doc comment's first paragraph without the run
// instructions, with the conventional "Command name" prefix removed.
func summarize(doc string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(doc), "\n\n")
	first, _, _ = strings.Cut(strings.Join(strings.Fields(first), " "), "Requires TYPESAFE_API_KEY")
	s := strings.TrimSpace(first)
	if rest, ok := strings.CutPrefix(s, "Command "); ok {
		if _, after, ok := strings.Cut(rest, " "); ok {
			s = after
		}
	}
	if r, size := utf8.DecodeRuneInString(s); size > 0 {
		s = string(unicode.ToUpper(r)) + s[size:]
	}
	return s
}
