package typesafe

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"testing/synctest"
	"time"
)

// wireEncode marshals a value and decodes it back for structural comparison.
func wireEncode(t *testing.T, value any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshaling %v: %v", value, err)
	}
	return decodeJSONMap(t, encoded)
}

func TestNormalizeQuestionsPreservesObjects(t *testing.T) {
	questions := Questions{
		"noul":   Noul{Instructions: "Spam?"},
		"choice": Choice{Instructions: "Tone?", Criteria: ChoiceCriteria{"calm": nil}},
		"score":  Score{Instructions: "Quality?", Criteria: ScoreCriteria{"bad", "good"}},
	}
	normalized, err := normalizeQuestions(questions)
	if err != nil {
		t.Fatalf("normalizeQuestions: %v", err)
	}
	got := wireEncode(t, map[string]Question(normalized))
	want := map[string]any{
		"noul":   map[string]any{"type": "noul", "instructions": "Spam?"},
		"choice": map[string]any{"type": "choice", "instructions": "Tone?", "criteria": map[string]any{"calm": nil}},
		"score":  map[string]any{"type": "score", "instructions": "Quality?", "criteria": []any{"bad", "good"}},
	}
	for name := range want {
		if got[name] == nil {
			t.Fatalf("question %q missing from %v", name, got)
		}
	}
	compareJSON(t, want, got)
}

func TestNormalizeQuestionsPreservesRawQuestions(t *testing.T) {
	raws := []RawQuestion{
		{"type": "noul", "instructions": "Spam?", "weight": 3, "criteria": map[string]any{"future": "kept"}},
		{"type": "future", "nested": map[string]any{"k": nil}},
		{"type": "score", "criteria": []any{"good"}, "weight": 3},
	}
	for _, raw := range raws {
		questions := Questions{"raw": raw, "typed": Noul{Instructions: "Spam?"}}
		normalized, err := normalizeQuestions(questions)
		if err != nil {
			t.Fatalf("normalizeQuestions(%v): %v", raw, err)
		}
		// The raw question passes through untouched, including unknown fields.
		got := wireEncode(t, map[string]Question(normalized))["raw"]
		want := wireEncode(t, map[string]any{"q": raw})["q"]
		compareJSON(t, want, got)
	}
}

func TestRawQuestionsRequireStructuralKeys(t *testing.T) {
	invalid := []RawQuestion{
		{},
		{"instructions": "Missing type"},
		{"type": ""},
		{"type": nil},
		{"type": 1},
		{"type": []any{"future"}},
		nil,
	}
	for _, raw := range invalid {
		questions := Questions{"invalid": raw}
		_, err := normalizeQuestions(questions)
		if err == nil {
			t.Fatalf("normalizeQuestions(%v) should fail", raw)
		}
		var typeSafeErr *TypeSafeError
		if !errors.As(err, &typeSafeErr) {
			t.Fatalf("error should be *TypeSafeError, got %T", err)
		}
		if want := `Question "invalid" must be a question object or a dictionary with a nonempty string "type".`; typeSafeErr.Error() != want {
			t.Errorf("Error() = %q, want %q", typeSafeErr.Error(), want)
		}
	}
}

func TestRawChoiceAndScoreRequireCriteria(t *testing.T) {
	_, err := normalizeQuestions(Questions{"q": RawQuestion{"type": "choice"}})
	if err == nil || err.Error() != `Question "q" requires "criteria".` {
		t.Errorf("choice without criteria: %v", err)
	}
	_, err = normalizeQuestions(Questions{"q": RawQuestion{"type": "score"}})
	if err == nil || err.Error() != `Question "q" requires "criteria".` {
		t.Errorf("score without criteria: %v", err)
	}
}

func TestEmptyQuestionsRejected(t *testing.T) {
	_, err := normalizeQuestions(Questions{})
	if err == nil || err.Error() != "At least one question is required." {
		t.Errorf("empty questions: %v", err)
	}
	_, err = normalizeQuestions(nil)
	if err == nil || err.Error() != "At least one question is required." {
		t.Errorf("nil questions: %v", err)
	}
}

