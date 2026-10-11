package gate

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

// rule: ARC-I-013
// kindSlug is the shape of a gate kind: "<module>.<slug>". A spec id such as "G2" or
// "G-B2" never matches, so it can not be used as a key.
var kindSlug = regexp.MustCompile(`^[a-z]+\.[a-z_]+$`)

// Bounds on a kind, matching gate.gate_kind in migrations/0001_init.sql.
const (
	minTTL       = time.Hour
	maxTTL       = 720 * time.Hour
	maxApprovers = 3
)

// Kinds holds the gate kinds registered at boot. It is safe for concurrent use.
type Kinds struct {
	mu      sync.RWMutex
	modules []string // when set, the only module prefixes a kind may use
	kinds   map[string]GateKind
	schemas map[string]*jsonschema.Schema
	labels  map[string]string // slug -> spec id (label only, never a key)
}

// NewKinds returns an empty set of kinds. When modules is not empty, a kind's prefix must
// be one of them (the app's module names).
func NewKinds(modules ...string) *Kinds {
	return &Kinds{
		modules: slices.Clone(modules),
		kinds:   map[string]GateKind{},
		schemas: map[string]*jsonschema.Schema{},
		labels:  map[string]string{},
	}
}

// Module returns the module prefix of a kind slug ("billing" for "billing.write_off").
func Module(slug string) string {
	m, _, _ := strings.Cut(slug, ".")
	return m
}

// ValidateKind checks a kind as module would register it. It does not check duplicates.
func ValidateKind(module string, k GateKind) error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }
	// rule: ARC-I-013
	if !kindSlug.MatchString(k.Slug) {
		bad("slug %q does not match <module>.<slug>", k.Slug)
	} else if Module(k.Slug) != module {
		bad("slug %q is not prefixed by registering module %q", k.Slug, module)
	}
	for _, p := range registry.KernelPrefixes {
		if strings.HasPrefix(k.Slug, p) {
			bad("slug %q uses kernel prefix %q", k.Slug, p)
		}
	}
	if k.Approver == "" {
		bad("approver capability is required")
	}
	if k.Effect == nil {
		bad("effect is required")
	}
	switch k.Risk {
	case RiskLow, RiskMedium, RiskHigh, RiskIrreversible:
	default:
		bad("risk %q is not low, medium, high or irreversible", k.Risk)
	}
	// rule: ARC-I-019
	if k.MinApprovers < 1 || k.MinApprovers > maxApprovers {
		bad("min approvers %d is not between 1 and %d", k.MinApprovers, maxApprovers)
	}
	// rule: ARC-I-019
	if k.Risk == RiskIrreversible && k.MinApprovers < 2 {
		bad("irreversible kind needs at least 2 approvers, has %d", k.MinApprovers)
	}
	if k.TTL < minTTL || k.TTL > maxTTL || k.TTL%time.Hour != 0 {
		bad("ttl %s is not a whole number of hours between 1h and 720h", k.TTL)
	}
	if k.CoolingOff < 0 {
		bad("cooling-off %s is negative", k.CoolingOff)
	}
	if len(k.PayloadSchema) == 0 {
		bad("payload schema is required")
	} else if _, err := compileSchema(k.Slug, k.PayloadSchema); err != nil {
		bad("payload schema: %v", err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("gate kind %q: %w: %w", k.Slug, ErrInvalidKind, errors.Join(errs...))
	}
	return nil
}

func compileSchema(slug string, schema []byte) (*jsonschema.Schema, error) {
	c := jsonschema.NewCompiler()
	url := "mem://gate/" + slug + ".json"
	if err := c.AddResource(url, bytes.NewReader(schema)); err != nil {
		return nil, fmt.Errorf("add schema: %w", err)
	}
	s, err := c.Compile(url)
	if err != nil {
		return nil, fmt.Errorf("compile schema: %w", err)
	}
	return s, nil
}

// Register adds kind k on behalf of module. The slug prefix must be module.
func (ks *Kinds) Register(module string, k GateKind) error {
	if err := ValidateKind(module, k); err != nil {
		return err
	}
	// rule: ARC-I-013
	if len(ks.modules) > 0 && !slices.Contains(ks.modules, module) {
		return fmt.Errorf("gate kind %q: module %q is not a known module: %w", k.Slug, module, ErrInvalidKind)
	}
	s, err := compileSchema(k.Slug, k.PayloadSchema)
	if err != nil {
		return fmt.Errorf("gate kind %q: %w: %w", k.Slug, ErrInvalidKind, err)
	}
	ks.mu.Lock()
	defer ks.mu.Unlock()
	if _, ok := ks.kinds[k.Slug]; ok {
		return fmt.Errorf("gate kind %q: %w", k.Slug, ErrDuplicateKind)
	}
	ks.kinds[k.Slug] = k
	ks.schemas[k.Slug] = s
	return nil
}

// rule: ARC-I-013
// specLabel is the shape of a spec gate id (G2, G-B2, G11t), stored as a label only.
var specLabel = regexp.MustCompile(`^G(-?[A-Z0-9]+)+[a-z]?$`)

// Label records the app's spec id for a registered kind. It is stored in
// gate.approval_request.spec_gate for traceability and is never used as a key.
func (ks *Kinds) Label(slug, specID string) error {
	if !specLabel.MatchString(specID) {
		return fmt.Errorf("gate kind %q: spec id %q: %w", slug, specID, ErrInvalidKind)
	}
	ks.mu.Lock()
	defer ks.mu.Unlock()
	if _, ok := ks.kinds[slug]; !ok {
		return fmt.Errorf("gate kind %q: %w", slug, ErrUnknownKind)
	}
	ks.labels[slug] = specID
	return nil
}

// Lookup returns a registered kind, its compiled payload schema and its spec label.
func (ks *Kinds) Lookup(slug string) (GateKind, *jsonschema.Schema, string, error) {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	k, ok := ks.kinds[slug]
	if !ok {
		return GateKind{}, nil, "", fmt.Errorf("gate kind %q: %w", slug, ErrUnknownKind)
	}
	return k, ks.schemas[slug], ks.labels[slug], nil
}

// Slugs returns every registered slug, sorted.
func (ks *Kinds) Slugs() []string {
	ks.mu.RLock()
	defer ks.mu.RUnlock()
	out := make([]string, 0, len(ks.kinds))
	for s := range ks.kinds {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// problems maps gate sentinel errors to HTTP problems.
var problems = []struct {
	err    error
	status int
	code   string
	rule   string
}{
	{ErrUnknownKind, http.StatusUnprocessableEntity, "unknown_gate_kind", "ARC-I-013"},
	{ErrInvalidKind, http.StatusUnprocessableEntity, "invalid_gate_kind", "ARC-I-013"},
	{ErrInvalidPayload, http.StatusUnprocessableEntity, "invalid_payload", ""},
	{ErrNotFound, http.StatusNotFound, "not_found", ""},
	{ErrPayloadChanged, http.StatusConflict, "payload_changed", "ARC-I-014"},
	{ErrAlreadyDecided, http.StatusConflict, "already_decided", "ARC-I-019"},
}

// Problem maps a gate error to a problem+json body. It returns nil when err is not a known
// gate error.
func Problem(err error) *api.Problem {
	for _, p := range problems {
		if errors.Is(err, p.err) {
			return &api.Problem{Title: p.err.Error(), Status: p.status, Code: p.code, Rule: p.rule}
		}
	}
	return nil
}
