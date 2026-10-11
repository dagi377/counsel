// Package schemas holds the input JSON Schemas of the 25 Counsel agent tools in
// spec/eng/a2-interfaces.json and registers the tools through kernel/agenttools, so every
// call is validated against its schema before a handler runs.
package schemas

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// toolsJSON is the name, kind, capability and input schema of each a2 tool, copied from
// spec/eng/a2-interfaces.json. A test keeps the copy equal to the spec.
//
//go:embed tools.json
var toolsJSON []byte

// Count is the number of a2 agent tools.
const Count = 25

// ErrMissingHandler: Register was given no handler for a tool.
var ErrMissingHandler = errors.New("schemas: no handler for tool")

// Handler runs one tool after its input passed the schema.
type Handler = func(ctx context.Context, p registry.Principal, in json.RawMessage) (registry.ToolResult, error)

// Def is one agent tool's definition.
type Def struct {
	Name       string            `json:"name"`
	Kind       registry.ToolKind `json:"kind"`
	Capability string            `json:"capability"`
	Input      json.RawMessage   `json:"input_schema"`
}

// Defs returns the definitions of all a2 agent tools, in spec order.
func Defs() ([]Def, error) {
	var defs []Def
	if err := json.Unmarshal(toolsJSON, &defs); err != nil {
		return nil, fmt.Errorf("schemas: decode tools: %w", err)
	}
	return defs, nil
}

// Register registers every a2 tool with tools, each with its handler from handlers. It
// fails with ErrMissingHandler if any tool has no handler, so no tool is left unregistered
// or registered without schema validation.
func Register(tools agenttools.Tools, handlers map[string]Handler) error {
	defs, err := Defs()
	if err != nil {
		return err
	}
	for _, d := range defs {
		h := handlers[d.Name]
		if h == nil {
			return fmt.Errorf("register %q: %w", d.Name, ErrMissingHandler)
		}
		t := registry.Tool{Name: d.Name, Kind: d.Kind, Input: []byte(d.Input), Capability: d.Capability, Handle: h}
		if err := tools.Register(t); err != nil {
			return fmt.Errorf("register %q: %w", d.Name, err)
		}
	}
	return nil
}