func TestEmptyScoreCriteriaRejected(t *testing.T) {
	// Typed form.
	_, err := normalizeQuestions(Questions{"rating": Score{Instructions: "Quality?", Criteria: ScoreCriteria{}}})
	if err == nil || err.Error() != `Score question "rating" has no criteria; at least one score is required.` {
		t.Errorf("typed empty score criteria: %v", err)
	}
	// Nil typed criteria is equally empty.
	_, err = normalizeQuestions(Questions{"rating": Score{Instructions: "Quality?"}})
	if err == nil || err.Error() != `Score question "rating" has no criteria; at least one score is required.` {
		t.Errorf("nil typed score criteria: %v", err)
	}
	// Raw form.
	_, err = normalizeQuestions(Questions{"rating": RawQuestion{"type": "score", "instructions": "Quality?", "criteria": []any{}}})
	if err == nil || err.Error() != `Score question "rating" has no criteria; at least one score is required.` {
		t.Errorf("raw empty score criteria: %v", err)
	}
}

func TestRawScoreFalsyCriteriaRejected(t *testing.T) {
	// Python gates raw score criteria on truthiness, so falsy scalars fail too.
	for name, criteria := range map[string]any{
		"zero":         0,
		"zero float":   0.0,
		"false":        false,
		"empty string": "",
	} {
		_, err := normalizeQuestions(Questions{"rating": RawQuestion{"type": "score", "criteria": criteria}})
		if err == nil || err.Error() != `Score question "rating" has no criteria; at least one score is required.` {
			t.Errorf("%s criteria: %v", name, err)
		}
	}
	for name, criteria := range map[string]any{
		"one":       1,
		"true":      true,
		"non-empty": "0-2",
		"non-zero":  0.5,
		"negative":  -1,
	} {
		if _, err := normalizeQuestions(Questions{"rating": RawQuestion{"type": "score", "criteria": criteria}}); err != nil {
			t.Errorf("%s criteria should pass: %v", name, err)
		}
	}
}

// TestRawScoreEmptyGoContainersRejected: Python's truthiness check rejects
// empty score criteria of any container type, so ordinary Go containers
// (typed slices, maps, arrays, named strings and booleans — including typed
// nils) must fail validation before any network I/O, not just the decoded
// []any / map[string]any forms.
func TestRawScoreEmptyGoContainersRejected(t *testing.T) {
	type namedString string
	type namedBool bool
	type namedFloat float64
	for name, criteria := range map[string]any{
		"empty string slice":     []string{},
		"empty typed slice":      ScoreCriteria{},
		"nil string slice":       []string(nil),
		"nil typed slice":        ScoreCriteria(nil),
		"empty string map":       map[string]string{},
		"empty typed map":        ChoiceCriteria{},
		"nil string map":         map[string]string(nil),
		"empty array":            [0]int{},
		"nil slice pointer":      (*[]any)(nil),
		"named empty string":     namedString(""),
		"named false":            namedBool(false),
		"named zero float":       namedFloat(0),
		"pointer to empty slice": &[]any{},
	} {
		_, err := normalizeQuestions(Questions{"rating": RawQuestion{"type": "score", "criteria": criteria}})
		if err == nil {
			t.Fatalf("%s criteria should be rejected as empty", name)
		}
		var typeSafeErr *TypeSafeError
		if !errors.As(err, &typeSafeErr) {
			t.Fatalf("%s: error should be *TypeSafeError, got %T", name, err)
		}
		if want := `Score question "rating" has no criteria; at least one score is required.`; typeSafeErr.Error() != want {
			t.Errorf("%s: Error() = %q, want %q", name, typeSafeErr.Error(), want)
		}
	}
}

