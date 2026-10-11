package registryfake

import (
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/registry"
)

func TestFakeSeal(t *testing.T) {
	tests := []struct {
		name     string
		register func(f *Fake)
		want     []error
	}{
		{
			name: "clean",
			register: func(f *Fake) {
				f.Capability(registry.Capability{ID: "desk.view"})
				f.Role(registry.Role{ID: "agent_role", Capabilities: []string{"desk.view"}})
				f.GateKind(registry.GateKind{Slug: "desk.close"})
				f.NumberSeries("desk.ticket", "T-{n}")
			},
		},
		{
			name: "duplicate capability",
			register: func(f *Fake) {
				f.Capability(registry.Capability{ID: "desk.view"})
				f.Capability(registry.Capability{ID: "desk.view"})
			},
			want: []error{registry.ErrDuplicateID},
		},
		{
			name: "same id across kinds is fine",
			register: func(f *Fake) {
				f.Capability(registry.Capability{ID: "desk.view"})
				f.Report(registry.ReportDef{ID: "desk.view"})
			},
		},
		{
			name: "kernel prefix",
			register: func(f *Fake) {
				f.Tool(registry.Tool{Name: "core.cite"})
			},
			want: []error{registry.ErrKernelPrefix},
		},
		{
			name: "duplicate and prefix",
			register: func(f *Fake) {
				f.GateKind(registry.GateKind{Slug: "kernel.x"})
				f.GateKind(registry.GateKind{Slug: "kernel.x"})
			},
			want: []error{registry.ErrDuplicateID, registry.ErrKernelPrefix},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			tt.register(f)
			err := f.Seal()
			if len(tt.want) == 0 && err != nil {
				t.Fatalf("Seal() = %v, want nil", err)
			}
			for _, w := range tt.want {
				if !errors.Is(err, w) {
					t.Errorf("Seal() = %v, want %v", err, w)
				}
			}
		})
	}
}

func TestFakeSealedRejectsLateRegistration(t *testing.T) {
	f := New()
	if err := f.Seal(); err != nil {
		t.Fatalf("first Seal() = %v", err)
	}
	f.Capability(registry.Capability{ID: "desk.view"})
	if err := f.Seal(); !errors.Is(err, registry.ErrSealed) {
		t.Fatalf("Seal() after late call = %v, want ErrSealed", err)
	}
	if got := f.IDs(registry.MethodCapability); len(got) != 0 {
		t.Errorf("late capability indexed: %v", got)
	}
}

func TestFakeRecordsAndIndexes(t *testing.T) {
	tests := []struct {
		name   string
		call   func(f *Fake)
		method string
		ids    []string
	}{
		{"capability", func(f *Fake) { f.Capability(registry.Capability{ID: "a.b"}) }, registry.MethodCapability, []string{"a.b"}},
		{"event", func(f *Fake) { f.EventType("a.created", nil) }, registry.MethodEventType, []string{"a.created"}},
		{"chart", func(f *Fake) { f.ChartOfAccounts(registry.Chart{Book: "ops"}) }, registry.MethodChartOfAccounts, []string{"ops"}},
		{"smoke", func(f *Fake) { f.SmokeCheck("a.up", nil) }, registry.MethodSmokeCheck, []string{"a.up"}},
		{"routes", func(f *Fake) { f.Routes(nil) }, registry.MethodRoutes, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			tt.call(f)
			if n := len(f.Method(tt.method)); n != 1 {
				t.Errorf("recorded %d %s calls, want 1", n, tt.method)
			}
			got := f.IDs(tt.method)
			if len(got) != len(tt.ids) || (len(got) > 0 && got[0] != tt.ids[0]) {
				t.Errorf("IDs(%s) = %v, want %v", tt.method, got, tt.ids)
			}
		})
	}
}
