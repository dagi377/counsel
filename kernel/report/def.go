package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/dagi377/counsel/kernel/audit"
)

// idRe is a module-prefixed report id: "<module>.<name>".
var idRe = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*$`)

// entry is a registered definition with its compiled params schema (nil when it has none).
type entry struct {
	def    ReportDef
	schema *jsonschema.Schema
}

// Reports registers and runs reports. Runs are recorded in memory and in the audit log; the
// audit record holds the params hash and row count, never row data.
type Reports struct {
	now   func() time.Time
	auth  Authorizer
	audit audit.Log

	mu   sync.Mutex
	defs map[string]entry
	runs []Run
	seq  int

	// schedMu guards the schedules and their slots. RunSlot holds it across a run so two
	// workers on the same slot cannot both run it; it is never taken while mu is held.
	schedMu   sync.Mutex
	schedules map[string]Schedule
	slots     map[slotKey]RunID
}

var _ Engine = (*Reports)(nil)

// New returns an engine with no definitions. now stamps runs, auth checks capabilities and
// log receives one event per run.
func New(now func() time.Time, auth Authorizer, log audit.Log) *Reports {
	return &Reports{now: now, auth: auth, audit: log, defs: map[string]entry{},
		schedules: map[string]Schedule{}, slots: map[slotKey]RunID{}}
}

// Register adds a report definition. The id must be module-prefixed, the capability and Run
// set, and Params, when present, a valid JSON Schema.
func (e *Reports) Register(d ReportDef) error {
	if !idRe.MatchString(d.ID) || d.Capability == "" || d.Run == nil {
		return fmt.Errorf("register %q: %w", d.ID, ErrInvalid)
	}
	en := entry{def: d}
	if len(d.Params) > 0 {
		s, err := compileParams(d.ID, d.Params)
		if err != nil {
			return fmt.Errorf("register %q: %w", d.ID, err)
		}
		en.schema = s
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.defs[d.ID]; ok {
		return fmt.Errorf("register %q: %w", d.ID, ErrDuplicate)
	}
	e.defs[d.ID] = en
	return nil
}

// compileParams compiles a params JSON Schema (draft 2020-12).
func compileParams(id string, raw []byte) (*jsonschema.Schema, error) {
	if !json.Valid(raw) {
		return nil, fmt.Errorf("params schema is not JSON: %w", ErrInvalid)
	}
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	url := "mem:///report/" + id + "/params"
	if err := c.AddResource(url, bytes.NewReader(raw)); err != nil {
		return nil, fmt.Errorf("params schema: %v: %w", err, ErrInvalid)
	}
	s, err := c.Compile(url)
	if err != nil {
		return nil, fmt.Errorf("params schema: %v: %w", err, ErrInvalid)
	}
	return s, nil
}

// lookup returns the registered entry for id.
func (e *Reports) lookup(id string) (entry, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	en, ok := e.defs[id]
	return en, ok
}
