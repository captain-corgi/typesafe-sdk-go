package typesafe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// Answer is a typed answer to a single question, identified by its concrete
// type: [NoulAnswer], [ChoiceAnswer], or [ScoreAnswer]. Answer kinds this SDK
// version does not model are dropped from Answers (with a warning) but remain
// available on [SystemOneResponse.Raw].
type Answer interface {
	isAnswer()
}

// NoulAnswer is a yes/no answer: the probability of a yes or true statement,
// from 0 to 1.
type NoulAnswer struct {
	Noul float64
}

func (NoulAnswer) isAnswer() {}

// ChoiceAnswer is a selected label with its probabilities.
type ChoiceAnswer struct {
	Choice        string
	Confidence    float64
	Probabilities map[string]float64
}

func (ChoiceAnswer) isAnswer() {}

// ScoreAnswer is an expected score with its rubric and probabilities.
type ScoreAnswer struct {
	Score         float64
	Confidence    float64
	Legend        map[int]any
	Probabilities map[int]float64
}

func (ScoreAnswer) isAnswer() {}

// Usage holds the token counts for a request; a nil field means the API did
// not report that count.
type Usage struct {
	InputTokens  *int
	OutputTokens *int
}

// SystemOneResponse carries answers grouped by question type, with model and
// usage metadata. RequestID and Raw are attached to every response created by
// the client; Raw's body has been buffered, so it is safe to read.
type SystemOneResponse struct {
	Model   string
	Usage   Usage
	Answers map[string]Answer

	RequestID string
	Raw       *http.Response

	noulsOnce   sync.Once
	nouls       map[string]NoulAnswer
	choicesOnce sync.Once
	choices     map[string]ChoiceAnswer
	scoresOnce  sync.Once
	scores      map[string]ScoreAnswer
}

// Nouls returns the yes/no answers keyed by question name. The view is
// memoized; do not mutate the returned map.
func (r *SystemOneResponse) Nouls() map[string]NoulAnswer {
	r.noulsOnce.Do(func() {
		r.nouls = make(map[string]NoulAnswer)
		for name, answer := range r.Answers {
			if typed, ok := answer.(NoulAnswer); ok {
				r.nouls[name] = typed
			}
		}
	})
	return r.nouls
}

// Choices returns the choice answers keyed by question name. The view is
// memoized; do not mutate the returned map.
func (r *SystemOneResponse) Choices() map[string]ChoiceAnswer {
	r.choicesOnce.Do(func() {
		r.choices = make(map[string]ChoiceAnswer)
		for name, answer := range r.Answers {
			if typed, ok := answer.(ChoiceAnswer); ok {
				r.choices[name] = typed
			}
		}
	})
	return r.choices
}

// Scores returns the score answers keyed by question name. The view is
// memoized; do not mutate the returned map.
func (r *SystemOneResponse) Scores() map[string]ScoreAnswer {
	r.scoresOnce.Do(func() {
		r.scores = make(map[string]ScoreAnswer)
		for name, answer := range r.Answers {
			if typed, ok := answer.(ScoreAnswer); ok {
				r.scores[name] = typed
			}
		}
	})
	return r.scores
}

// ModelMetadata describes a single available model.
type ModelMetadata struct {
	Name        string
	Description string
	ReleaseDate string
}

// ListModelsResponse lists the models available to the account. RequestID and
// Raw are attached to every response created by the client; Raw's body has
// been buffered, so it is safe to read.
type ListModelsResponse struct {
	Models []ModelMetadata

	RequestID string
	Raw       *http.Response
}

// knownAnswerTypes are the answer discriminators this SDK models.
var knownAnswerTypes = map[string]struct{}{"noul": {}, "choice": {}, "score": {}}

