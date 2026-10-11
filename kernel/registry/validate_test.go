package registry

import (
	"errors"
	"os"
	"testing"
	"testing/fstest"
	"time"
)

func loadTestSchemas(t *testing.T) *Schemas {
	t.Helper()
	s, err := LoadSchemas(os.DirFS("../../spec/kernel"))
	if err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	return s
}

const objSchema = `{"type":"object"}`

func goodGate() GateKind {
	return GateKind{
		Slug: "desk.close", Approver: "desk.approve", Risk: RiskHigh, MinApprovers: 1,
		TTL: 72 * time.Hour, PayloadSchema: []byte(objSchema),
	}
}

const goodPack = `pack: desk
version: 1
holidays: [2026-01-01]
rules:
  - id: reply_due
    trigger: opened
    offset_days: 2
    day_count: business
`

// register makes a full, valid set of registrations and then applies extra.
func register(r Registry, extra func(Registry)) {
	r.Capability(Capability{ID: "desk.approve", Description: "approve"})
	r.Role(Role{ID: "desk.agent", Name: "Agent", Capabilities: []string{"desk.approve"}})
	r.SubjectKind(SubjectKind{ID: "desk.ticket"}, nil)
	r.GateKind(goodGate())
	r.EventType("desk.opened", []byte(objSchema))
	r.InboundProvider(Provider{ID: "mail"})
	r.RulePack(fstest.MapFS{"desk.yaml": {Data: []byte(goodPack)}})
	r.DocumentKind(DocumentKind{ID: "desk.note", Description: "note", RetentionClass: "desk.default"})
	r.RetentionClass(RetentionClass{ID: "desk.default", MinYears: 7, DestructionGate: "desk.purge"})
	r.Report(ReportDef{ID: "desk.open", Capability: "desk.approve", Params: []byte(objSchema)})
	r.PortalPage(PortalPage{ID: "desk.status", Route: "/status/{id}", Capability: "desk.approve"})
	r.Tool(Tool{Name: "find_ticket", Kind: ToolRead, Input: []byte(objSchema), Capability: "desk.approve"})
	r.ChartOfAccounts(Chart{Book: "desk", Accounts: []Account{{Code: "1000", Type: AccountAsset}}})
	r.NumberSeries("desk.ticket", "T-{NNNN}")
	r.SmokeCheck("desk.ping", nil)
	r.RestoreCheck("desk.rows", nil)
	if extra != nil {
		extra(r)
	}
}

