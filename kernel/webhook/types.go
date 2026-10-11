package webhook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// typeName is a module-prefixed event type, e.g. "work.item_created".
var typeName = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// errBadType means an event type's name or schema is malformed.
var errBadType = errors.New("webhook: malformed event type")

// Types holds the event types modules register at boot. Only a registered type can be
// emitted, and its payload must match the type's JSON Schema. Safe for concurrent use.
type Types struct {
	mu     sync.RWMutex
	byName map[string]*jsonschema.Schema
}

// NewTypes returns an empty registry.
func NewTypes() *Types {
	return &Types{byName: map[string]*jsonschema.Schema{}}
}

// Register adds t. The name must be module-prefixed and the schema must compile.
func (r *Types) Register(t EventType) error {
	if !typeName.MatchString(t.Name) {
		return fmt.Errorf("register type %q: %w", t.Name, errBadType)
	}
	s, err := compile(t)
	if err != nil {
		return fmt.Errorf("register type %s: %w: %v", t.Name, errBadType, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byName[t.Name]; ok {
		return fmt.Errorf("register type %s: %w", t.Name, ErrDuplicateType)
	}
	r.byName[t.Name] = s
	return nil
}

// Check returns nil when m may be emitted: its type is registered and its payload matches
// the type's schema. Errors name the type only, never payload values.
func (r *Types) Check(m Message) error {
	r.mu.RLock()
	s, ok := r.byName[m.Type]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("emit %q: %w", m.Type, ErrUnknownType)
	}
	var v any
	if err := json.Unmarshal(m.Payload, &v); err != nil {
		return fmt.Errorf("emit %s: %w", m.Type, ErrInvalidPayload)
	}
	if err := s.Validate(v); err != nil {
		return fmt.Errorf("emit %s: %w", m.Type, ErrInvalidPayload)
	}
	return nil
}

// Names returns the registered type names, sorted.
func (r *Types) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.byName))
	for n := range r.byName {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func compile(t EventType) (*jsonschema.Schema, error) {
	if len(t.Schema) == 0 {
		return nil, errors.New("empty schema")
	}
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	url := "mem://webhook/" + t.Name + ".json"
	if err := c.AddResource(url, bytes.NewReader(t.Schema)); err != nil {
		return nil, err
	}
	return c.Compile(url)
}