// ParseSystemOneResponse decodes an HTTP response from POST /v1/systemone
// into a [SystemOneResponse] — the Go counterpart of the Python SDK's
// SystemOneResponse.from_http_response. The body is buffered and re-attached,
// so resp stays readable; a nil response is rejected. Non-2xx statuses map to
// the typed error taxonomy, and invalid 2xx bodies produce a
// *ResponseValidationError.
func ParseSystemOneResponse(resp *http.Response) (*SystemOneResponse, error) {
	if resp == nil {
		return nil, newTypeSafeError("ParseSystemOneResponse requires a non-nil *http.Response.")
	}
	body, err := bufferResponseBody(resp)
	if err != nil {
		return nil, err
	}
	if err := responseStatusError(resp, body); err != nil {
		return nil, err
	}
	return parseSystemOneResponse(resp, body)
}

// ParseListModelsResponse decodes an HTTP response from GET /v1/models into a
// [ListModelsResponse]; see [ParseSystemOneResponse] for the contract.
func ParseListModelsResponse(resp *http.Response) (*ListModelsResponse, error) {
	if resp == nil {
		return nil, newTypeSafeError("ParseListModelsResponse requires a non-nil *http.Response.")
	}
	body, err := bufferResponseBody(resp)
	if err != nil {
		return nil, err
	}
	if err := responseStatusError(resp, body); err != nil {
		return nil, err
	}
	return parseListModelsResponse(resp, body)
}

// bufferResponseBody drains resp, closes the original body, and re-attaches a
// buffered copy so the response stays readable. A nil Body (a hand-built
// response) reads as empty.
func bufferResponseBody(resp *http.Response) ([]byte, error) {
	if resp.Body == nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return nil, nil
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, newConnectionError(err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// responseStatusError maps a non-2xx response onto the typed error taxonomy.
func responseStatusError(resp *http.Response, body []byte) error {
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return apiErrorFor(resp.StatusCode, string(body), decodeJSONLenient(body), resp.Header, endpointFromResponse(resp))
	}
	return nil
}

// parseSystemOneResponse decodes a 2xx /v1/systemone body into a
// SystemOneResponse, attaching request metadata. Structurally invalid bodies
// produce a *ResponseValidationError with a dotted FieldPath.
func parseSystemOneResponse(resp *http.Response, body []byte) (*SystemOneResponse, error) {
	if err := validateSystemOneEnvelope(resp, body); err != nil {
		return nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, validationErrorFor(resp, body, "")
	}

	model, ok := decodeStringStrict(top["model"])
	if !ok {
		return nil, validationErrorFor(resp, body, "model")
	}
	usage, path := decodeUsage(top["usage"])
	if path != "" {
		return nil, validationErrorFor(resp, body, path)
	}

	answers := make(map[string]Answer)
	if rawAnswers, present := top["answers"]; present {
		if rawIsNull(rawAnswers) {
			return nil, validationErrorFor(resp, body, "answers")
		}
		var answerMap map[string]json.RawMessage
		if err := json.Unmarshal(rawAnswers, &answerMap); err != nil || answerMap == nil {
			return nil, validationErrorFor(resp, body, "answers")
		}
		for _, name := range objectKeys(rawAnswers, answerMap) {
			answer, path := decodeAnswer(answerMap[name])
			if path != "" {
				return nil, validationErrorFor(resp, body, "answers."+name+"."+path)
			}
			if answer != nil {
				answers[name] = answer
			}
		}
	}

	result := &SystemOneResponse{Model: model, Usage: usage, Answers: answers}
	if id, present := headerValue(resp.Header, requestIDHeader); present {
		result.RequestID = id
	}
	result.Raw = resp
	return result, nil
}

// validateSystemOneEnvelope mirrors the Python SDK's pre-validation pass:
// when answers is an object, each entry must name a known string type (else
// it errors or, for unrecognized types, is dropped with a warning) — and this
// happens before the top-level model/usage checks.
func validateSystemOneEnvelope(resp *http.Response, body []byte) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil || top == nil {
		return validationErrorFor(resp, body, "")
	}
	rawAnswers, present := top["answers"]
	if !present || rawIsNull(rawAnswers) {
		return nil
	}
	var answerMap map[string]json.RawMessage
	if err := json.Unmarshal(rawAnswers, &answerMap); err != nil || answerMap == nil {
		return nil // A non-object answers value is reported as "answers" later.
	}
	for _, name := range objectKeys(rawAnswers, answerMap) {
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(answerMap[name], &entry); err != nil || entry == nil {
			return validationErrorFor(resp, body, "answers."+name+".type")
		}
		kind, ok := decodeStringStrict(entry["type"])
		if !ok {
			return validationErrorFor(resp, body, "answers."+name+".type")
		}
		if _, known := knownAnswerTypes[kind]; !known {
			// Forward compatibility: ignore answer kinds this SDK version does not
			// model; the raw payload stays available on Raw.
			sdkLogger().Warn(fmt.Sprintf("Ignoring answer '%s' with unrecognized type '%s'", name, kind))
		}
	}
	return nil
}