// TestRawScoreNonEmptyGoContainersPassThrough: nonempty Go containers pass
// validation and reach the wire with their values preserved.
func TestRawScoreNonEmptyGoContainersPassThrough(t *testing.T) {
	type namedString string
	type namedBool bool
	for name, criteria := range map[string]any{
		"string slice":  []string{"bad", "good"},
		"typed slice":   ScoreCriteria{"good"},
		"string map":    map[string]string{"0": "bad"},
		"named string":  namedString("0-2"),
		"named true":    namedBool(true),
		"nested values": []map[string]any{{"summary": "bad"}},
	} {
		normalized, err := normalizeQuestions(Questions{"rating": RawQuestion{"type": "score", "criteria": criteria}})
		if err != nil {
			t.Fatalf("%s criteria should pass: %v", name, err)
		}
		got := wireEncode(t, map[string]Question(normalized))["rating"]
		want := wireEncode(t, map[string]any{"q": RawQuestion{"type": "score", "criteria": criteria}})["q"]
		compareJSON(t, want, got)
	}
}

// TestRawScoreEmptyContainerNeverReachesNetwork: a raw score question whose
// criteria is an ordinary empty Go container fails before any network I/O.
func TestRawScoreEmptyContainerNeverReachesNetwork(t *testing.T) {
	attempts := 0
	client := newLoggedOutClient(t, "https://unreachable.invalid")
	client.httpClient = &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		return textResponse(req, http.StatusOK, resultBody), nil
	})}
	_, err := client.SystemOne(t.Context(), &SystemOneParams{
		State:     "x",
		Questions: Questions{"rating": RawQuestion{"type": "score", "criteria": []string{}}},
	})
	var typeSafeErr *TypeSafeError
	if !errors.As(err, &typeSafeErr) {
		t.Fatalf("expected *TypeSafeError, got %T: %v", err, err)
	}
	if attempts != 0 {
		t.Errorf("empty container criteria reached the network (%d attempts)", attempts)
	}
}

// Run potentially cyclic input in a subprocess so a regression cannot leak a
// spinning goroutine or hang the test suite.
func TestRawScoreCriteriaPointerCycles(t *testing.T) {
	const childFlag = "TYPESAFE_TEST_CRITERIA_CYCLES"
	if os.Getenv(childFlag) != "1" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, executable, "-test.run=^TestRawScoreCriteriaPointerCycles$")
		cmd.Env = append(os.Environ(), childFlag+"=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("cycle validation failed (context: %v): %v\n%s", ctx.Err(), err, output)
		}
		return
	}

	var self, first, second any
	self = &self
	first, second = &second, &first
	client := newDeterministicClient(t, func(req *http.Request) (*http.Response, error) {
		t.Fatal("cyclic criteria reached the transport")
		return nil, nil
	})
	for name, criteria := range map[string]any{"self": self, "multi-node": first} {
		for _, canceled := range []bool{false, true} {
			ctx, cancel := context.WithCancel(t.Context())
			if canceled {
				cancel()
			}
			_, err := client.SystemOne(ctx, &SystemOneParams{
				State: "x", Questions: Questions{"q": RawQuestion{"type": "score", "criteria": criteria}},
			})
			cancel()
			var sdkErr *TypeSafeError
			if !errors.As(err, &sdkErr) || err.Error() != `Score question "q" has cyclic criteria.` {
				t.Fatalf("%s, canceled=%t: expected cyclic-criteria error, got %v", name, canceled, err)
			}
		}
	}
}

type customCriteriaSlice []string

func (customCriteriaSlice) MarshalJSON() ([]byte, error) { return []byte(`["bad","good"]`), nil }

type customCriteriaMap map[string]string

func (customCriteriaMap) MarshalJSON() ([]byte, error) { return []byte(`["bad","good"]`), nil }

type customCriteriaScalar int

func (customCriteriaScalar) MarshalJSON() ([]byte, error) { return []byte(`["bad","good"]`), nil }

type customCriteriaText string

func (customCriteriaText) MarshalText() ([]byte, error) { return []byte("custom rubric"), nil }

type countedCriteria int

func (c *countedCriteria) MarshalJSON() ([]byte, error) {
	*c++
	if *c > 1 {
		return nil, errors.New("marshaler called more than once")
	}
	return []byte(`["bad","good"]`), nil
}

type failingCriteria []string

