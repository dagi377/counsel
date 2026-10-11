package gate

import (
	"encoding/json"
	"fmt"

	"github.com/dagi377/counsel/kernel/canon"
)

// rule: ARC-I-015
// JCS returns the RFC 8785 canonical form of payload (kernel/canon): object keys sorted by
// UTF-16 code unit, no insignificant whitespace, strings escaped only where RFC 8785
// requires. A gate payload number must be an integer literal within +-(2^53-1), since
// amounts are integer cents and rates integer ppm; its ES6 form is then its decimal form.
// Any other number is ErrInvalidPayload. PayloadHash is the SHA-256 of these bytes.
func JCS(payload json.RawMessage) ([]byte, error) {
	doc, err := decodeJSON(payload)
	if err != nil {
		return nil, err
	}
	return canonicalize(doc)
}

// decodeJSON decodes one JSON value, keeping numbers as their literal text.
func decodeJSON(payload json.RawMessage) (any, error) {
	doc, err := canon.Decode(payload)
	if err != nil {
		return nil, fmt.Errorf("payload: %w", ErrInvalidPayload)
	}
	return doc, nil
}

// canonicalize serialises a decoded JSON value per RFC 8785 with integer-only numbers.
func canonicalize(doc any) ([]byte, error) {
	b, err := canon.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("payload: %w: %w", ErrInvalidPayload, err)
	}
	return b, nil
}
