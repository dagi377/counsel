package identity

import "github.com/dagi377/counsel/kernel/registry"

// Load replays recorded registrations into r: every Capability, Role, SubjectKind and
// GateKind call, in order. Boot uses it to fill the catalog a checker was built on before the
// app registered, so capability checks see the app's roles. Other calls are skipped.
func (r *Roles) Load(calls []registry.Call) {
	for _, c := range calls {
		switch c.Method {
		case registry.MethodCapability:
			if x, ok := callArg[registry.Capability](c, 0); ok {
				r.Capability(x)
			}
		case registry.MethodRole:
			if x, ok := callArg[registry.Role](c, 0); ok {
				r.Role(x)
			}
		case registry.MethodSubjectKind:
			k, ok1 := callArg[registry.SubjectKind](c, 0)
			res, ok2 := callArg[registry.SubjectResolver](c, 1)
			if ok1 && ok2 {
				r.SubjectKind(k, res)
			}
		case registry.MethodGateKind:
			if x, ok := callArg[registry.GateKind](c, 0); ok {
				r.GateKind(x)
			}
		}
	}
}

func callArg[T any](c registry.Call, i int) (T, bool) {
	var zero T
	if i >= len(c.Args) {
		return zero, false
	}
	v, ok := c.Args[i].(T)
	return v, ok
}