func (failingCriteria) MarshalJSON() ([]byte, error) { return nil, errors.New("cannot encode rubric") }

func TestRawScoreCustomEncodingPreserved(t *testing.T) {
	var counted countedCriteria
	var asInterface any = &counted
	for name, tt := range map[string]struct {
		criteria any
		want     any
	}{
		"empty slice":      {customCriteriaSlice{}, []any{"bad", "good"}},
		"nil slice":        {customCriteriaSlice(nil), []any{"bad", "good"}},
		"empty map":        {customCriteriaMap{}, []any{"bad", "good"}},
		"nil map":          {customCriteriaMap(nil), []any{"bad", "good"}},
		"zero scalar":      {customCriteriaScalar(0), []any{"bad", "good"}},
		"text marshaler":   {customCriteriaText(""), "custom rubric"},
		"pointer receiver": {&asInterface, []any{"bad", "good"}},
		"plain pointer":    {&[]string{"good"}, []any{"good"}},
	} {
		t.Run(name, func(t *testing.T) {
			// The 503 on the first attempt triggers a real backoff, so this
			// runs on the bubble clock instead of sleeping for 500ms.
			synctest.Test(t, func(t *testing.T) {
				attempts := 0
				client := newDeterministicClient(t, func(req *http.Request) (*http.Response, error) {
					attempts++
					body := decodeJSONMap(t, readAll(req.Body))
					question := body["questions"].(map[string]any)["q"].(map[string]any)
					compareJSON(t, tt.want, question["criteria"])
					if question["custom"] != "preserved" {
						t.Fatal("raw question field lost")
					}
					if attempts == 1 {
						return textResponse(req, http.StatusServiceUnavailable, `{}`), nil
					}
					return textResponse(req, http.StatusOK, resultBody), nil
				})
				_, err := client.SystemOne(t.Context(), &SystemOneParams{
					State: "x", Questions: Questions{"q": RawQuestion{"type": "score", "criteria": tt.criteria, "custom": "preserved"}},
				})
				if err != nil || attempts != 2 {
					t.Fatalf("attempts=%d, error=%v", attempts, err)
				}
			})
		})
	}
	if counted != 1 {
		t.Fatalf("pointer marshaler called %d times, want once across retries", counted)
	}
}

func TestRawScoreCustomEncodingFailures(t *testing.T) {
	for name, tt := range map[string]struct {
		criteria any
		message  string
	}{
		"marshaler error": {failingCriteria{}, "The request body could not be encoded as JSON"},
		"nil pointer":     {(*countedCriteria)(nil), `Score question "q" has no criteria; at least one score is required.`},
	} {
		t.Run(name, func(t *testing.T) {
			client := newDeterministicClient(t, func(req *http.Request) (*http.Response, error) {
				t.Fatal("invalid criteria reached transport")
				return nil, nil
			})
			_, err := client.SystemOne(t.Context(), &SystemOneParams{
				State: "x", Questions: Questions{"q": RawQuestion{"type": "score", "criteria": tt.criteria}},
			})
			var sdkErr *TypeSafeError
			if !errors.As(err, &sdkErr) || err.Error() != tt.message {
				t.Fatalf("expected %q, got %v", tt.message, err)
			}
		})
	}
}

func TestTypedChoiceRequiresCriteria(t *testing.T) {
	_, err := normalizeQuestions(Questions{"q": Choice{Instructions: "Tone?"}})
	if err == nil || err.Error() != `Question "q" requires "criteria".` {
		t.Errorf("typed choice without criteria: %v", err)
	}
	// Since the v0.7.3 Python sync, an explicitly empty (non-nil) criteria
	// map is rejected too.
	_, err = normalizeQuestions(Questions{"q": Choice{Criteria: ChoiceCriteria{}}})
	if err == nil || err.Error() != `Choice question "q" has no criteria; at least one choice is required.` {
		t.Errorf("typed choice with empty criteria: %v", err)
	}
}

