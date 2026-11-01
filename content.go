package typesafe

import (
	"encoding"
	"encoding/json"
	"errors"
	"math"
	"reflect"
)

// errRequestBodyCycle marks a cyclic request value. encoding/json rejects
// cyclic maps and slices with its own error, but a pointer-only cycle (an
// interface pointing at its own address) escapes that detector on this
// toolchain and overflows the stack, so the walk itself must catch every
// cycle before encoding.
var errRequestBodyCycle = errors.New("request body contains a cycle")

// assetContext threads the per-request asset state through the body walk.
type assetContext struct {
	// aliases maps Assets members by pointer identity; every placed
	// occurrence of a member serializes as its alias (Python keys by id).
	aliases map[*Image]string
	// found collects placed images serialized as their reference.
	found map[string]*Image
	// seen holds Map/Slice/Ptr identities currently on the walk stack.
	// Arrays are traversed but never keyed: reflect.Value.Pointer panics on
	// arrays, and an array cannot form a cycle without passing through a
	// keyed kind.
	seen map[uintptr]struct{}
	// replacements counts substitutions so containers are rebuilt only when a
	// descendant changed; untouched subtrees pass by reference to the encoder
	// and image-free bodies encode byte-identically to a body with no walk.
	replacements int
}

func newAssetContext(assets map[string]*Image) *assetContext {
	ctx := &assetContext{
		found: make(map[string]*Image),
		seen:  make(map[uintptr]struct{}),
	}
	if len(assets) > 0 {
		ctx.aliases = make(map[*Image]string, len(assets))
		for _, name := range sortedKeys(assets) {
			if asset := assets[name]; asset != nil {
				// One image under several aliases serializes as the last
				// alias in sorted order; Python's id-keyed comprehension also
				// keeps the final entry.
				ctx.aliases[asset] = name
			}
		}
	}
	return ctx
}

// rewriteContent walks a request-body value, substituting placed *Image
// values with their alias or reference, stringifying non-finite floats, and
// collecting the images it finds; it never mutates the caller's values.
// Reference text is never rewritten — the assets map is keyed by that same
// reference string. Values whose concrete type implements json.Marshaler or
// encoding.TextMarshaler (pointer types included) pass through untouched so
// their custom wire form is preserved; assets reachable only behind them
// serialize as uncollected references.
func rewriteContent(value any, ctx *assetContext) (any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case *Image:
		if typed == nil {
			// A typed nil encodes as JSON null in every position, including
			// as a present-but-null criteria outcome.
			return value, nil
		}
		ctx.replacements++
		if alias, ok := ctx.aliases[typed]; ok {
			return alias, nil
		}
		ctx.found[typed.reference] = typed
		return typed.reference, nil
	case Image:
		// A bare value would otherwise silently encode as {}.
		return nil, newTypeSafeError("Images must be passed as *Image.")
	case Noul:
		return rewriteNoul(typed, ctx)
	case Choice:
		return rewriteChoice(typed, ctx)
	case Score:
		return rewriteScore(typed, ctx)
	case Box:
		return rewriteBox(typed, ctx)
	case NoulCriteria:
		return rewriteNoulCriteriaValue(typed, ctx)
	case *NoulCriteria:
		if typed == nil {
			return value, nil
		}
		before := ctx.replacements
		walked, err := rewriteNoulCriteriaValue(*typed, ctx)
		if err != nil {
			return nil, err
		}
		if ctx.replacements == before {
			return value, nil
		}
		return walked, nil
	case RawQuestion:
		if typed == nil {
			return value, nil
		}
		return rewriteKeyed(reflect.ValueOf(map[string]any(typed)), value, ctx, func() (any, error) {
			return rewriteStringMap(map[string]any(typed), ctx)
		})
	case Questions:
		return rewriteQuestionMap(map[string]Question(typed), ctx)
	case map[string]Question:
		return rewriteQuestionMap(typed, ctx)
	case ChoiceCriteria:
		if typed == nil {
			return value, nil
		}
		return rewriteKeyed(reflect.ValueOf(map[string]any(typed)), value, ctx, func() (any, error) {
			return rewriteStringMap(map[string]any(typed), ctx)
		})
	case ScoreCriteria:
		if typed == nil {
			return value, nil
		}
		return rewriteKeyed(reflect.ValueOf([]any(typed)), value, ctx, func() (any, error) {
			return rewriteSlice([]any(typed), ctx)
		})
	case map[string]any:
		if typed == nil {
			return value, nil
		}
		return rewriteKeyed(reflect.ValueOf(typed), value, ctx, func() (any, error) {
			return rewriteStringMap(typed, ctx)
		})
	case []any:
		if typed == nil {
			return value, nil
		}
		return rewriteKeyed(reflect.ValueOf(typed), value, ctx, func() (any, error) {
			return rewriteSlice(typed, ctx)
		})
	case json.Marshaler, encoding.TextMarshaler:
		return value, nil
	case float64:
		return rewriteFloat(typed, value, ctx)
	case float32:
		return rewriteFloat(float64(typed), value, ctx)
	default:
		return rewriteReflected(value, ctx)
	}
}