// parseListModelsResponse decodes a 2xx /v1/models body into a
// ListModelsResponse, attaching request metadata.
func parseListModelsResponse(resp *http.Response, body []byte) (*ListModelsResponse, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil || top == nil {
		return nil, validationErrorFor(resp, body, "")
	}
	rawModels, present := top["models"]
	if !present || rawIsNull(rawModels) {
		return nil, validationErrorFor(resp, body, "models")
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(rawModels, &entries); err != nil {
		return nil, validationErrorFor(resp, body, "models")
	}
	models := make([]ModelMetadata, 0, len(entries))
	for index, entry := range entries {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(entry, &fields); err != nil || fields == nil {
			return nil, validationErrorFor(resp, body, "models["+strconv.Itoa(index)+"]")
		}
		var metadata ModelMetadata
		for _, field := range []struct {
			name   string
			target *string
		}{{"name", &metadata.Name}, {"description", &metadata.Description}, {"release_date", &metadata.ReleaseDate}} {
			value, ok := decodeStringStrict(fields[field.name])
			if !ok {
				return nil, validationErrorFor(resp, body, "models["+strconv.Itoa(index)+"]."+field.name)
			}
			*field.target = value
		}
		models = append(models, metadata)
	}
	result := &ListModelsResponse{Models: models}
	if id, present := headerValue(resp.Header, requestIDHeader); present {
		result.RequestID = id
	}
	result.Raw = resp
	return result, nil
}

// decodeAnswer decodes one answer object by its type discriminator. A nil
// Answer with an empty path means the answer was recognized but dropped as an
// unknown type (already logged).
func decodeAnswer(raw json.RawMessage) (Answer, string) {
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entry); err != nil || entry == nil {
		return nil, "type"
	}
	kind, ok := decodeStringStrict(entry["type"])
	if !ok {
		return nil, "type"
	}
	switch kind {
	case "noul":
		value, path := decodeNoulAnswer(entry)
		return value, path
	case "choice":
		value, path := decodeChoiceAnswer(entry)
		return value, path
	case "score":
		value, path := decodeScoreAnswer(entry)
		return value, path
	default:
		return nil, ""
	}
}

func decodeNoulAnswer(entry map[string]json.RawMessage) (Answer, string) {
	value, ok := decodeFloatStrict(entry["noul"])
	if !ok {
		return nil, "noul"
	}
	return NoulAnswer{Noul: value}, ""
}

func decodeChoiceAnswer(entry map[string]json.RawMessage) (Answer, string) {
	choice, ok := decodeStringStrict(entry["choice"])
	if !ok {
		return nil, "choice"
	}
	confidence, ok := decodeFloatStrict(entry["confidence"])
	if !ok {
		return nil, "confidence"
	}
	probabilities, badKey, bad := decodeStringFloatMap(entry["probabilities"])
	if bad {
		return nil, "probabilities"
	}
	if badKey != nil {
		return nil, "probabilities." + *badKey
	}
	return ChoiceAnswer{Choice: choice, Confidence: confidence, Probabilities: probabilities}, ""
}