func TestWireEncodingOmitsOnlyNilFields(t *testing.T) {
	tests := []struct {
		name     string
		question Question
		want     map[string]any
	}{
		{"bare noul", Noul{}, map[string]any{"type": "noul"}},
		{"choice", Choice{Criteria: ChoiceCriteria{"a": nil}},
			map[string]any{"type": "choice", "criteria": map[string]any{"a": nil}}},
		{"score", Score{Criteria: ScoreCriteria{"good"}},
			map[string]any{"type": "score", "criteria": []any{"good"}}},
		{"empty instructions and criteria are kept",
			Noul{Instructions: "", Criteria: &NoulCriteria{}},
			map[string]any{"type": "noul", "instructions": "", "criteria": map[string]any{}}},
		{"empty array instructions kept",
			Noul{Instructions: []any{}, Criteria: &NoulCriteria{}},
			map[string]any{"type": "noul", "instructions": []any{}, "criteria": map[string]any{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compareJSON(t, tt.want, wireEncode(t, tt.question))
		})
	}
}

func TestDiscriminatorTagsEmitFirst(t *testing.T) {
	noul := Noul{Instructions: "Spam?"}
	choice := Choice{Instructions: "Tone?", Criteria: ChoiceCriteria{"calm": nil}}
	score := Score{Instructions: "Quality?", Criteria: ScoreCriteria{"good"}}
	for _, question := range []struct {
		value Question
		tag   string
	}{{noul, "noul"}, {choice, "choice"}, {score, "score"}} {
		encoded, err := json.Marshal(question.value)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		// The tag both leads the wire form and identifies the question.
		if got := wireEncode(t, question.value)["type"]; got != question.tag {
			t.Errorf("tag = %v, want %v", got, question.tag)
		}
		if want := `{"type":"` + question.tag + `"`; string(encoded[:len(want)]) != want {
			t.Errorf("wire form should start with %q, got %s", want, encoded)
		}
	}
}

func TestOptionalNoulCriteriaForms(t *testing.T) {
	tests := []struct {
		criteria *NoulCriteria
		want     any
	}{
		{nil, nil},
		{&NoulCriteria{}, map[string]any{}},
		{&NoulCriteria{True: "Yes"}, map[string]any{"true": "Yes"}},
		{&NoulCriteria{False: "No"}, map[string]any{"false": "No"}},
		{&NoulCriteria{True: "Yes", False: "No"}, map[string]any{"true": "Yes", "false": "No"}},
		{&NoulCriteria{True: map[string]any{"summary": "Unsolicited", "examples": []any{"Buy now"}}},
			map[string]any{"true": map[string]any{"summary": "Unsolicited", "examples": []any{"Buy now"}}}},
	}
	for _, tt := range tests {
		question := Noul{Instructions: "Spam?", Criteria: tt.criteria}
		got := wireEncode(t, question)
		if tt.want == nil {
			if _, present := got["criteria"]; present {
				t.Errorf("criteria %+v should be omitted", tt.criteria)
			}
			continue
		}
		compareJSON(t, tt.want, got["criteria"])
	}
}

// TestNoulCriteriaExplicitNullNeedsRaw: a typed nil outcome omits the key —
// Python's Noul(criteria={"true": None}) emits {"true": null} instead, which
// Go expresses only through RawQuestion. Both wire forms are documented.
func TestNoulCriteriaExplicitNullNeedsRaw(t *testing.T) {
	typed := wireEncode(t, Noul{Instructions: "Spam?", Criteria: &NoulCriteria{True: nil, False: "No"}})
	if _, present := typed["criteria"].(map[string]any)["true"]; present {
		t.Error("typed nil True should omit the key (documented deviation)")
	}
	raw := wireEncode(t, RawQuestion{
		"type":     "noul",
		"criteria": map[string]any{"true": nil, "false": "No"},
	})
	compareJSON(t, map[string]any{"true": nil, "false": "No"}, raw["criteria"])
}

func TestNestedExplicitNullsPreserved(t *testing.T) {
	// Nulls nested inside criteria values survive the wire form.
	choice := Choice{
		Instructions: map[string]any{"task": nil, "topic": "Tone?"},
		Criteria:     ChoiceCriteria{"calm": map[string]any{"note": nil}, "angry": []any{nil, "hostile"}},
	}
	got := wireEncode(t, choice)
	compareJSON(t, map[string]any{"task": nil, "topic": "Tone?"}, got["instructions"])
	compareJSON(t, map[string]any{"calm": map[string]any{"note": nil}, "angry": []any{nil, "hostile"}}, got["criteria"])

	score := Score{Criteria: ScoreCriteria{nil, "ok"}}
	compareJSON(t, []any{nil, "ok"}, wireEncode(t, score)["criteria"])
}

// --- shared comparison helpers ----------------------------------------------

func compareJSON(t *testing.T, want, got any) {
	t.Helper()
	w, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	g, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}
	if string(w) != string(g) {
		t.Errorf("mismatch:\n want %s\n  got %s", w, g)
	}
}

