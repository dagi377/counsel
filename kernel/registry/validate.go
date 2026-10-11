package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

// Schema kinds: one spec/kernel/<kind>.schema.json each.
const (
	SchemaCapability     = "capability"
	SchemaDocumentKind   = "documentkind"
	SchemaGateKind       = "gatekind"
	SchemaPortalPage     = "portalpage"
	SchemaReport         = "report"
	SchemaRetentionClass = "retentionclass"
	SchemaRulePack       = "rulepack"
	SchemaTool           = "tool"
)

var schemaKinds = []string{
	SchemaCapability, SchemaDocumentKind, SchemaGateKind, SchemaPortalPage,
	SchemaReport, SchemaRetentionClass, SchemaRulePack, SchemaTool,
}

// Schemas holds the compiled spec/kernel schemas that registered data must satisfy.
type Schemas struct {
	byKind map[string]*jsonschema.Schema
}

// LoadSchemas compiles every <kind>.schema.json in fsys, which is rooted at spec/kernel
// (for example os.DirFS("spec/kernel")). A missing or broken schema is an error.
func LoadSchemas(fsys fs.FS) (*Schemas, error) {
	s := &Schemas{byKind: map[string]*jsonschema.Schema{}}
	for _, kind := range schemaKinds {
		name := kind + ".schema.json"
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("load schema %s: %w", name, err)
		}
		c := jsonschema.NewCompiler()
		c.Draft = jsonschema.Draft2020
		c.AssertFormat = true
		url := "file:///spec/kernel/" + name
		if err := c.AddResource(url, bytes.NewReader(raw)); err != nil {
			return nil, fmt.Errorf("add schema %s: %w", name, err)
		}
		sch, err := c.Compile(url)
		if err != nil {
			return nil, fmt.Errorf("compile schema %s: %w", name, err)
		}
		s.byKind[kind] = sch
	}
	return s, nil
}

// Boot is the kernel's Booter: it records every registration and validates it all on Seal
// against the spec/kernel schemas. It is safe for concurrent use.
type Boot struct {
	Recorder

	schemas *Schemas
	mu      sync.Mutex
	sealed  int // number of calls covered by the first Seal; -1 before it
}

var _ Booter = (*Boot)(nil)

// NewBoot returns a Boot that validates against schemas.
func NewBoot(schemas *Schemas) *Boot { return &Boot{schemas: schemas, sealed: -1} }

// Seal validates all registrations and freezes the registry. Every problem is returned,
// joined, each wrapping ErrDuplicateID, ErrKernelPrefix, ErrInvalid or ErrSealed. Calls
// made after the first Seal are reported as ErrSealed by every later Seal.
func (b *Boot) Seal() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	calls := b.Calls()
	if b.sealed >= 0 {
		var errs []error
		for _, c := range calls[b.sealed:] {
			errs = append(errs, fmt.Errorf("%s after boot: %w", c.Method, ErrSealed))
		}
		return errors.Join(errs...)
	}
	b.sealed = len(calls)
	return Validate(b.schemas, calls)
}

// Validate checks recorded calls: ids are unique per kind, no app id uses a kernel prefix,
// and data with a spec/kernel schema satisfies it. It returns every problem, joined.
func Validate(schemas *Schemas, calls []Call) error {
	if schemas == nil {
		return fmt.Errorf("validate: no schemas: %w", ErrInvalid)
	}
	v := &validator{schemas: schemas, seen: map[string]map[string]bool{}}
	for _, c := range calls {
		v.call(c)
	}
	return errors.Join(v.errs...)
}

type validator struct {
	schemas *Schemas
	seen    map[string]map[string]bool
	errs    []error
}

func (v *validator) fail(err error) { v.errs = append(v.errs, err) }

// id records one id of a kind and reports duplicates and kernel-reserved prefixes.
func (v *validator) id(kind, id string) {
	if v.seen[kind] == nil {
		v.seen[kind] = map[string]bool{}
	}
	if v.seen[kind][id] {
		v.fail(fmt.Errorf("%s %q: %w", kind, id, ErrDuplicateID))
	}
	v.seen[kind][id] = true
	for _, p := range KernelPrefixes {
		if strings.HasPrefix(id, p) {
			v.fail(fmt.Errorf("%s %q: %w", kind, id, ErrKernelPrefix))
		}
	}
}

