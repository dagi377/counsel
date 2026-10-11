package deadline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

// packSchemaURL names the rule pack schema inside the compiler; it is never fetched.
const packSchemaURL = "file:///spec/kernel/rulepack.schema.json"

// PackLoader parses rule pack YAML and validates it against the rule pack JSON Schema.
type PackLoader struct {
	schema *jsonschema.Schema
}

// NewPackLoader compiles schema, the contents of spec/kernel/rulepack.schema.json.
func NewPackLoader(schema []byte) (*PackLoader, error) {
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	c.AssertFormat = true
	if err := c.AddResource(packSchemaURL, bytes.NewReader(schema)); err != nil {
		return nil, fmt.Errorf("deadline: add pack schema: %w", err)
	}
	s, err := c.Compile(packSchemaURL)
	if err != nil {
		return nil, fmt.Errorf("deadline: compile pack schema: %w", err)
	}
	return &PackLoader{schema: s}, nil
}

// packFile is the YAML shape of a rule pack.
type packFile struct {
	Pack     string     `yaml:"pack"`
	Version  int        `yaml:"version"`
	TimeZone string     `yaml:"time_zone"`
	Holidays []string   `yaml:"holidays"`
	Rules    []ruleFile `yaml:"rules"`
}

type ruleFile struct {
	ID          string `yaml:"id"`
	Trigger     string `yaml:"trigger"`
	OffsetDays  int    `yaml:"offset_days"`
	MonthsAfter int    `yaml:"months_after"`
	DayCount    string `yaml:"day_count"`
	Roll        string `yaml:"roll"`
}

// Parse validates raw rule pack YAML against the schema and returns the Pack. Any failure
// wraps ErrInvalidPack.
func (l *PackLoader) Parse(raw []byte) (Pack, error) {
	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return Pack{}, fmt.Errorf("%w: yaml: %v", ErrInvalidPack, err)
	}
	doc, err := plain(&node)
	if err != nil {
		return Pack{}, fmt.Errorf("%w: yaml: %v", ErrInvalidPack, err)
	}
	// Round-trip through JSON so the validator sees JSON types.
	js, err := json.Marshal(doc)
	if err != nil {
		return Pack{}, fmt.Errorf("%w: not JSON data: %v", ErrInvalidPack, err)
	}
	var v any
	if err := json.Unmarshal(js, &v); err != nil {
		return Pack{}, fmt.Errorf("%w: decode: %v", ErrInvalidPack, err)
	}
	if err := l.schema.Validate(v); err != nil {
		return Pack{}, fmt.Errorf("%w: %v", ErrInvalidPack, err)
	}
	var f packFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return Pack{}, fmt.Errorf("%w: yaml: %v", ErrInvalidPack, err)
	}
	return f.pack()
}

// plain decodes a YAML node into JSON-like values, keeping an unquoted date such as
// 2026-12-25 as the string it was written as instead of a timestamp.
func plain(n *yaml.Node) (any, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return plain(n.Content[0])
	case yaml.ScalarNode:
		if n.Tag == "!!timestamp" {
			return n.Value, nil
		}
		var v any
		if err := n.Decode(&v); err != nil {
			return nil, err
		}
		return v, nil
	case yaml.AliasNode:
		return plain(n.Alias)
	case yaml.SequenceNode:
		out := make([]any, len(n.Content))
		for i, c := range n.Content {
			e, err := plain(c)
			if err != nil {
				return nil, err
			}
			out[i] = e
		}
		return out, nil
	case yaml.MappingNode:
		out := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			e, err := plain(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			out[n.Content[i].Value] = e
		}
		return out, nil
	}
	return nil, fmt.Errorf("unexpected yaml node kind %d", n.Kind)
}

// pack converts a schema-valid file to a Pack, rejecting what the contract cannot express.
func (f packFile) pack() (Pack, error) {
	if f.TimeZone != "" {
		if _, err := time.LoadLocation(f.TimeZone); err != nil {
			return Pack{}, fmt.Errorf("%w: time zone %q", ErrInvalidPack, f.TimeZone)
		}
	}
	p := Pack{ID: f.Pack, Version: strconv.Itoa(f.Version)}
	for _, h := range f.Holidays {
		t, err := time.Parse(time.DateOnly, h)
		if err != nil {
			return Pack{}, fmt.Errorf("%w: holiday %q", ErrInvalidPack, h)
		}
		p.Holidays = append(p.Holidays, Date{Year: t.Year(), Month: t.Month(), Day: t.Day()})
	}
	seen := map[string]bool{}
	for _, r := range f.Rules {
		if seen[r.ID] {
			return Pack{}, fmt.Errorf("%w: duplicate rule %q", ErrInvalidPack, r.ID)
		}
		seen[r.ID] = true
		rule := Rule{ID: r.ID, Trigger: r.Trigger, Mode: DayCount(r.DayCount), Days: r.OffsetDays,
			MonthsAfter: r.MonthsAfter, Roll: Roll(r.Roll)}
		switch rule.Mode {
		case Calendar, Clear, Business:
		default:
			return Pack{}, fmt.Errorf("%w: rule %q: day count %q", ErrInvalidPack, r.ID, r.DayCount)
		}
		switch rule.Roll {
		case "":
			rule.Roll = RollNone
		case RollNone, RollForward:
		default:
			return Pack{}, fmt.Errorf("%w: rule %q: roll %q not supported", ErrInvalidPack, r.ID, r.Roll)
		}
		if rule.MonthsAfter < 0 {
			return Pack{}, fmt.Errorf("%w: rule %q: negative months_after", ErrInvalidPack, r.ID)
		}
		p.Rules = append(p.Rules, rule)
	}
	return p, nil
}