func TestBoxWire(t *testing.T) {
	compareJSON(t, map[string]any{"type": "box"}, wireEncode(t, Box{}))
	compareJSON(t, map[string]any{"type": "box", "instructions": "The dog"}, wireEncode(t, Box{Instructions: "The dog"}))
	if _, present := wireEncode(t, Box{})["instructions"]; present {
		t.Error("nil instructions must be omitted")
	}
	encoded, err := json.Marshal(Box{Instructions: "The dog"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := `{"type":"box"`; string(encoded[:len(want)]) != want {
		t.Errorf("wire form should start with %q, got %s", want, encoded)
	}
	if _, err := normalizeQuestions(Questions{"q": Box{}}); err != nil {
		t.Errorf("bare Box must pass validation: %v", err)
	}
}

func TestRawBoxPassthrough(t *testing.T) {
	// A raw box question gets only the generic raw-question validation.
	raw := RawQuestion{"type": "box"}
	if _, err := normalizeQuestions(Questions{"q": raw}); err != nil {
		t.Fatalf("raw box: %v", err)
	}
	if _, err := normalizeQuestions(Questions{"q": RawQuestion{"type": "box", "instructions": "The dog", "future": 1}}); err != nil {
		t.Fatalf("raw box with extra fields: %v", err)
	}
}

func TestValidationChoiceEmpty(t *testing.T) {
	tests := []struct {
		name     string
		question Question
	}{
		{"typed empty criteria", Choice{Criteria: ChoiceCriteria{}}},
		{"raw empty criteria", RawQuestion{"type": "choice", "criteria": map[string]any{}}},
		{"raw null criteria", RawQuestion{"type": "choice", "criteria": nil}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeQuestions(Questions{"q": tt.question})
			want := `Choice question "q" has no criteria; at least one choice is required.`
			if err == nil || err.Error() != want {
				t.Fatalf("error = %v, want %q", err, want)
			}
		})
	}
}

func TestValidationNoul(t *testing.T) {
	tests := []struct {
		name     string
		question Question
		want     string
	}{
		{"typed without either", Noul{}, `Noul question "q" has neither instructions nor criteria; at least one is required.`},
		{"typed with empty criteria", Noul{Criteria: &NoulCriteria{}}, `Noul question "q" has neither instructions nor criteria; at least one is required.`},
		{"raw without either", RawQuestion{"type": "noul"}, `Noul question "q" has neither instructions nor criteria; at least one is required.`},
		{"raw empty criteria", RawQuestion{"type": "noul", "criteria": map[string]any{}}, `Noul question "q" has neither instructions nor criteria; at least one is required.`},
		{"raw null-valued criteria", RawQuestion{"type": "noul", "criteria": map[string]any{"true": nil}}, `Noul question "q" has neither instructions nor criteria; at least one is required.`},
		{"raw criteria not an object", RawQuestion{"type": "noul", "criteria": "x"}, `Noul question "q" criteria must be an object.`},
		{"raw criteria is a list", RawQuestion{"type": "noul", "criteria": []any{"x"}}, `Noul question "q" criteria must be an object.`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := normalizeQuestions(Questions{"q": tt.question})
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
	for _, question := range []Question{
		Noul{Instructions: "?"},
		Noul{Criteria: &NoulCriteria{True: "yes"}},
		RawQuestion{"type": "noul", "instructions": "?"},
		RawQuestion{"type": "noul", "criteria": map[string]any{"true": "yes"}},
	} {
		if _, err := normalizeQuestions(Questions{"q": question}); err != nil {
			t.Errorf("valid noul rejected: %v", err)
		}
	}
}

func TestValidationStateRequired(t *testing.T) {
	attempts := 0
	client := newDeterministicClient(t, func(req *http.Request) (*http.Response, error) {
		attempts++
		return jsonResponse(http.StatusOK, resultBody), nil
	})
	_, err := client.SystemOne(t.Context(), &SystemOneParams{
		State:     nil,
		Questions: Questions{"q": Noul{Instructions: "?"}},
	})
	if err == nil || err.Error() != "State is required." {
		t.Fatalf("error = %v, want %q", err, "State is required.")
	}
	// State is checked before the questions, mirroring the Python SDK.
	_, err = client.SystemOne(t.Context(), &SystemOneParams{
		State:     nil,
		Questions: Questions{},
	})
	if err == nil || err.Error() != "State is required." {
		t.Fatalf("error = %v, want the state error before the questions error, got %v", err, err)
	}
	if attempts != 0 {
		t.Errorf("stateless request reached the network (%d attempts)", attempts)
	}
}

func TestValidationBeforeNetwork(t *testing.T) {
	tests := []struct {
		name    string
		params  *SystemOneParams
		message string
	}{
		{
			"empty questions",
			&SystemOneParams{State: "x"},
			"At least one question is required.",
		},
		{
			"raw question without type",
			&SystemOneParams{State: "x", Questions: Questions{"q": RawQuestion{"instructions": "?"}}},
			`Question "q" must be a question object or a dictionary with a nonempty string "type".`,
		},
		{
			"raw choice missing criteria",
			&SystemOneParams{State: "x", Questions: Questions{"q": RawQuestion{"type": "choice"}}},
			`Question "q" requires "criteria".`,
		},
		{
			"raw score missing criteria",
			&SystemOneParams{State: "x", Questions: Questions{"q": RawQuestion{"type": "score"}}},
			`Question "q" requires "criteria".`,
		},
		{
			"empty score criteria",
			&SystemOneParams{State: "x", Questions: Questions{"q": Score{}}},
			`Score question "q" has no criteria; at least one score is required.`,
		},
		{
			"empty choice criteria",
			&SystemOneParams{State: "x", Questions: Questions{"q": Choice{Criteria: ChoiceCriteria{}}}},
			`Choice question "q" has no criteria; at least one choice is required.`,
		},
		{
			"noul criteria not an object",
			&SystemOneParams{State: "x", Questions: Questions{"q": RawQuestion{"type": "noul", "criteria": "x"}}},
			`Noul question "q" criteria must be an object.`,
		},
		{
			"noul with neither",
			&SystemOneParams{State: "x", Questions: Questions{"q": Noul{}}},
			`Noul question "q" has neither instructions nor criteria; at least one is required.`,
		},
		{
			"state required",
			&SystemOneParams{Questions: Questions{"q": Noul{Instructions: "?"}}},
			"State is required.",
		},
		{
			"negative timeout",
			&SystemOneParams{State: "x", Questions: Questions{"q": Noul{Instructions: "?"}}, Timeout: -time.Second},
			"timeout must be a positive, finite number of seconds.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempts := 0
			client := newDeterministicClient(t, func(req *http.Request) (*http.Response, error) {
				attempts++
				return jsonResponse(http.StatusOK, resultBody), nil
			})
			_, err := client.SystemOne(t.Context(), tt.params)
			var sdkErr *TypeSafeError
			if !errors.As(err, &sdkErr) || err.Error() != tt.message {
				t.Fatalf("error = %v, want %q", err, tt.message)
			}
			if attempts != 0 {
				t.Errorf("invalid request reached the network (%d attempts)", attempts)
			}
		})
	}
}
