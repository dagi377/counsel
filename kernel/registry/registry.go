// CONTRACT: the Registry interface. Frozen once on main; changes are additive only (a new
// extension point) and need a docs/QUESTIONS.md entry. A removed or renamed method needs a
// major kernel version and an ADR. Source: spec/eng/k1-kernel.html.

// Package registry is the only way an app extends the kernel. The app's Register function
// receives a Registry at boot; nothing can be registered at runtime. Registered data is
// validated at boot against spec/kernel/*.schema.json, and duplicate ids or a kernel prefix
// make boot fail.
package registry

import (
	"io/fs"
	"sync"

	"github.com/go-chi/chi/v5"
)

// Registry is the boot-time extension surface the kernel gives an app.
type Registry interface {
	Capability(Capability)
	Role(Role)
	SubjectKind(SubjectKind, SubjectResolver)
	GateKind(GateKind)
	EventType(name string, schema []byte)
	InboundProvider(Provider)
	RulePack(fs.FS)
	DocumentKind(DocumentKind)
	RetentionClass(RetentionClass)
	Report(ReportDef)
	PortalPage(PortalPage)
	Tool(Tool)
	ChartOfAccounts(Chart)
	NumberSeries(name, format string)
	SmokeCheck(name string, fn CheckFunc)
	RestoreCheck(name string, fn CheckFunc)
	Routes(func(chi.Router))
}

// Method names, as recorded in Call.Method.
const (
	MethodCapability      = "Capability"
	MethodRole            = "Role"
	MethodSubjectKind     = "SubjectKind"
	MethodGateKind        = "GateKind"
	MethodEventType       = "EventType"
	MethodInboundProvider = "InboundProvider"
	MethodRulePack        = "RulePack"
	MethodDocumentKind    = "DocumentKind"
	MethodRetentionClass  = "RetentionClass"
	MethodReport          = "Report"
	MethodPortalPage      = "PortalPage"
	MethodTool            = "Tool"
	MethodChartOfAccounts = "ChartOfAccounts"
	MethodNumberSeries    = "NumberSeries"
	MethodSmokeCheck      = "SmokeCheck"
	MethodRestoreCheck    = "RestoreCheck"
	MethodRoutes          = "Routes"
)

// Call is one recorded Registry call. Args holds the call's arguments in order.
type Call struct {
	Method string
	Args   []any
}

// Recorder is a recording fake Registry for module tests. It accepts every call, validates
// nothing and keeps each call in order. It is safe for concurrent use.
type Recorder struct {
	mu    sync.Mutex
	calls []Call
}

var _ Registry = (*Recorder)(nil)

func (r *Recorder) record(method string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, Call{Method: method, Args: args})
}

// Calls returns a copy of every recorded call, in call order.
func (r *Recorder) Calls() []Call {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Call, len(r.calls))
	copy(out, r.calls)
	return out
}

// Method returns the recorded calls to one method, in call order.
func (r *Recorder) Method(name string) []Call {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Call
	for _, c := range r.calls {
		if c.Method == name {
			out = append(out, c)
		}
	}
	return out
}

// Capability records the call.
func (r *Recorder) Capability(c Capability) { r.record(MethodCapability, c) }

// Role records the call.
func (r *Recorder) Role(x Role) { r.record(MethodRole, x) }

// SubjectKind records the call.
func (r *Recorder) SubjectKind(k SubjectKind, res SubjectResolver) {
	r.record(MethodSubjectKind, k, res)
}

// GateKind records the call.
func (r *Recorder) GateKind(g GateKind) { r.record(MethodGateKind, g) }

// EventType records the call.
func (r *Recorder) EventType(name string, schema []byte) { r.record(MethodEventType, name, schema) }

// InboundProvider records the call.
func (r *Recorder) InboundProvider(p Provider) { r.record(MethodInboundProvider, p) }

// RulePack records the call.
func (r *Recorder) RulePack(f fs.FS) { r.record(MethodRulePack, f) }

// DocumentKind records the call.
func (r *Recorder) DocumentKind(d DocumentKind) { r.record(MethodDocumentKind, d) }

// RetentionClass records the call.
func (r *Recorder) RetentionClass(c RetentionClass) { r.record(MethodRetentionClass, c) }

// Report records the call.
func (r *Recorder) Report(d ReportDef) { r.record(MethodReport, d) }

// PortalPage records the call.
func (r *Recorder) PortalPage(p PortalPage) { r.record(MethodPortalPage, p) }

// Tool records the call.
func (r *Recorder) Tool(t Tool) { r.record(MethodTool, t) }

// ChartOfAccounts records the call.
func (r *Recorder) ChartOfAccounts(c Chart) { r.record(MethodChartOfAccounts, c) }

// NumberSeries records the call.
func (r *Recorder) NumberSeries(name, format string) { r.record(MethodNumberSeries, name, format) }

// SmokeCheck records the call.
func (r *Recorder) SmokeCheck(name string, fn CheckFunc) { r.record(MethodSmokeCheck, name, fn) }

// RestoreCheck records the call.
func (r *Recorder) RestoreCheck(name string, fn CheckFunc) { r.record(MethodRestoreCheck, name, fn) }

// Routes records the call.
func (r *Recorder) Routes(fn func(chi.Router)) { r.record(MethodRoutes, fn) }