func decodeScoreAnswer(entry map[string]json.RawMessage) (Answer, string) {
	score, ok := decodeFloatStrict(entry["score"])
	if !ok {
		return nil, "score"
	}
	confidence, ok := decodeFloatStrict(entry["confidence"])
	if !ok {
		return nil, "confidence"
	}
	legend, badKey, bad := decodeLegendMap(entry["legend"])
	if bad {
		return nil, "legend"
	}
	if badKey != nil {
		return nil, "legend." + *badKey
	}
	probabilities, badKey, bad := decodeIntFloatMap(entry["probabilities"])
	if bad {
		return nil, "probabilities"
	}
	if badKey != nil {
		return nil, "probabilities." + *badKey
	}
	return ScoreAnswer{Score: score, Confidence: confidence, Legend: legend, Probabilities: probabilities}, ""
}

// decodeUsage decodes the optional token counts. An empty path means success.
func decodeUsage(raw json.RawMessage) (Usage, string) {
	if raw == nil {
		return Usage{}, "usage"
	}
	if rawIsNull(raw) {
		return Usage{}, "usage"
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return Usage{}, "usage"
	}
	usage := Usage{}
	for _, field := range []struct {
		name   string
		target **int
	}{{"input_tokens", &usage.InputTokens}, {"output_tokens", &usage.OutputTokens}} {
		value, present := fields[field.name]
		if !present || rawIsNull(value) {
			continue
		}
		decoded, ok := decodeOptionalIntStrict(value)
		if !ok {
			return Usage{}, "usage." + field.name
		}
		*field.target = decoded
	}
	return usage, ""
}

// --- strict JSON field decoders -------------------------------------------