func TestBootSeal(t *testing.T) {
	schemas := loadTestSchemas(t)
	tests := []struct {
		name  string
		extra func(Registry)
		want  []error
	}{
		{name: "valid app boots"},
		{name: "duplicate capability", extra: func(r Registry) {
			r.Capability(Capability{ID: "desk.approve", Description: "again"})
		}, want: []error{ErrDuplicateID}},
		{name: "duplicate tool", extra: func(r Registry) {
			r.Tool(Tool{Name: "find_ticket", Kind: ToolRead, Input: []byte(objSchema), Capability: "desk.approve"})
		}, want: []error{ErrDuplicateID}},
		{name: "duplicate rule pack", extra: func(r Registry) {
			r.RulePack(fstest.MapFS{"again.yaml": {Data: []byte(goodPack)}})
		}, want: []error{ErrDuplicateID}},
		{name: "duplicate account code", extra: func(r Registry) {
			r.ChartOfAccounts(Chart{Book: "ops", Accounts: []Account{{Code: "1"}, {Code: "1"}}})
		}, want: []error{ErrDuplicateID}},
		{name: "kernel prefix on capability", extra: func(r Registry) {
			r.Capability(Capability{ID: "core.anything", Description: "x"})
		}, want: []error{ErrKernelPrefix}},
		{name: "kernel prefix on number series", extra: func(r Registry) {
			r.NumberSeries("kernel.seq", "{N}")
		}, want: []error{ErrKernelPrefix}},
		{name: "kernel prefix on role", extra: func(r Registry) {
			r.Role(Role{ID: "core.admin"})
		}, want: []error{ErrKernelPrefix}},
		{name: "capability without module prefix", extra: func(r Registry) {
			r.Capability(Capability{ID: "approve", Description: "x"})
		}, want: []error{ErrInvalid}},
		{name: "irreversible gate with one approver", extra: func(r Registry) {
			g := goodGate()
			g.Slug, g.Risk = "desk.delete", RiskIrreversible
			r.GateKind(g)
		}, want: []error{ErrInvalid}},
		{name: "gate payload schema not JSON", extra: func(r Registry) {
			g := goodGate()
			g.Slug, g.PayloadSchema = "desk.bad", []byte("{")
			r.GateKind(g)
		}, want: []error{ErrInvalid}},
		{name: "gate without payload schema", extra: func(r Registry) {
			g := goodGate()
			g.Slug, g.PayloadSchema = "desk.none", nil
			r.GateKind(g)
		}, want: []error{ErrInvalid}},
		{name: "gate negative ttl", extra: func(r Registry) {
			g := goodGate()
			g.Slug, g.TTL = "desk.neg", -time.Hour
			r.GateKind(g)
		}, want: []error{ErrInvalid}},
		{name: "purgeable retention class without destruction gate", extra: func(r Registry) {
			r.RetentionClass(RetentionClass{ID: "desk.short", MinYears: 1})
		}, want: []error{ErrInvalid}},
		{name: "never purge needs no gate", extra: func(r Registry) {
			r.RetentionClass(RetentionClass{ID: "desk.forever", NeverPurge: true})
		}},
		{name: "portal route not a path", extra: func(r Registry) {
			r.PortalPage(PortalPage{ID: "desk.home", Route: "home", Capability: "desk.approve"})
		}, want: []error{ErrInvalid}},
		{name: "tool bad kind", extra: func(r Registry) {
			r.Tool(Tool{Name: "close_ticket", Kind: "approve", Input: []byte(objSchema), Capability: "desk.approve"})
		}, want: []error{ErrInvalid}},
		{name: "report params not a schema", extra: func(r Registry) {
			r.Report(ReportDef{ID: "desk.bad", Capability: "desk.approve", Params: []byte(`{"type":7}`)})
		}, want: []error{ErrInvalid}},
		{name: "event type without schema", extra: func(r Registry) {
			r.EventType("desk.closed", nil)
		}, want: []error{ErrInvalid}},
		{name: "rule pack with bad day count", extra: func(r Registry) {
			r.RulePack(fstest.MapFS{"x.yaml": {Data: []byte("pack: other\nversion: 1\nrules:\n  - {id: a, trigger: b, offset_days: 1, day_count: weekly}\n")}})
		}, want: []error{ErrInvalid}},
		{name: "rule pack with bad holiday", extra: func(r Registry) {
			r.RulePack(fstest.MapFS{"x.yaml": {Data: []byte("pack: other\nversion: 1\nholidays: [\"2026-13-40\"]\nrules:\n  - {id: a, trigger: b, offset_days: 1, day_count: business}\n")}})
		}, want: []error{ErrInvalid}},
		{name: "empty rule pack", extra: func(r Registry) {
			r.RulePack(fstest.MapFS{})
		}, want: []error{ErrInvalid}},
		{name: "several problems all reported", extra: func(r Registry) {
			r.Capability(Capability{ID: "desk.approve", Description: "dup"})
			r.Capability(Capability{ID: "core.x", Description: "x"})
			r.Tool(Tool{Name: "Bad Name", Kind: ToolRead, Input: []byte(objSchema), Capability: "desk.approve"})
		}, want: []error{ErrDuplicateID, ErrKernelPrefix, ErrInvalid}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBoot(schemas)
			register(b, tt.extra)
			err := b.Seal()
			if len(tt.want) == 0 {
				if err != nil {
					t.Fatalf("Seal() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Seal() = nil, want %v", tt.want)
			}
			for _, w := range tt.want {
				if !errors.Is(err, w) {
					t.Errorf("Seal() = %v, want it to wrap %v", err, w)
				}
			}
		})
	}
}

func TestBootSealedRejectsLateRegistration(t *testing.T) {
	b := NewBoot(loadTestSchemas(t))
	register(b, nil)
	if err := b.Seal(); err != nil {
		t.Fatalf("first Seal() = %v", err)
	}
	if err := b.Seal(); err != nil {
		t.Fatalf("second Seal() with no late calls = %v", err)
	}
	b.Capability(Capability{ID: "desk.late", Description: "late"})
	if err := b.Seal(); !errors.Is(err, ErrSealed) {
		t.Fatalf("Seal() after late call = %v, want ErrSealed", err)
	}
}

func TestLoadSchemasMissing(t *testing.T) {
	tests := []struct {
		name string
		fsys fstest.MapFS
	}{
		{name: "empty", fsys: fstest.MapFS{}},
		{name: "broken capability", fsys: fstest.MapFS{"capability.schema.json": {Data: []byte("{")}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := LoadSchemas(tt.fsys); err == nil {
				t.Fatal("LoadSchemas() = nil error, want failure")
			}
		})
	}
	if err := Validate(nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Validate(nil) = %v, want ErrInvalid", err)
	}
}

func TestDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{72 * time.Hour, "72h"},
		{90 * time.Minute, "90m"},
		{1500 * time.Millisecond, "1500ms"},
		{time.Microsecond, "1us"},
		{3, "3ns"},
		{0, "0h"},
	}
	for _, tt := range tests {
		if got := duration(tt.d); got != tt.want {
			t.Errorf("duration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
