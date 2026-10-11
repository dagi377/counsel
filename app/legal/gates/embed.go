package gates

import _ "embed"

// specData is a byte-for-byte copy of spec/gates.json. go:embed cannot reach spec/ from
// app/legal, so the copy lives here and TestSpecDataMatchesSpec keeps it in step.
//
//go:embed kinds.json
var specData []byte

// SpecData returns the embedded spec/gates.json, the input to ParseKinds and RegisterKinds.
// The caller gets its own copy.
func SpecData() []byte {
	return append([]byte(nil), specData...)
}