// rawIsNull reports whether a raw JSON value is the null literal.
func rawIsNull(raw json.RawMessage) bool {
	return raw != nil && bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// decodeStringStrict decodes a JSON string; null and non-strings fail.
func decodeStringStrict(raw json.RawMessage) (string, bool) {
	if raw == nil || rawIsNull(raw) {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

// isNumberLiteral reports whether the raw value is a JSON number token.
// A literal check is needed because json.Number is a string kind, so
// encoding/json would otherwise accept quoted strings as numbers.
func isNumberLiteral(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	first := trimmed[0]
	return first == '-' || first == '+' || first == '.' || (first >= '0' && first <= '9')
}

// decodeFloatStrict decodes a JSON number (integer literals included); null
// and non-numbers fail.
func decodeFloatStrict(raw json.RawMessage) (float64, bool) {
	if raw == nil || rawIsNull(raw) || !isNumberLiteral(raw) {
		return 0, false
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return 0, false
	}
	value, err := number.Float64()
	if err != nil {
		return 0, false
	}
	return value, true
}

// decodeOptionalIntStrict decodes a JSON integer literal; floats, strings,
// and null fail.
func decodeOptionalIntStrict(raw json.RawMessage) (*int, bool) {
	if !isNumberLiteral(raw) {
		return nil, false
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return nil, false
	}
	value, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil {
		return nil, false
	}
	converted := int(value)
	if int64(converted) != value {
		// The value exceeds the platform's int range (32-bit builds);
		// fail validation rather than silently wrapping.
		return nil, false
	}
	return &converted, true
}

// coerceIntKey coerces a map key to an integer like the Python SDK's lax
// key coercion: trimmed decimal integers, integral float spellings ("1.0",
// "2.0000"), and underscored literals ("1_0") all coerce; fractions,
// exponent forms, and non-decimal spellings fail. Values beyond 2^53 are
// rejected to keep the conversion exact on every platform.
func coerceIntKey(key string) (int, bool) {
	trimmed := strings.TrimSpace(key)
	if level, err := strconv.Atoi(trimmed); err == nil {
		return level, true
	}
	if strings.ContainsAny(trimmed, "eE") {
		return 0, false
	}
	value, err := parseDecimalFloat(trimmed)
	if err != nil || value != math.Trunc(value) || value > 1<<53 || value < -(1<<53) {
		return 0, false
	}
	exact := int64(value)
	if exact > math.MaxInt || exact < math.MinInt {
		// Beyond the platform's int range (32-bit builds): fail validation
		// rather than wrapping.
		return 0, false
	}
	return int(exact), true
}

// decodeStringFloatMap decodes an object of string→float. bad is set when the
// value was structurally invalid (missing, null, or not an object); otherwise
// a non-nil badKey names the first key, in document order, whose value was
// invalid (possibly the empty string, rendering a trailing-dot path like the
// Python SDK).
func decodeStringFloatMap(raw json.RawMessage) (m map[string]float64, badKey *string, bad bool) {
	if raw == nil || rawIsNull(raw) {
		return nil, nil, true
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil || entries == nil {
		return nil, nil, true
	}
	result := make(map[string]float64, len(entries))
	for _, key := range objectKeys(raw, entries) {
		value, ok := decodeFloatStrict(entries[key])
		if !ok {
			return nil, &key, false
		}
		result[key] = value
	}
	return result, nil, false
}

// decodeIntFloatMap decodes an object whose keys coerce to integers
// (stringified int keys, per the wire format; surrounding whitespace is
// tolerated like the Python SDK) and whose values are floats.
func decodeIntFloatMap(raw json.RawMessage) (m map[int]float64, badKey *string, bad bool) {
	if raw == nil || rawIsNull(raw) {
		return nil, nil, true
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil || entries == nil {
		return nil, nil, true
	}
	result := make(map[int]float64, len(entries))
	for _, key := range objectKeys(raw, entries) {
		level, keyOk := coerceIntKey(key)
		value, valueOk := decodeFloatStrict(entries[key])
		if !keyOk || !valueOk {
			return nil, &key, false
		}
		result[level] = value
	}
	return result, nil, false
}

// decodeLegendMap decodes a score legend: keys coerce to integers and values
// are text, objects, or arrays (nulls allowed nested inside them). A value of
// any other shape fails with the ".str" path suffix, mirroring the Python
// SDK's str|dict|list union error location.
func decodeLegendMap(raw json.RawMessage) (m map[int]any, badKey *string, bad bool) {
	if raw == nil || rawIsNull(raw) {
		return nil, nil, true
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil || entries == nil {
		return nil, nil, true
	}
	result := make(map[int]any, len(entries))
	for _, key := range objectKeys(raw, entries) {
		level, keyOk := coerceIntKey(key)
		if !keyOk {
			return nil, &key, false
		}
		var value any
		if err := json.Unmarshal(entries[key], &value); err != nil {
			return nil, &key, false
		}
		switch value.(type) {
		case string, map[string]any, []any:
			result[level] = value
		default:
			suffixed := key + ".str"
			return nil, &suffixed, false
		}
	}
	return result, nil, false
}

// objectKeys returns the keys of a raw JSON object in document order, so a
// decode error names the same entry the Python SDK (which preserves input
// order) would report first. entries is the same object decoded into a map;
// when token streaming fails it falls back to sorted order, which stays
// deterministic.
func objectKeys(raw json.RawMessage, entries map[string]json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		return sortedKeys(entries)
	}
	keys := make([]string, 0, len(entries))
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return sortedKeys(entries)
		}
		key, ok := token.(string)
		if !ok {
			return sortedKeys(entries)
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return sortedKeys(entries)
		}
		keys = append(keys, key)
	}
	if len(keys) != len(entries) { // duplicate keys collapsed by the map
		return sortedKeys(entries)
	}
	return keys
}

// sortedKeys returns map keys in sorted order so decode errors are
// deterministic.
func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
