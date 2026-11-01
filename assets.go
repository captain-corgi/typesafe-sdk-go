package typesafe

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// referenceRegex matches the reference of any image, `Image.String()`; the
// reference is the same in text and in JSON.
var referenceRegex = regexp.MustCompile(`<image:[0-9a-f]{32}>`)

// resolvedImage is an image with its data read and validated.
type resolvedImage struct {
	contentType string
	// data is a private copy; it is immutable after resolution.
	data []byte
}

// Image is an image asset backed by image file bytes or a path to an image
// file, created with [NewImageFromBytes], [NewImageFromFile], or
// [NewImageFromBase64]. Place an *Image anywhere in request content (state,
// question instructions or criteria) or reference it from any string through
// its unique reference ([Image.Reference] / [Image.String], interpolatable
// with fmt.Sprintf); the client sends the bytes as a multipart asset and the
// reference in its place. Images compare by identity, not content: two images
// built from identical bytes are distinct assets. An Image is loaded once, on
// first use, and the cached resolution is reused by every later request, so
// later changes to the source bytes or file are invisible.
type Image struct {
	// data holds []byte image bytes or a string file path; a str payload is
	// unrepresentable in Go, unlike Python.
	data      any
	reference string
	mu        sync.Mutex
	resolved  *resolvedImage
}

// Asset is an asset referenced from request content; currently only images
// are supported.
type Asset = Image

// randomHex returns n random bytes as lowercase hex (2n characters).
func randomHex(n int) string {
	buf := make([]byte, n)
	// crypto/rand.Read never fails on the supported platforms; a failure means
	// the process cannot generate randomness for anything else either.
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("typesafe: crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(buf)
}

// newReference mints a unique reference at construction: "<image:" plus 32
// lowercase hex characters plus ">" — 40 ASCII bytes, JSON-safe, identical in
// text and JSON.
func newReference() string {
	return "<image:" + randomHex(16) + ">"
}

// NewImageFromBytes creates an [Image] from raw image file bytes. The bytes
// are copied once, at first use, so the slice may be reused or mutated
// afterwards. No validation happens here; the data is validated when the
// image is first sent.
func NewImageFromBytes(data []byte) *Image {
	return &Image{data: data, reference: newReference()}
}

// NewImageFromFile creates an [Image] over a path to an image file. The file
// is read when the request is prepared, not now, so it may be created after
// this call but before the request.
func NewImageFromFile(name string) *Image {
	return &Image{data: name, reference: newReference()}
}

// NewImageFromBase64 creates an [Image] from base64-encoded image bytes. The
// text is decoded immediately; whitespace, such as the line breaks of wrapped
// base64, is ignored. It returns an error when the text is not non-empty
// ASCII base64 or does not decode.
func NewImageFromBase64(text string) (*Image, error) {
	if !isASCII(text) {
		return nil, newTypeSafeError("Image data must contain non-empty ASCII base64 text.")
	}
	var stripped strings.Builder
	stripped.Grow(len(text))
	for i := 0; i < len(text); i++ {
		switch c := text[i]; c {
		// Python's str.split() also treats the ASCII separators \x1c-\x1f as
		// whitespace, so wrapped base64 carrying them decodes there.
		case ' ', '\t', '\n', '\r', '\v', '\f', '\x1c', '\x1d', '\x1e', '\x1f':
		default:
			stripped.WriteByte(c)
		}
	}
	encoded := stripped.String()
	if encoded == "" {
		return nil, newTypeSafeError("Image data must contain non-empty ASCII base64 text.")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, newTypeSafeError("Image data must contain valid base64 text.")
	}
	return NewImageFromBytes(data), nil
}

func isASCII(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] > 0x7F {
			return false
		}
	}
	return true
}

// Reference returns the unique reference to this image, for using it inside
// text, such as an fmt.Sprintf verb.
func (i *Image) Reference() string {
	return i.reference
}

// String returns the unique reference to this image; fmt functions
// interpolate images as their reference.
func (i *Image) String() string {
	return i.reference
}

// MarshalJSON emits the image's reference, the context-free fallback used
// when an *Image is encoded outside the client's asset-aware body walk.
func (i *Image) MarshalJSON() ([]byte, error) {
	if i == nil {
		return []byte("null"), nil
	}
	return []byte(strconv.Quote(i.reference)), nil
}

// resolve reads and validates the image once; the bytes are copied at first
// use so a mutable source cannot change what later requests send.
func (i *Image) resolve() (resolvedImage, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.resolved != nil {
		return *i.resolved, nil
	}
	data, err := i.readData()
	if err != nil {
		return resolvedImage{}, err
	}
	if len(data) == 0 {
		return resolvedImage{}, newTypeSafeError("Image data is empty.")
	}
	resolved := resolvedImage{contentType: sniffContentType(data), data: data}
	i.resolved = &resolved
	return resolved, nil
}

func (i *Image) readData() ([]byte, error) {
	switch data := i.data.(type) {
	case []byte:
		return bytes.Clone(data), nil
	case string:
		content, err := os.ReadFile(data)
		if err != nil {
			// Python prints only the capitalized strerror; Go's *fs.PathError
			// wording is documented as a deviation.
			return nil, newTypeSafeError("Image file '%s' could not be read: %v.", data, err)
		}
		return content, nil
	default:
		// A zero-value Image holds no data; every constructor stores []byte
		// or string.
		return nil, newTypeSafeError("Image data is empty.")
	}
}

// sniffContentType labels an image part from its file signature only.
func sniffContentType(data []byte) string {
	if bytes.HasPrefix(data, []byte(pngSignature)) {
		return "image/png"
	}
	if bytes.HasPrefix(data, []byte(jpegSignature)) {
		return "image/jpeg"
	}
	return octetStreamContentType
}

// pendingAsset is one entry of a request's pending assets: an Assets alias
// or a reference found in the encoded body.
type pendingAsset struct {
	name  string
	image *Image
}

// resolvePendingAssets validates each pending entry in order and resolves
// each distinct image once, naming the failing alias in errors. Entries align
// with the input; duplicate images share their resolution.
func resolvePendingAssets(pending []pendingAsset) ([]resolvedImage, error) {
	resolved := make([]resolvedImage, len(pending))
	byImage := make(map[*Image]resolvedImage)
	for index, entry := range pending {
		if strings.TrimSpace(entry.name) == "" {
			return nil, newTypeSafeError("Asset aliases must be non-empty strings.")
		}
		if entry.image == nil {
			return nil, newTypeSafeError("Asset '%s' must be an asset.", entry.name)
		}
		if cached, ok := byImage[entry.image]; ok {
			resolved[index] = cached
			continue
		}
		image, err := entry.image.resolve()
		if err != nil {
			return nil, newTypeSafeError("Asset '%s': %v", entry.name, err)
		}
		byImage[entry.image] = image
		resolved[index] = image
	}
	return resolved, nil
}

// The image satisfies the JSON marshaler contract through the package.
var _ json.Marshaler = (*Image)(nil)
