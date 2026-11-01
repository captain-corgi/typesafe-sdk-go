package typesafe

import (
	"bytes"
	"fmt"
)

// part is one body part of a multipart/form-data request; name and filename
// must be ASCII without quotes, as they are written unescaped.
type part struct {
	name        string
	contentType string
	content     []byte
	// filename is empty when the part carries no filename parameter.
	filename string
}

// encodeMultipart encodes parts into a body, returning its content type and
// bytes. The boundary is 32 random lowercase hex characters, like the Python
// SDK's uuid4().hex; the framing follows RFC 7578 byte for byte.
func encodeMultipart(parts []part) (string, []byte) {
	boundary := randomHex(16)
	var body bytes.Buffer
	for _, p := range parts {
		// Dispositions are written unescaped, exactly like the Python SDK.
		disposition := `form-data; name="` + p.name + `"`
		if p.filename != "" {
			disposition += `; filename="` + p.filename + `"`
		}
		fmt.Fprintf(&body, "--%s\r\nContent-Disposition: %s\r\nContent-Type: %s\r\n\r\n", boundary, disposition, p.contentType)
		body.Write(p.content)
		body.WriteString("\r\n")
	}
	fmt.Fprintf(&body, "--%s--\r\n", boundary)
	return multipartContentType + boundary, body.Bytes()
}
