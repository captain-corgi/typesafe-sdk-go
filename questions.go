package typesafe

import (
	"bytes"
	"encoding"
	"encoding/json"
	"reflect"
)

// Question is a typed or raw question about the request state. The zero
// interface is implemented only by [Noul], [Choice], [Score], and
// [RawQuestion]; the concrete type determines the statically-known answer
// type.
type Question interface {
	isQuestion()
}

// Questions maps question names (chosen by the caller, echoed by the answers)
// to their questions. The map must be non-empty.
type Questions map[string]Question

// NoulCriteria optionally describes the yes and no outcomes of a [Noul]
// question. A nil field omits the key from the wire form; values may be any
// JSON content with null allowed inside nested values. An explicit JSON null
// for an outcome (rather than omitting it) is not expressible with this typed
// struct — use a [RawQuestion] for that wire form.
type NoulCriteria struct {
	True  JSONContent
	False JSONContent
}

// MarshalJSON emits only the set criteria keys, preserving nested values
// verbatim.
func (c NoulCriteria) MarshalJSON() ([]byte, error) {
	buf := bytes.NewBufferString("{")
	first := true
	appendField := func(name string, value JSONContent) error {
		encoded, err := marshalJSONCompact(value)
		if err != nil {
			return err
		}
		if !first {
			buf.WriteString(",")
		}
		first = false
		buf.WriteString(`"`)
		buf.WriteString(name)
		buf.WriteString(`":`)
		buf.Write(encoded)
		return nil
	}
	if c.True != nil {
		if err := appendField("true", c.True); err != nil {
			return nil, err
		}
	}
	if c.False != nil {
		if err := appendField("false", c.False); err != nil {
			return nil, err
		}
	}
	buf.WriteString("}")
	return buf.Bytes(), nil
}

// Noul is a yes/no question answered by [NoulAnswer] with a probability of
// true. Instructions and criteria are optional; a nil Instructions is omitted
// from the wire form.
type Noul struct {
	Instructions JSONContent
	Criteria     *NoulCriteria
}

func (Noul) isQuestion() {}

// MarshalJSON emits the wire form: "type" first, then the optional
// "instructions" and "criteria" when set.
func (q Noul) MarshalJSON() ([]byte, error) {
	buf := bytes.NewBufferString(`{"type":"noul"`)
	if q.Instructions != nil {
		encoded, err := marshalJSONCompact(q.Instructions)
		if err != nil {
			return nil, err
		}
		buf.WriteString(`,"instructions":`)
		buf.Write(encoded)
	}
	if q.Criteria != nil {
		encoded, err := marshalJSONCompact(q.Criteria)
		if err != nil {
			return nil, err
		}
		buf.WriteString(`,"criteria":`)
		buf.Write(encoded)
	}
	buf.WriteString("}")
	return buf.Bytes(), nil
}

// ChoiceCriteria maps labels to their descriptions (text, object, or array),
// or nil for undescribed labels. The map is required and may be empty.
type ChoiceCriteria map[string]JSONContent

// Choice is a classification question answered by [ChoiceAnswer] with the
// selected label, a confidence, and per-label probabilities. Criteria is
// required; a nil Instructions is omitted from the wire form.
type Choice struct {
	Instructions JSONContent
	Criteria     ChoiceCriteria
}

func (Choice) isQuestion() {}

// MarshalJSON emits the wire form: "type" first, then "instructions" when
// set, then the required "criteria".
func (q Choice) MarshalJSON() ([]byte, error) {
	buf := bytes.NewBufferString(`{"type":"choice"`)
	if q.Instructions != nil {
		encoded, err := marshalJSONCompact(q.Instructions)
		if err != nil {
			return nil, err
		}
		buf.WriteString(`,"instructions":`)
		buf.Write(encoded)
	}
	buf.WriteString(`,"criteria":`)
	if q.Criteria == nil {
		buf.WriteString("null")
	} else if encoded, err := marshalJSONCompact(map[string]JSONContent(q.Criteria)); err == nil {
		buf.Write(encoded)
	} else {
		return nil, err
	}
	buf.WriteString("}")
	return buf.Bytes(), nil
}

// ScoreCriteria is the ordered, non-empty rubric for a [Score] question: one
// entry (text, object, or array) per score level, starting at zero.
type ScoreCriteria []JSONContent

// Score is a rubric-scoring question answered by [ScoreAnswer] with the
// expected score, a confidence, a legend, and per-level probabilities.
// Criteria is required and non-empty; a nil Instructions is omitted from the
// wire form.
type Score struct {
	Instructions JSONContent
	Criteria     ScoreCriteria
}

func (Score) isQuestion() {}

// MarshalJSON emits the wire form: "type" first, then "instructions" when
// set, then the required "criteria".
func (q Score) MarshalJSON() ([]byte, error) {
	buf := bytes.NewBufferString(`{"type":"score"`)
	if q.Instructions != nil {
		encoded, err := marshalJSONCompact(q.Instructions)
		if err != nil {
			return nil, err
		}
		buf.WriteString(`,"instructions":`)
		buf.Write(encoded)
	}
	buf.WriteString(`,"criteria":`)
	if q.Criteria == nil {
		buf.WriteString("null")
	} else if encoded, err := marshalJSONCompact([]JSONContent(q.Criteria)); err == nil {
		buf.Write(encoded)
	} else {
		return nil, err
	}
	buf.WriteString("}")
	return buf.Bytes(), nil
}

