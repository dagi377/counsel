// Package registryfake is an in-memory registry.Booter for tests. It checks duplicate ids,
// kernel-reserved prefixes and runtime registration; it does not validate JSON Schemas.
package registryfake

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/registry"
)

var _ registry.Booter = (*Fake)(nil)

// Fake keeps every registration in memory, keyed by kind and id.
type Fake struct {
	registry.Recorder

	mu     sync.Mutex
	ids    map[string]map[string]int // kind -> id -> count
	order  []kindID
	late   []string
	sealed bool
}

type kindID struct{ kind, id string }

// New returns an empty Fake.
func New() *Fake { return &Fake{ids: map[string]map[string]int{}} }

func (f *Fake) add(kind, id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ids == nil {
		f.ids = map[string]map[string]int{}
	}
	if f.sealed {
		f.late = append(f.late, kind+" "+id)
		return
	}
	if f.ids[kind] == nil {
		f.ids[kind] = map[string]int{}
	}
	if f.ids[kind][id] == 0 {
		f.order = append(f.order, kindID{kind, id})
	}
	f.ids[kind][id]++
}

// IDs returns the ids registered for one method name (registry.Method*), in first-seen order.
func (f *Fake) IDs(kind string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, k := range f.order {
		if k.kind == kind {
			out = append(out, k.id)
		}
	}
	return out
}

// Seal checks duplicates, kernel prefixes and late registrations, then freezes the Fake.
func (f *Fake) Seal() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var errs []error
	for _, k := range f.order {
		if n := f.ids[k.kind][k.id]; n > 1 {
			errs = append(errs, fmt.Errorf("%s %q registered %d times: %w", k.kind, k.id, n, registry.ErrDuplicateID))
		}
		for _, p := range registry.KernelPrefixes {
			if strings.HasPrefix(k.id, p) {
				errs = append(errs, fmt.Errorf("%s %q: %w", k.kind, k.id, registry.ErrKernelPrefix))
			}
		}
	}
	for _, l := range f.late {
		errs = append(errs, fmt.Errorf("%s: %w", l, registry.ErrSealed))
	}
	f.late = nil
	f.sealed = true
	return errors.Join(errs...)
}

// Capability records and indexes the capability.
func (f *Fake) Capability(c registry.Capability) {
	f.Recorder.Capability(c)
	f.add(registry.MethodCapability, c.ID)
}

// Role records and indexes the role.
func (f *Fake) Role(r registry.Role) { f.Recorder.Role(r); f.add(registry.MethodRole, r.ID) }

// SubjectKind records and indexes the subject kind.
func (f *Fake) SubjectKind(k registry.SubjectKind, res registry.SubjectResolver) {
	f.Recorder.SubjectKind(k, res)
	f.add(registry.MethodSubjectKind, k.ID)
}

// GateKind records and indexes the gate kind.
func (f *Fake) GateKind(g registry.GateKind) {
	f.Recorder.GateKind(g)
	f.add(registry.MethodGateKind, g.Slug)
}

// EventType records and indexes the event type.
func (f *Fake) EventType(name string, schema []byte) {
	f.Recorder.EventType(name, schema)
	f.add(registry.MethodEventType, name)
}

// InboundProvider records and indexes the provider.
func (f *Fake) InboundProvider(p registry.Provider) {
	f.Recorder.InboundProvider(p)
	f.add(registry.MethodInboundProvider, p.ID)
}

// RulePack records the rule pack; packs carry no id.
func (f *Fake) RulePack(fsys fs.FS) { f.Recorder.RulePack(fsys) }

// DocumentKind records and indexes the document kind.
func (f *Fake) DocumentKind(d registry.DocumentKind) {
	f.Recorder.DocumentKind(d)
	f.add(registry.MethodDocumentKind, d.ID)
}

// RetentionClass records and indexes the retention class.
func (f *Fake) RetentionClass(c registry.RetentionClass) {
	f.Recorder.RetentionClass(c)
	f.add(registry.MethodRetentionClass, c.ID)
}

// Report records and indexes the report.
func (f *Fake) Report(d registry.ReportDef) {
	f.Recorder.Report(d)
	f.add(registry.MethodReport, d.ID)
}

// PortalPage records and indexes the portal page.
func (f *Fake) PortalPage(p registry.PortalPage) {
	f.Recorder.PortalPage(p)
	f.add(registry.MethodPortalPage, p.ID)
}

// Tool records and indexes the tool.
func (f *Fake) Tool(t registry.Tool) { f.Recorder.Tool(t); f.add(registry.MethodTool, t.Name) }

// ChartOfAccounts records and indexes the chart by book.
func (f *Fake) ChartOfAccounts(c registry.Chart) {
	f.Recorder.ChartOfAccounts(c)
	f.add(registry.MethodChartOfAccounts, c.Book)
}

// NumberSeries records and indexes the series.
func (f *Fake) NumberSeries(name, format string) {
	f.Recorder.NumberSeries(name, format)
	f.add(registry.MethodNumberSeries, name)
}

// SmokeCheck records and indexes the check.
func (f *Fake) SmokeCheck(name string, fn registry.CheckFunc) {
	f.Recorder.SmokeCheck(name, fn)
	f.add(registry.MethodSmokeCheck, name)
}

// RestoreCheck records and indexes the check.
func (f *Fake) RestoreCheck(name string, fn registry.CheckFunc) {
	f.Recorder.RestoreCheck(name, fn)
	f.add(registry.MethodRestoreCheck, name)
}

// Routes records the route function; routes carry no id.
func (f *Fake) Routes(fn func(chi.Router)) { f.Recorder.Routes(fn) }