// rewriteFloat stringifies non-finite floats — JSON has no NaN or Infinity
// tokens — and passes finite values through unchanged.
func rewriteFloat(value float64, original any, ctx *assetContext) (any, error) {
	var replacement string
	switch {
	case math.IsNaN(value):
		replacement = "NaN"
	case math.IsInf(value, 1):
		replacement = "Infinity"
	case math.IsInf(value, -1):
		replacement = "-Infinity"
	default:
		return original, nil
	}
	ctx.replacements++
	return replacement, nil
}

// rewriteNoul rebuilds the question with walked fields so its MarshalJSON
// keeps emitting "type" first; rebuilding as a map would let the encoder sort
// other keys ahead of it.
func rewriteNoul(q Noul, ctx *assetContext) (any, error) {
	before := ctx.replacements
	instructions, err := rewriteContent(q.Instructions, ctx)
	if err != nil {
		return nil, err
	}
	var criteria *NoulCriteria
	if q.Criteria != nil {
		walked, err := rewriteContent(q.Criteria, ctx)
		if err != nil {
			return nil, err
		}
		switch typed := walked.(type) {
		case *NoulCriteria:
			criteria = typed
		case NoulCriteria:
			criteria = &typed
		}
	}
	if ctx.replacements == before {
		return q, nil
	}
	return Noul{Instructions: instructions, Criteria: criteria}, nil
}

func rewriteChoice(q Choice, ctx *assetContext) (any, error) {
	before := ctx.replacements
	instructions, err := rewriteContent(q.Instructions, ctx)
	if err != nil {
		return nil, err
	}
	walked, err := rewriteContent(q.Criteria, ctx)
	if err != nil {
		return nil, err
	}
	if ctx.replacements == before {
		return q, nil
	}
	criteria, _ := walked.(ChoiceCriteria)
	if converted, ok := walked.(map[string]any); ok {
		criteria = ChoiceCriteria(converted)
	}
	return Choice{Instructions: instructions, Criteria: criteria}, nil
}

func rewriteScore(q Score, ctx *assetContext) (any, error) {
	before := ctx.replacements
	instructions, err := rewriteContent(q.Instructions, ctx)
	if err != nil {
		return nil, err
	}
	walked, err := rewriteContent(q.Criteria, ctx)
	if err != nil {
		return nil, err
	}
	if ctx.replacements == before {
		return q, nil
	}
	criteria, _ := walked.(ScoreCriteria)
	if converted, ok := walked.([]any); ok {
		criteria = ScoreCriteria(converted)
	}
	return Score{Instructions: instructions, Criteria: criteria}, nil
}

func rewriteBox(q Box, ctx *assetContext) (any, error) {
	before := ctx.replacements
	instructions, err := rewriteContent(q.Instructions, ctx)
	if err != nil {
		return nil, err
	}
	if ctx.replacements == before {
		return q, nil
	}
	return Box{Instructions: instructions}, nil
}

func rewriteNoulCriteriaValue(c NoulCriteria, ctx *assetContext) (any, error) {
	before := ctx.replacements
	rebuilt := NoulCriteria{True: c.True, False: c.False}
	if c.True != nil {
		walked, err := rewriteContent(c.True, ctx)
		if err != nil {
			return nil, err
		}
		rebuilt.True = walked
	}
	if c.False != nil {
		walked, err := rewriteContent(c.False, ctx)
		if err != nil {
			return nil, err
		}
		rebuilt.False = walked
	}
	if ctx.replacements == before {
		return c, nil
	}
	return rebuilt, nil
}

