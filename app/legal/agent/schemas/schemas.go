// Package schemas holds the input JSON Schemas of the 26 Counsel agent tools in
// spec/eng/a2-interfaces.json and registers the tools through kernel/agenttools, so every
// call is validated against its schema before a handler runs.
package schemas

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// toolsJSON is the name, kind, capability and input schema of each a2 tool, copied from
// spec/eng/a2-interfaces.json. A test keeps the copy equal to the spec.
//
//go:embed tools.json
var toolsJSON []byte

// Count is the number of a2 agent tools.
const Count = 27

// ErrMissingHandler: Register was given no handler for a tool.
var ErrMissingHandler = errors.New("schemas: no handler for tool")

// ErrUnknownTool: no a2 tool has the name.
var ErrUnknownTool = errors.New("schemas: unknown tool")

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

// ByName returns the a2 definition of one tool, or ErrUnknownTool.
func ByName(name string) (Def, error) {
	defs, err := Defs()
	if err != nil {
		return Def{}, err
	}
	for _, d := range defs {
		if d.Name == name {
			return d, nil
		}
	}
	return Def{}, fmt.Errorf("schemas: %q: %w", name, ErrUnknownTool)
}

// Compile compiles the tool's input schema, the same one Register gives kernel/agenttools,
// so a handler that validates its own input checks against the a2 schema and no copy of it.
func (d Def) Compile() (*jsonschema.Schema, error) {
	c := jsonschema.NewCompiler()
	url := "tool://" + d.Name + "/input.json"
	if err := c.AddResource(url, bytes.NewReader(d.Input)); err != nil {
		return nil, fmt.Errorf("schemas: %q: %w", d.Name, err)
	}
	sch, err := c.Compile(url)
	if err != nil {
		return nil, fmt.Errorf("schemas: %q: %w", d.Name, err)
	}
	return sch, nil
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
