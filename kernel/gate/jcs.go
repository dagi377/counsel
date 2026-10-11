package gate

import (
	"bytes"
	"encoding/json"
)

// rule: ARC-I-015
// JCS returns the RFC 8785 canonical form of payload: object keys sorted by UTF-16 code
// unit, no insignificant whitespace, no HTML escaping and integers in plain decimal. A
// payload number with a fraction or exponent is ErrInvalidPayload, since amounts are
// integer cents and rates integer ppm. PayloadHash is the SHA-256 of these bytes.
func JCS(payload json.RawMessage) ([]byte, error) {
	var doc any
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, ErrInvalidPayload
	}
	var b bytes.Buffer
	if err := canonical(&b, doc); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
