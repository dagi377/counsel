// Package schemas embeds a byte-identical copy of spec/kernel/*.schema.json, so the
// registry validates registrations wherever the binary runs. embed_test.go fails when the
// copy drifts from spec/kernel; refresh it by copying the spec files here.
package schemas

import "embed"

// FS holds the registry schemas, one <kind>.schema.json per registry kind, at its root.
//
//go:embed *.schema.json
var FS embed.FS