// RawQuestion is a wire passthrough for question shapes this SDK does not
// model: it is sent to the API untouched (unknown fields included), and the
// API is the schema validator. The client only checks the minimal shape: a
// non-empty string "type", plus "criteria" for choice and score questions.
type RawQuestion map[string]any

func (RawQuestion) isQuestion() {}

// answerType reports the question's wire discriminator, or "" for a raw
// question whose type is missing or not a non-empty string.
func (q RawQuestion) answerType() string {
	value, ok := q["type"].(string)
	if !ok || value == "" {
		return ""
	}
	return value
}

// normalizeQuestions validates questions before any network I/O and returns a
// shallow copy for the wire body. It raises a *TypeSafeError when the map is
// empty, a raw question lacks a non-empty string "type", a raw choice/score
// question lacks "criteria", or score criteria (typed or raw) is empty.
func normalizeQuestions(questions Questions) (map[string]Question, error) {
	if len(questions) == 0 {
		return nil, newTypeSafeError("At least one question is required.")
	}
	normalized := make(map[string]Question, len(questions))
	for name, question := range questions {
		switch typed := question.(type) {
		case Noul:
			// Nothing to validate: both fields are optional.
		case Choice:
			if typed.Criteria == nil {
				return nil, newTypeSafeError("Question %q requires %q.", name, "criteria")
			}
		case Score:
			if err := validateScoreCriteria(name, len(typed.Criteria)); err != nil {
				return nil, err
			}
		case RawQuestion:
			kind := typed.answerType()
			if kind == "" {
				return nil, newTypeSafeError("Question %q must be a question object or a dictionary with a nonempty string %q.", name, "type")
			}
			if kind == "choice" || kind == "score" {
				if _, ok := typed["criteria"]; !ok {
					return nil, newTypeSafeError("Question %q requires %q.", name, "criteria")
				}
				if kind == "score" {
					length, err := rawCriteriaLength(name, typed["criteria"])
					if err != nil {
						return nil, err
					}
					if err := validateScoreCriteria(name, length); err != nil {
						return nil, err
					}
				}
			}
		default:
			return nil, newTypeSafeError("Question %q must be a question object or a dictionary with a nonempty string %q.", name, "type")
		}
		normalized[name] = question
	}
	return normalized, nil
}

// validateScoreCriteria rejects a score question with no criteria; at least
// one score is required.
func validateScoreCriteria(name string, length int) error {
	if length == 0 {
		return newTypeSafeError("Score question %q has no criteria; at least one score is required.", name)
	}
	return nil
}

// rawCriteriaLength reports the element/key/character count of a raw score
// question's criteria value, mirroring Python truthiness: nil, false, zero
// numbers, empty containers, and empty strings are falsy; any other scalar is
// truthy. Custom JSON/text marshalers are left for encoding and API validation:
// their underlying Go value need not describe their wire representation, and
// validation must not invoke a stateful marshaler before the body is encoded.
// Pointer cycles are rejected before encoding. Other unsupported values are
// left for the encoder to reject; user values are never rewritten.
func rawCriteriaLength(name string, criteria any) (int, error) {
	value := reflect.ValueOf(criteria)
	var seen map[any]struct{}
	for value.IsValid() {
		if (value.Kind() == reflect.Ptr || value.Kind() == reflect.Interface) && value.IsNil() {
			return 0, nil // nil pointers encode as null, even with MarshalJSON
		}
		if value.CanInterface() {
			switch value.Interface().(type) {
			case json.Marshaler, encoding.TextMarshaler:
				return 1, nil
			}
		}
		if value.Kind() == reflect.Ptr {
			pointer := value.Interface() // pointers are comparable, including their type
			if _, exists := seen[pointer]; exists {
				return 0, newTypeSafeError("Score question %q has cyclic criteria.", name)
			}
			if seen == nil {
				seen = make(map[any]struct{})
			}
			seen[pointer] = struct{}{}
		} else if value.Kind() != reflect.Interface {
			break
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return 0, nil
	}
	switch value.Kind() {
	case reflect.Slice, reflect.Map, reflect.Array, reflect.String:
		return value.Len(), nil
	case reflect.Bool:
		if value.Bool() {
			return 1, nil
		}
		return 0, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if value.Int() != 0 {
			return 1, nil
		}
		return 0, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if value.Uint() != 0 {
			return 1, nil
		}
		return 0, nil
	case reflect.Float32, reflect.Float64:
		if value.Float() != 0 {
			return 1, nil
		}
		return 0, nil
	}
	return 1, nil
}

// The typed questions satisfy json.Marshaler through the package.
var (
	_ json.Marshaler = Noul{}
	_ json.Marshaler = NoulCriteria{}
	_ json.Marshaler = Choice{}
	_ json.Marshaler = Score{}
)