func rewriteQuestionMap(questions map[string]Question, ctx *assetContext) (any, error) {
	if questions == nil {
		return questions, nil
	}
	return rewriteKeyed(reflect.ValueOf(questions), questions, ctx, func() (any, error) {
		before := ctx.replacements
		walked := make(map[string]any, len(questions))
		for name, question := range questions {
			value, err := rewriteContent(question, ctx)
			if err != nil {
				return nil, err
			}
			walked[name] = value
		}
		if ctx.replacements == before {
			return questions, nil
		}
		return walked, nil
	})
}

func rewriteStringMap(entries map[string]any, ctx *assetContext) (any, error) {
	before := ctx.replacements
	walked := make(map[string]any, len(entries))
	for key, value := range entries {
		rewritten, err := rewriteContent(value, ctx)
		if err != nil {
			return nil, err
		}
		walked[key] = rewritten
	}
	if ctx.replacements == before {
		return entries, nil
	}
	return walked, nil
}

func rewriteSlice(items []any, ctx *assetContext) (any, error) {
	before := ctx.replacements
	walked := make([]any, len(items))
	for index, item := range items {
		rewritten, err := rewriteContent(item, ctx)
		if err != nil {
			return nil, err
		}
		walked[index] = rewritten
	}
	if ctx.replacements == before {
		return items, nil
	}
	return walked, nil
}

// rewriteKeyed runs walk under the cycle guard, keyed by the container's
// identity for the duration of its subtree.
func rewriteKeyed(reflected reflect.Value, original any, ctx *assetContext, walk func() (any, error)) (any, error) {
	key := reflected.Pointer()
	if _, seen := ctx.seen[key]; seen {
		return nil, errRequestBodyCycle
	}
	ctx.seen[key] = struct{}{}
	result, err := walk()
	delete(ctx.seen, key)
	return result, err
}

var stringType = reflect.TypeOf("")

// rewriteReflected walks container and pointer kinds the explicit arms do not
// cover. Nil maps, slices, and pointers pass through so explicit nested nulls
// stay null; structs and unsupported kinds pass through for the encoder to
// encode or reject exactly as before.
func rewriteReflected(value any, ctx *assetContext) (any, error) {
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Map:
		if reflected.IsNil() {
			return value, nil
		}
		if reflected.Type().Key() != stringType {
			// Non-string keys pass through for the encoder to reject, and
			// defined string keys may carry TextMarshaler formatting, which
			// a rebuild as map[string]any would lose.
			return value, nil
		}
		return rewriteKeyed(reflected, value, ctx, func() (any, error) {
			before := ctx.replacements
			walked := make(map[string]any, reflected.Len())
			iter := reflected.MapRange()
			for iter.Next() {
				rewritten, err := rewriteContent(iter.Value().Interface(), ctx)
				if err != nil {
					return nil, err
				}
				walked[iter.Key().String()] = rewritten
			}
			if ctx.replacements == before {
				return value, nil
			}
			return walked, nil
		})
	case reflect.Slice:
		if reflected.IsNil() {
			return value, nil
		}
		if _, isBytes := value.([]byte); isBytes {
			// Bytes encode as a base64 string; nothing inside them can be
			// substituted.
			return value, nil
		}
		return rewriteKeyed(reflected, value, ctx, func() (any, error) {
			before := ctx.replacements
			walked := make([]any, reflected.Len())
			for index := range walked {
				rewritten, err := rewriteContent(reflected.Index(index).Interface(), ctx)
				if err != nil {
					return nil, err
				}
				walked[index] = rewritten
			}
			if ctx.replacements == before {
				return value, nil
			}
			return walked, nil
		})
	case reflect.Array:
		before := ctx.replacements
		walked := make([]any, reflected.Len())
		for index := range walked {
			rewritten, err := rewriteContent(reflected.Index(index).Interface(), ctx)
			if err != nil {
				return nil, err
			}
			walked[index] = rewritten
		}
		if ctx.replacements == before {
			return value, nil
		}
		return walked, nil
	case reflect.Ptr:
		if reflected.IsNil() {
			return value, nil
		}
		return rewriteKeyed(reflected, value, ctx, func() (any, error) {
			// Returning the walked pointee in place of the pointer is
			// wire-safe: JSON(*T) is byte-identical to JSON(T). Struct
			// pointees pass through unchanged (struct fields are never
			// opened); custom marshalers never reach this arm.
			before := ctx.replacements
			walked, err := rewriteContent(reflected.Elem().Interface(), ctx)
			if err != nil {
				return nil, err
			}
			if ctx.replacements == before {
				return value, nil
			}
			return walked, nil
		})
	}
	return value, nil
}