// check validates doc against the schema of kind.
func (v *validator) check(kind, id string, doc any) {
	sch := v.schemas.byKind[kind]
	if sch == nil {
		v.fail(fmt.Errorf("%s %q: no schema loaded: %w", kind, id, ErrInvalid))
		return
	}
	if err := sch.Validate(doc); err != nil {
		v.fail(fmt.Errorf("%s %q: %v: %w", kind, id, err, ErrInvalid))
	}
}

// embedded decodes an embedded JSON Schema document and checks that it compiles.
func (v *validator) embedded(kind, id, field string, raw []byte) any {
	if len(raw) == 0 {
		return nil // the kind's schema reports the missing field
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		v.fail(fmt.Errorf("%s %q: %s is not JSON: %w", kind, id, field, ErrInvalid))
		return nil
	}
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	url := "mem:///" + kind + "/" + id + "/" + field
	if err := c.AddResource(url, bytes.NewReader(raw)); err != nil {
		v.fail(fmt.Errorf("%s %q: %s: %v: %w", kind, id, field, err, ErrInvalid))
		return doc
	}
	if _, err := c.Compile(url); err != nil {
		v.fail(fmt.Errorf("%s %q: %s is not a JSON Schema: %v: %w", kind, id, field, err, ErrInvalid))
	}
	return doc
}

func (v *validator) call(c Call) {
	switch c.Method {
	case MethodCapability:
		x := c.Args[0].(Capability)
		v.id(c.Method, x.ID)
		v.check(SchemaCapability, x.ID, map[string]any{"id": x.ID, "description": x.Description})
	case MethodRole:
		v.id(c.Method, c.Args[0].(Role).ID)
	case MethodSubjectKind:
		v.id(c.Method, c.Args[0].(SubjectKind).ID)
	case MethodGateKind:
		x := c.Args[0].(GateKind)
		v.id(c.Method, x.Slug)
		doc := map[string]any{
			"slug":          x.Slug,
			"approver":      x.Approver,
			"risk":          string(x.Risk),
			"min_approvers": x.MinApprovers,
			"ttl":           duration(x.TTL),
		}
		if x.CoolingOff != 0 {
			doc["cooling_off"] = duration(x.CoolingOff)
		}
		if p := v.embedded(SchemaGateKind, x.Slug, "payload_schema", x.PayloadSchema); p != nil {
			doc["payload_schema"] = p
		}
		v.check(SchemaGateKind, x.Slug, doc)
	case MethodEventType:
		name, _ := c.Args[0].(string)
		v.id(c.Method, name)
		if v.embedded("event", name, "schema", c.Args[1].([]byte)) == nil {
			v.fail(fmt.Errorf("event %q: schema missing: %w", name, ErrInvalid))
		}
	case MethodInboundProvider:
		v.id(c.Method, c.Args[0].(Provider).ID)
	case MethodRulePack:
		v.rulePack(c.Args[0].(fs.FS))
	case MethodDocumentKind:
		x := c.Args[0].(DocumentKind)
		v.id(c.Method, x.ID)
		v.check(SchemaDocumentKind, x.ID, map[string]any{
			"id": x.ID, "description": x.Description, "retention_class": x.RetentionClass,
		})
	case MethodRetentionClass:
		x := c.Args[0].(RetentionClass)
		v.id(c.Method, x.ID)
		doc := map[string]any{"id": x.ID, "min_years": x.MinYears, "never_purge": x.NeverPurge}
		if x.DestructionGate != "" {
			doc["destruction_gate"] = x.DestructionGate
		}
		v.check(SchemaRetentionClass, x.ID, doc)
	case MethodReport:
		x := c.Args[0].(ReportDef)
		v.id(c.Method, x.ID)
		doc := map[string]any{"id": x.ID, "capability": x.Capability}
		if p := v.embedded(SchemaReport, x.ID, "params", x.Params); p != nil {
			doc["params"] = p
		}
		v.check(SchemaReport, x.ID, doc)
	case MethodPortalPage:
		x := c.Args[0].(PortalPage)
		v.id(c.Method, x.ID)
		v.check(SchemaPortalPage, x.ID, map[string]any{"id": x.ID, "route": x.Route, "capability": x.Capability})
	case MethodTool:
		x := c.Args[0].(Tool)
		v.id(c.Method, x.Name)
		doc := map[string]any{"name": x.Name, "kind": string(x.Kind), "capability": x.Capability}
		if p := v.embedded(SchemaTool, x.Name, "input", x.Input); p != nil {
			doc["input"] = p
		}
		v.check(SchemaTool, x.Name, doc)
	case MethodChartOfAccounts:
		x := c.Args[0].(Chart)
		v.id(c.Method, x.Book)
		codes := map[string]bool{}
		for _, a := range x.Accounts {
			if codes[a.Code] {
				v.fail(fmt.Errorf("chart %q account %q: %w", x.Book, a.Code, ErrDuplicateID))
			}
			codes[a.Code] = true
		}
	case MethodNumberSeries, MethodSmokeCheck, MethodRestoreCheck:
		name, _ := c.Args[0].(string)
		v.id(c.Method, name)
	case MethodRoutes:
		// Routes carry no id or data to validate.
	}
}

