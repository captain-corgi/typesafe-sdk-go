package typesafe

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// JSONContent is any value acceptable as request state, instructions, or
// criteria: a string, a JSON object (map[string]any), a JSON array ([]any),
// or any nested combination, with null allowed inside nested values.
type JSONContent = any

// JSONValue is any JSON value, used for the free-form ExtraBody fields.
type JSONValue = any

// marshalJSONCompact encodes a value as compact JSON without HTML escaping,
// mirroring the Python SDK's serializer.
func marshalJSONCompact(value any) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// decodeJSONLenient decodes a response body: empty input yields nil, invalid
// JSON decodes as UTF-8 text with replacement characters, and valid JSON
// decodes into map[string]any / []any / float64 / string / bool / nil. Bodies
// carrying invalid UTF-8 anywhere (including inside string literals, where
// encoding/json would otherwise substitute) are treated as invalid JSON so
// the raw text is preserved like the Python SDK.
func decodeJSONLenient(content []byte) any {
	if len(content) == 0 {
		return nil
	}
	if !utf8.Valid(content) {
		return replaceInvalidUTF8(content)
	}
	var value any
	if err := json.Unmarshal(content, &value); err != nil {
		return replaceInvalidUTF8(content)
	}
	return value
}

// replaceInvalidUTF8 renders a body as text with replacement characters.
func replaceInvalidUTF8(content []byte) string {
	return strings.ToValidUTF8(string(content), "\ufffd")
}