// rulePack validates every .yaml, .yml or .json file in fsys as one rule pack.
func (v *validator) rulePack(fsys fs.FS) {
	if fsys == nil {
		v.fail(fmt.Errorf("rule pack: nil filesystem: %w", ErrInvalid))
		return
	}
	found := 0
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := path.Ext(p)
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return nil
		}
		found++
		raw, err := fs.ReadFile(fsys, p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		var y any
		if err := yaml.Unmarshal(raw, &y); err != nil {
			v.fail(fmt.Errorf("rule pack %s: %v: %w", p, err, ErrInvalid))
			return nil
		}
		// Round-trip through JSON so the validator sees JSON types.
		b, err := json.Marshal(normalize(y))
		if err != nil {
			v.fail(fmt.Errorf("rule pack %s: %v: %w", p, err, ErrInvalid))
			return nil
		}
		var doc any
		if err := json.Unmarshal(b, &doc); err != nil {
			v.fail(fmt.Errorf("rule pack %s: %v: %w", p, err, ErrInvalid))
			return nil
		}
		id := p
		if m, ok := doc.(map[string]any); ok {
			if name, ok := m["pack"].(string); ok {
				id = name
			}
		}
		v.id(MethodRulePack, id)
		v.check(SchemaRulePack, id, doc)
		return nil
	})
	if err != nil {
		v.fail(fmt.Errorf("rule pack: %v: %w", err, ErrInvalid))
	}
	if err == nil && found == 0 {
		v.fail(fmt.Errorf("rule pack: no .yaml or .json files: %w", ErrInvalid))
	}
}

// normalize turns YAML-only values into JSON ones: dates become YYYY-MM-DD strings.
func normalize(y any) any {
	switch x := y.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = normalize(e)
		}
	case []any:
		for i, e := range x {
			x[i] = normalize(e)
		}
	case time.Time:
		return x.Format(time.DateOnly)
	}
	return y
}

// duration formats d in the schema's Go-duration grammar with a single integer unit,
// e.g. 72h or 90m. A negative duration keeps its sign and so fails the schema.
func duration(d time.Duration) string {
	units := []struct {
		u    time.Duration
		name string
	}{{time.Hour, "h"}, {time.Minute, "m"}, {time.Second, "s"}, {time.Millisecond, "ms"}, {time.Microsecond, "us"}}
	for _, u := range units {
		if d%u.u == 0 {
			return strconv.FormatInt(int64(d/u.u), 10) + u.name
		}
	}
	return strconv.FormatInt(int64(d), 10) + "ns"
}
