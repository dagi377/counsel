package tools

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"text/template"
	"time"

	"gopkg.in/yaml.v3"
)

// The cell chart is rendered by a small text/template shim so these tests need no helm
// binary and no network. The shim supports only the Helm functions the chart uses; a
// template that calls anything else fails to parse here, which keeps the chart simple.
// When helm is on PATH, TestHelmShimMatchesHelm checks the shim against `helm template`.

const (
	chartDir   = "../deploy/helm/counsel"
	valuesFile = chartDir + "/values.yaml"
)

// k8sObj is one rendered manifest.
type k8sObj map[string]any

func (o k8sObj) kind() string { s, _ := o["kind"].(string); return s }

func (o k8sObj) meta(field string) string {
	m, _ := o["metadata"].(map[string]any)
	s, _ := m[field].(string)
	return s
}

func (o k8sObj) id() string {
	return o.kind() + "/" + o.meta("namespace") + "/" + o.meta("name")
}

func (o k8sObj) label(key string) string {
	m, _ := o["metadata"].(map[string]any)
	l, _ := m["labels"].(map[string]any)
	s, _ := l[key].(string)
	return s
}

// dig walks nested maps by key; it returns nil when any step is missing.
func dig(v any, path ...string) any {
	if o, ok := v.(k8sObj); ok {
		v = map[string]any(o)
	}
	for _, p := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[p]
	}
	return v
}

func helmFuncs() template.FuncMap {
	return template.FuncMap{
		"quote": func(v any) string { return fmt.Sprintf("%q", fmt.Sprint(v)) },
		"default": func(d any, given ...any) any {
			if len(given) == 0 || given[0] == nil || reflect.ValueOf(given[0]).IsZero() {
				return d
			}
			return given[0]
		},
		"required": func(msg string, v any) (any, error) {
			if v == nil || reflect.ValueOf(v).IsZero() {
				return nil, errors.New(msg)
			}
			return v, nil
		},
		"fail": func(msg string) (string, error) { return "", errors.New(msg) },
		// int matters: helm reads values as JSON, so 1000000 would print as 1e+06.
		"int":        func(v any) int { return asInt(v) },
		"regexMatch": func(re, s string) bool { return regexp.MustCompile(re).MatchString(s) },
	}
}

// setPath sets a dotted key (e.g. "region.dr") in a values map, creating maps as needed.
func setPath(m map[string]any, key string, v any) {
	parts := strings.Split(key, ".")
	for _, p := range parts[:len(parts)-1] {
		next, ok := m[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[p] = next
		}
		m = next
	}
	m[parts[len(parts)-1]] = v
}

func loadValues(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(valuesFile)
	if err != nil {
		t.Fatalf("read values: %v", err)
	}
	vals := map[string]any{}
	if err := yaml.Unmarshal(raw, &vals); err != nil {
		t.Fatalf("parse values: %v", err)
	}
	return vals
}

// renderChart renders every template with values.yaml plus the dotted-key overrides.
func renderChart(t *testing.T, set map[string]any) ([]k8sObj, error) {
	t.Helper()
	vals := loadValues(t)
	for k, v := range set {
		setPath(vals, k, v)
	}
	files, err := filepath.Glob(filepath.Join(chartDir, "templates", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no chart templates found: %v", err)
	}
	sort.Strings(files)
	data := map[string]any{
		"Values":  vals,
		"Release": map[string]any{"Name": "counsel", "Namespace": "default"},
		"Chart":   map[string]any{"Name": "counsel"},
	}
	var out []k8sObj
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		tpl, err := template.New(filepath.Base(f)).Funcs(helmFuncs()).Option("missingkey=zero").Parse(string(src))
		if err != nil {
			t.Fatalf("parse %s: %v", f, err)
		}
		var buf bytes.Buffer
		if err := tpl.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("render %s: %w", filepath.Base(f), err)
		}
		objs, err := decodeManifests(buf.Bytes())
		if err != nil {
			t.Fatalf("decode %s: %v\n%s", f, err, buf.String())
		}
		out = append(out, objs...)
	}
	return out, nil
}

func mustRender(t *testing.T, set map[string]any) []k8sObj {
	t.Helper()
	objs, err := renderChart(t, set)
	if err != nil {
		t.Fatalf("render chart: %v", err)
	}
	return objs
}

func decodeManifests(b []byte) ([]k8sObj, error) {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	var out []k8sObj
	for {
		// Decode into a plain map: yaml.v3 would reuse the named type for nested maps.
		var o map[string]any
		err := dec.Decode(&o)
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if len(o) > 0 {
			out = append(out, k8sObj(o))
		}
	}
}

// controlPlane reports whether a manifest belongs to the shared control plane (the edge
// WAF) rather than to the cell. Those are the only resources allowed to repeat across cells.
func controlPlane(o k8sObj) bool {
	return o.label("app.kubernetes.io/part-of") == "counsel-controlplane"
}

// refKeys are keys whose string values name a per-cell resource.
var refKeys = map[string]bool{
	"namespace": true, "secretName": true, "serviceAccountName": true, "host": true, "hosts": true,
	"destinationPath": true, "encryptionKeyId": true, "kmsKeyId": true, "storageClass": true,
}

// refParents are maps whose "name" key names another per-cell resource.
var refParents = map[string]bool{
	"metadata": true, "secretKeyRef": true, "secretRef": true, "configMapRef": true,
	"configMapKeyRef": true, "secret": true, "service": true, "s3Credentials": true, "cluster": true,
	"secretStoreRef": true, "target": true, "serviceAccountRef": true,
}

var refDataKey = regexp.MustCompile(`BUCKET|KMS|HOSTNAME|DSN|SECRET|^key_id$`)

// cellRefs collects every string that names a per-cell resource: object names and
// namespaces, secret and config refs, hostnames, buckets, KMS keys and DSN refs.
func cellRefs(objs []k8sObj) map[string]string {
	refs := map[string]string{}
	var walk func(where string, parent string, v any)
	walk = func(where, parent string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				switch s := c.(type) {
				case string:
					if refKeys[k] || (k == "name" && refParents[parent]) {
						refs[s] = where + "." + k
					}
				case []any:
					if refKeys[k] {
						for _, e := range s {
							if str, ok := e.(string); ok {
								refs[str] = where + "." + k
							}
						}
					}
				}
				walk(where+"."+k, k, c)
			}
		case []any:
			for _, c := range x {
				walk(where, parent, c)
			}
		}
	}
	for _, o := range objs {
		if controlPlane(o) {
			continue
		}
		walk(o.id(), "", map[string]any(o))
		if o.kind() == "ConfigMap" {
			data, _ := o["data"].(map[string]any)
			for k, v := range data {
				if s, ok := v.(string); ok && refDataKey.MatchString(k) {
					refs[s] = o.id() + ".data." + k
				}
			}
		}
	}
	return refs
}

// sharedRefs returns refs present in both renders.
func sharedRefs(a, b map[string]string) []string {
	var out []string
	for s, where := range a {
		if _, ok := b[s]; ok {
			out = append(out, fmt.Sprintf("%q (%s)", s, where))
		}
	}
	sort.Strings(out)
	return out
}

// rule: ARC-U-001
func TestARC_U_001_NoSharedResources(t *testing.T) {
	a := cellRefs(mustRender(t, map[string]any{"cell": "alpha"}))
	b := cellRefs(mustRender(t, map[string]any{"cell": "bravo"}))
	if len(a) == 0 {
		t.Fatal("render found no per-cell resource refs")
	}
	if shared := sharedRefs(a, b); len(shared) > 0 {
		t.Fatalf("cells alpha and bravo share resources: %s", strings.Join(shared, ", "))
	}
	tests := []struct {
		name string
		cell string
		refs map[string]string
	}{
		{"alpha", "alpha", a},
		{"bravo", "bravo", b},
	}
	for _, tt := range tests {
		t.Run(tt.name+" derives every ref from the cell", func(t *testing.T) {
			for s, where := range tt.refs {
				if !strings.Contains(s, tt.cell) {
					t.Errorf("%s = %q does not derive from cell %q", where, s, tt.cell)
				}
			}
		})
	}
}

// rule: ARC-U-001
func TestARC_U_001_GuardCatchesSharedRef(t *testing.T) {
	mk := func(cell, secret string) []k8sObj {
		return []k8sObj{{
			"kind":     "Deployment",
			"metadata": map[string]any{"name": "counsel-" + cell + "-api", "namespace": "counsel-" + cell},
			"spec":     map[string]any{"env": []any{map[string]any{"valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": secret}}}}},
		}}
	}
	tests := []struct {
		name       string
		a, b       []k8sObj
		wantShared int
	}{
		{"per-cell secret", mk("alpha", "counsel-alpha-pg-app"), mk("bravo", "counsel-bravo-pg-app"), 0},
		{"shared DSN secret", mk("alpha", "counsel-pg-app"), mk("bravo", "counsel-pg-app"), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sharedRefs(cellRefs(tt.a), cellRefs(tt.b)); len(got) != tt.wantShared {
				t.Fatalf("shared = %v, want %d", got, tt.wantShared)
			}
		})
	}
}

// rule: ARC-U-001
func TestARC_U_001_CellRequired(t *testing.T) {
	tests := []struct {
		name    string
		cell    any
		wantErr bool
	}{
		{"missing", "", true},
		{"not a DNS label", "Firm_A", true},
		{"valid", "firm-a", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := renderChart(t, map[string]any{"cell": tt.cell})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// canadaRegions is the allow-list; it lives here, not in values, so a values change cannot
// widen it.
var canadaRegions = []string{"ca-central-1", "ca-west-1"}

// regionShaped matches cloud region ids such as us-east-1, eu-west-2, ca-central-1.
var regionShaped = regexp.MustCompile(`\b(?:af|ap|ca|eu|il|me|mx|sa|us)(?:-gov)?-(?:central|north|south|east|west|northeast|northwest|southeast|southwest)-\d+`)

// nonCanadian returns every region-shaped string in the render that is not Canadian, and
// every AWS endpoint that names no Canadian region.
func nonCanadian(objs []k8sObj) []string {
	var bad []string
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		case string:
			for _, r := range regionShaped.FindAllString(x, -1) {
				if !slices.Contains(canadaRegions, r) {
					bad = append(bad, x)
				}
			}
			if strings.Contains(x, "amazonaws.com") && !slices.ContainsFunc(canadaRegions, func(r string) bool { return strings.Contains(x, r) }) {
				bad = append(bad, x)
			}
		}
	}
	for _, o := range objs {
		walk(map[string]any(o))
	}
	sort.Strings(bad)
	return bad
}

// rule: ARC-U-002
func TestARC_U_002_CanadaOnly(t *testing.T) {
	vals := loadValues(t)
	primary, _ := dig(vals, "region", "primary").(string)
	dr, _ := dig(vals, "region", "dr").(string)
	if primary != "ca-central-1" {
		t.Errorf("region.primary = %q, want ca-central-1", primary)
	}
	if !slices.Contains(canadaRegions, dr) || dr == primary {
		t.Errorf("region.dr = %q, want a second Canadian region", dr)
	}
	tests := []struct {
		name    string
		set     map[string]any
		wantBad bool
	}{
		{"defaults", map[string]any{"cell": "alpha"}, false},
		{"DR in the US", map[string]any{"cell": "alpha", "region.dr": "us-east-1"}, true},
		{"primary in the EU", map[string]any{"cell": "alpha", "region.primary": "eu-west-1"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bad := nonCanadian(mustRender(t, tt.set))
			if (len(bad) > 0) != tt.wantBad {
				t.Fatalf("non-Canadian strings %v, wantBad %v", bad, tt.wantBad)
			}
		})
	}
}

// TestHelmShimMatchesHelm checks that the shim renders the same objects as helm itself.
func TestHelmShimMatchesHelm(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm not on PATH; the shim render is the only check")
	}
	out, err := exec.Command(helm, "template", "counsel", chartDir, "--set", "cell=alpha").CombinedOutput()
	if err != nil {
		t.Fatalf("helm template: %v\n%s", err, out)
	}
	want, err := decodeManifests(out)
	if err != nil {
		t.Fatalf("decode helm output: %v", err)
	}
	got := mustRender(t, map[string]any{"cell": "alpha"})
	index := func(objs []k8sObj) map[string]k8sObj {
		m := map[string]k8sObj{}
		for _, o := range objs {
			m[o.id()] = o
		}
		return m
	}
	g, w := index(got), index(want)
	if len(g) != len(w) {
		t.Fatalf("shim rendered %d objects, helm %d", len(g), len(w))
	}
	for id, wo := range w {
		if !reflect.DeepEqual(g[id], wo) {
			t.Errorf("%s differs between shim and helm", id)
		}
	}
}

// statelessTiers are the tiers ARC-U-003 and ARC-U-016 cover.
var statelessTiers = []string{"web", "api", "worker", "outbox"}

// byKind indexes rendered objects of one kind by their component label.
func byKind(objs []k8sObj, kind string) map[string]k8sObj {
	out := map[string]k8sObj{}
	for _, o := range objs {
		if o.kind() == kind {
			comp, _ := dig(o, "spec", "selector", "matchLabels", "app.kubernetes.io/component").(string)
			out[comp] = o
		}
	}
	return out
}

func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return -1
}

// podContainers returns the containers of a Deployment's pod template.
func podContainers(o k8sObj) []map[string]any {
	var out []map[string]any
	list, _ := dig(o, "spec", "template", "spec", "containers").([]any)
	for _, c := range list {
		if m, ok := c.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// rule: ARC-U-003
func TestARC_U_003_ZoneSpread(t *testing.T) {
	objs := mustRender(t, map[string]any{"cell": "alpha"})
	deps, pdbs := byKind(objs, "Deployment"), byKind(objs, "PodDisruptionBudget")
	for _, tier := range statelessTiers {
		t.Run(tier, func(t *testing.T) {
			d, ok := deps[tier]
			if !ok {
				t.Fatalf("no Deployment for tier %s", tier)
			}
			if r := asInt(dig(d, "spec", "replicas")); r < 2 {
				t.Errorf("replicas = %d, want >= 2", r)
			}
			spread, _ := dig(d, "spec", "template", "spec", "topologySpreadConstraints").([]any)
			want := map[string]any{"maxSkew": 1, "topologyKey": "topology.kubernetes.io/zone", "whenUnsatisfiable": "DoNotSchedule"}
			found := false
			for _, c := range spread {
				m, _ := c.(map[string]any)
				if m["maxSkew"] == want["maxSkew"] && m["topologyKey"] == want["topologyKey"] && m["whenUnsatisfiable"] == want["whenUnsatisfiable"] &&
					reflect.DeepEqual(dig(m, "labelSelector", "matchLabels"), dig(d, "spec", "selector", "matchLabels")) {
					found = true
				}
			}
			if !found {
				t.Errorf("no zone spread constraint %v selecting the tier's pods in %v", want, spread)
			}
			p, ok := pdbs[tier]
			if !ok {
				t.Fatalf("no PodDisruptionBudget for tier %s", tier)
			}
			if m := asInt(dig(p, "spec", "minAvailable")); m != 1 {
				t.Errorf("PDB minAvailable = %d, want 1", m)
			}
			if !reflect.DeepEqual(dig(p, "spec", "selector"), dig(d, "spec", "selector")) {
				t.Errorf("PDB selector %v does not match Deployment selector %v", dig(p, "spec", "selector"), dig(d, "spec", "selector"))
			}
		})
	}
}

// rule: ARC-U-016
func TestARC_U_016_RollingUpdate(t *testing.T) {
	deps := byKind(mustRender(t, map[string]any{"cell": "alpha"}), "Deployment")
	for _, tier := range statelessTiers {
		t.Run(tier, func(t *testing.T) {
			d := deps[tier]
			if d == nil {
				t.Fatalf("no Deployment for tier %s", tier)
			}
			tests := []struct {
				name      string
				got, want any
			}{
				{"strategy", dig(d, "spec", "strategy", "type"), "RollingUpdate"},
				{"maxUnavailable 0", asInt(dig(d, "spec", "strategy", "rollingUpdate", "maxUnavailable")), 0},
				{"maxSurge 1", asInt(dig(d, "spec", "strategy", "rollingUpdate", "maxSurge")), 1},
				{"grace <= 30s", func() bool {
					g := asInt(dig(d, "spec", "template", "spec", "terminationGracePeriodSeconds"))
					return g > 10 && g <= 30
				}(), true},
			}
			for _, c := range podContainers(d) {
				tests = append(tests, struct {
					name      string
					got, want any
				}{"preStop sleep 10", fmt.Sprint(dig(c, "lifecycle", "preStop", "exec", "command")), "[sleep 10]"})
				// Tiers that serve HTTP must gate readiness on /readyz (DB + pooler).
				if dig(c, "ports") != nil {
					tests = append(tests, struct {
						name      string
						got, want any
					}{"readiness /readyz", dig(c, "readinessProbe", "httpGet", "path"), "/readyz"})
				}
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					if tt.got != tt.want {
						t.Fatalf("got %v, want %v", tt.got, tt.want)
					}
				})
			}
		})
	}
	// The serving tiers must have a readiness probe, not just pass vacuously.
	for _, tier := range []string{"web", "api"} {
		cs := podContainers(deps[tier])
		if len(cs) == 0 || dig(cs[0], "readinessProbe", "httpGet", "path") != "/readyz" {
			t.Errorf("tier %s has no /readyz readiness probe", tier)
		}
	}
}

// byName indexes rendered objects of one kind by metadata.name.
func byName(objs []k8sObj, kind string) map[string]k8sObj {
	out := map[string]k8sObj{}
	for _, o := range objs {
		if o.kind() == kind {
			out[o.meta("name")] = o
		}
	}
	return out
}

// rule: ARC-U-005
func TestARC_U_005_Isolation(t *testing.T) {
	objs := mustRender(t, map[string]any{"cell": "alpha"})
	pcs := byName(objs, "PriorityClass")
	prio := func(name string) int { return asInt(dig(pcs[name], "value")) }
	db, app, agent := prio("counsel-alpha-db"), prio("counsel-alpha-app"), prio("counsel-alpha-agent")
	quotas, limits := byName(objs, "ResourceQuota"), byName(objs, "LimitRange")
	sched := byName(objs, "ConfigMap")["paploo-alpha-agent-scheduling"]
	vals := loadValues(t)
	tests := []struct {
		name string
		ok   bool
	}{
		{"priority classes rendered", db > 0 && app > 0 && agent > 0},
		{"DB priority above app", db > app},
		{"agent priority lowest", agent < app && agent < db},
		{"agents never preempt", dig(pcs["counsel-alpha-agent"], "preemptionPolicy") == "Never"},
		{"cell ResourceQuota", quotas["counsel-alpha"] != nil && quotas["counsel-alpha"].meta("namespace") == "counsel-alpha"},
		{"cell LimitRange", limits["counsel-alpha"] != nil && limits["counsel-alpha"].meta("namespace") == "counsel-alpha"},
		{"agent ResourceQuota", quotas["paploo-alpha-agents"] != nil && quotas["paploo-alpha-agents"].meta("namespace") == "paploo-alpha-agents"},
		{"agent LimitRange", limits["paploo-alpha-agents"] != nil},
		{"agent pool differs from app and DB", dig(vals, "nodePools", "agent") != dig(vals, "nodePools", "app") && dig(vals, "nodePools", "agent") != dig(vals, "nodePools", "db")},
		{"agent scheduling uses agent pool", dig(sched, "data", "nodeSelector") == fmt.Sprintf("counsel.io/pool=%v", dig(vals, "nodePools", "agent"))},
		{"agent scheduling tolerates agent taint", dig(sched, "data", "toleration") == fmt.Sprintf("counsel.io/pool=%v:NoSchedule", dig(vals, "nodePools", "agent"))},
		{"agent scheduling uses lowest priority", dig(sched, "data", "priorityClassName") == "counsel-alpha-agent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatal("check failed")
			}
		})
	}
	for tier, d := range byKind(objs, "Deployment") {
		t.Run(tier+" on app pool with app priority", func(t *testing.T) {
			ps := dig(d, "spec", "template", "spec")
			if dig(ps, "priorityClassName") != "counsel-alpha-app" {
				t.Errorf("priorityClassName = %v", dig(ps, "priorityClassName"))
			}
			if dig(ps, "nodeSelector", "counsel.io/pool") != dig(vals, "nodePools", "app") {
				t.Errorf("nodeSelector = %v", dig(ps, "nodeSelector"))
			}
		})
	}
}

// rule: ARC-U-041
func TestARC_U_041_Capacity(t *testing.T) {
	objs := mustRender(t, map[string]any{"cell": "alpha"})
	vals := loadValues(t)
	if m := asInt(dig(vals, "capacity", "loadMultiplier")); m != 3 {
		t.Errorf("capacity.loadMultiplier = %d, want 3", m)
	}
	if p := asInt(dig(vals, "capacity", "dbStorageAlertPercent")); p != 70 {
		t.Errorf("capacity.dbStorageAlertPercent = %d, want 70", p)
	}
	var checked int
	for _, o := range objs {
		var pod any
		switch o.kind() {
		case "Deployment", "StatefulSet", "DaemonSet", "Job":
			pod = dig(o, "spec", "template", "spec")
		case "CronJob":
			pod = dig(o, "spec", "jobTemplate", "spec", "template", "spec")
		default:
			continue
		}
		for _, list := range []string{"initContainers", "containers"} {
			cs, _ := dig(pod, list).([]any)
			for _, c := range cs {
				checked++
				name := o.id() + "/" + fmt.Sprint(dig(c, "name"))
				t.Run(name, func(t *testing.T) {
					for _, f := range []string{"requests", "limits"} {
						for _, r := range []string{"cpu", "memory"} {
							if v, _ := dig(c, "resources", f, r).(string); v == "" {
								t.Errorf("missing resources.%s.%s", f, r)
							}
						}
					}
				})
			}
		}
	}
	if checked == 0 {
		t.Fatal("no containers rendered")
	}
}

// pgCluster returns the cell's CloudNativePG Cluster and the rest of the render.
func pgCluster(t *testing.T, cell string) (k8sObj, []k8sObj) {
	t.Helper()
	objs := mustRender(t, map[string]any{"cell": cell})
	c := byName(objs, "Cluster")["counsel-"+cell+"-pg"]
	if c == nil {
		t.Fatalf("no Cluster counsel-%s-pg rendered", cell)
	}
	return c, objs
}

type check struct {
	name      string
	got, want any
}

func runChecks(t *testing.T, tests []check) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

// rule: ARC-U-006
func TestARC_U_006_QuorumSync(t *testing.T) {
	c, _ := pgCluster(t, "alpha")
	runChecks(t, []check{
		{"3 instances", asInt(dig(c, "spec", "instances")), 3},
		{"minSyncReplicas 1", asInt(dig(c, "spec", "minSyncReplicas")), 1},
		{"maxSyncReplicas 2", asInt(dig(c, "spec", "maxSyncReplicas")), 2},
		{"synchronous_commit on", dig(c, "spec", "postgresql", "parameters", "synchronous_commit"), "on"},
		{"one instance per zone", dig(c, "spec", "affinity", "topologyKey"), "topology.kubernetes.io/zone"},
		{"anti-affinity required", dig(c, "spec", "affinity", "podAntiAffinityType"), "required"},
		{"anti-affinity on", dig(c, "spec", "affinity", "enablePodAntiAffinity"), true},
		{"DB priority class", dig(c, "spec", "priorityClassName"), "counsel-alpha-db"},
		{"DB node pool", dig(c, "spec", "affinity", "nodeSelector", "counsel.io/pool"), dig(loadValues(t), "nodePools", "db")},
	})
}

// cronDaily reports whether a six-field CNPG cron runs once a day.
func cronDaily(s string) bool {
	f := strings.Fields(s)
	if len(f) != 6 {
		return false
	}
	isNum := regexp.MustCompile(`^\d+$`).MatchString
	return isNum(f[0]) && isNum(f[1]) && isNum(f[2]) && f[3] == "*" && f[4] == "*" && f[5] == "*"
}

// rule: ARC-U-008
func TestARC_U_008_WALArchive(t *testing.T) {
	c, objs := pgCluster(t, "alpha")
	dr, _ := dig(loadValues(t), "region", "dr").(string)
	store := dig(c, "spec", "backup", "barmanObjectStore")
	var daily bool
	for _, b := range byName(objs, "ScheduledBackup") {
		if dig(b, "spec", "cluster", "name") == "counsel-alpha-pg" && cronDaily(fmt.Sprint(dig(b, "spec", "schedule"))) {
			daily = true
		}
	}
	runChecks(t, []check{
		{"archive to DR bucket", dig(store, "destinationPath"), "s3://counsel-alpha-wal-dr/"},
		{"DR region endpoint", strings.Contains(fmt.Sprint(dig(store, "endpointURL")), "."+dr+"."), true},
		{"archive_timeout 60s", dig(c, "spec", "postgresql", "parameters", "archive_timeout"), "60s"},
		{"PITR retention 35d", dig(c, "spec", "backup", "retentionPolicy"), "35d"},
		{"daily ScheduledBackup", daily, true},
	})
	for _, tt := range []struct {
		cron string
		want bool
	}{{"0 0 3 * * *", true}, {"0 0 3 * * 0", false}, {"0 3 * * *", false}} {
		if got := cronDaily(tt.cron); got != tt.want {
			t.Errorf("cronDaily(%q) = %v, want %v", tt.cron, got, tt.want)
		}
	}
}

// rule: ARC-U-009
func TestARC_U_009_Encryption(t *testing.T) {
	c, objs := pgCluster(t, "alpha")
	cm := byName(objs, "ConfigMap")["counsel-alpha-config"]
	cellKey := dig(cm, "data", "COUNSEL_KMS_KEY")
	scName, _ := dig(c, "spec", "storage", "storageClass").(string)
	sc := byName(objs, "StorageClass")[scName]
	store := dig(c, "spec", "backup", "barmanObjectStore")
	hba, _ := dig(c, "spec", "postgresql", "pg_hba").([]any)
	hbaOK := len(hba) > 0
	for _, l := range hba {
		f := strings.Fields(fmt.Sprint(l))
		// Only TLS entries may accept; a non-TLS entry must reject.
		if len(f) == 0 || !(f[0] == "hostssl" || (f[0] == "hostnossl" && f[len(f)-1] == "reject")) {
			hbaOK = false
		}
	}
	runChecks(t, []check{
		{"cell KMS key set", cellKey != nil && cellKey != "", true},
		{"storage class exists", sc != nil, true},
		{"volumes encrypted", dig(sc, "parameters", "encrypted"), "true"},
		{"volume key is the cell key", dig(sc, "parameters", "kmsKeyId"), cellKey},
		{"WAL backup encrypted", dig(store, "wal", "encryption"), "aws:kms"},
		{"base backup encrypted", dig(store, "data", "encryption"), "aws:kms"},
		{"pg_hba hostssl only", hbaOK, true},
		{"TLS 1.2+", dig(c, "spec", "postgresql", "parameters", "ssl_min_protocol_version"), "TLSv1.2"},
	})
}

// rule: ARC-U-018
func TestARC_U_018_MigrationJobValues(t *testing.T) {
	m, ok := dig(loadValues(t), "migrate").(map[string]any)
	if !ok {
		t.Fatal("values.migrate missing")
	}
	hooks, _ := m["hooks"].([]any)
	runChecks(t, []check{
		{"enabled", m["enabled"], true},
		{"pre-upgrade hook", slices.Contains(hooks, any("pre-upgrade")), true},
		{"lock_timeout 2s", m["lockTimeout"], "2s"},
		{"retries", asInt(m["retries"]) >= 1, true},
	})
	// Backfills run as batched jobs, never inside the migration hook.
	for k := range m {
		if strings.Contains(strings.ToLower(k), "backfill") {
			t.Errorf("values.migrate.%s: no backfill inside the migration hook", k)
		}
	}
}

// rule: ARC-U-039
func TestARC_U_039_Rotation(t *testing.T) {
	objs := mustRender(t, map[string]any{"cell": "alpha"})
	ext := byName(objs, "ExternalSecret")
	keys := func(name string) []string {
		var out []string
		list, _ := dig(ext[name], "spec", "data").([]any)
		for _, d := range list {
			out = append(out, fmt.Sprint(dig(d, "secretKey")))
		}
		return out
	}
	twoValues := func(name string) bool {
		k := keys(name)
		return slices.Contains(k, "current") && slices.Contains(k, "previous")
	}
	vals := loadValues(t)
	roles, _ := dig(vals, "rotation", "dbRoles").([]any)
	roleKeys := keys("counsel-alpha-db-roles")
	cellKey := dig(byName(objs, "ConfigMap")["counsel-alpha-config"], "data", "COUNSEL_KMS_KEY")
	cert := byName(objs, "Certificate")["counsel-alpha-tls"]
	hours := func(v any) time.Duration {
		d, err := time.ParseDuration(fmt.Sprint(v))
		if err != nil {
			return 0
		}
		return d
	}
	runChecks(t, []check{
		{"webhook signing has two active values", twoValues("counsel-alpha-webhook-signing"), true},
		{"API tokens have two active values", twoValues("counsel-alpha-api-tokens"), true},
		{"two DB roles", len(roles) == 2 && roles[0] != roles[1], true},
		{"both DB roles synced", len(roleKeys) == 2 && slices.Contains(roleKeys, fmt.Sprint(roles[0])) && slices.Contains(roleKeys, fmt.Sprint(roles[1])), true},
		{"KMS key id is the cell key", dig(byName(objs, "ConfigMap")["counsel-alpha-kms"], "data", "key_id"), cellKey},
		{"key id in every ciphertext", dig(byName(objs, "ConfigMap")["counsel-alpha-kms"], "data", "key_id_in_ciphertext"), "true"},
		{"cert renews >= 30 days early", hours(dig(cert, "spec", "renewBefore")) >= 720*time.Hour, true},
		{"cert renews before expiry", hours(dig(cert, "spec", "renewBefore")) < hours(dig(cert, "spec", "duration")), true},
		{"cert alert at 14 days", hours(dig(cert, "metadata", "annotations", "counsel.io/expiry-alert-before")), 336 * time.Hour},
		{"cert is the ingress TLS secret", dig(cert, "spec", "secretName"), "counsel-alpha-tls"},
	})
}

// migrateJob renders the chart and returns the migration Job, or nil when there is none.
func migrateJob(t *testing.T, set map[string]any) k8sObj {
	t.Helper()
	var job k8sObj
	for _, o := range mustRender(t, set) {
		if o.kind() == "Job" && o.label("app.kubernetes.io/component") == "migrate" {
			if job != nil {
				t.Fatalf("more than one migration Job")
			}
			job = o
		}
	}
	return job
}

func containerEnv(c map[string]any, name string) any {
	list, _ := c["env"].([]any)
	for _, e := range list {
		if m, ok := e.(map[string]any); ok && m["name"] == name {
			if v, ok := m["value"]; ok {
				return v
			}
			return m["valueFrom"]
		}
	}
	return nil
}

// rule: MIG-061
// rule: ARC-U-018
func TestMIG_061_MigrationJob(t *testing.T) {
	job := migrateJob(t, map[string]any{"cell": "alpha"})
	if job == nil {
		t.Fatal("no migration Job rendered")
	}
	ann, _ := dig(job, "metadata", "annotations").(map[string]any)
	hooks, _ := ann["helm.sh/hook"].(string)
	cs := podContainers(job)
	if len(cs) != 1 {
		t.Fatalf("migration Job has %d containers, want 1", len(cs))
	}
	args, _ := cs[0]["args"].([]any)
	var argv []string
	for _, a := range args {
		argv = append(argv, fmt.Sprint(a))
	}
	runChecks(t, []check{
		{"namespace", job.meta("namespace"), "counsel-alpha"},
		{"pre-install hook", slices.Contains(strings.Split(hooks, ","), "pre-install"), true},
		{"pre-upgrade hook", slices.Contains(strings.Split(hooks, ","), "pre-upgrade"), true},
		{"hook-delete-policy", ann["helm.sh/hook-delete-policy"], "before-hook-creation"},
		{"backoffLimit >= 3", asInt(dig(job, "spec", "backoffLimit")) >= 3, true},
		{"restartPolicy", dig(job, "spec", "template", "spec", "restartPolicy"), "Never"},
		{"runs counsel-migrate", len(argv) > 0 && argv[0] == "counsel-migrate", true},
		{"expand only", slices.Contains(argv, "-mode=expand"), true},
		{"no backfill in the hook", strings.Contains(strings.ToLower(strings.Join(argv, " ")), "backfill"), false},
		{"PGOPTIONS lock_timeout 2s", containerEnv(cs[0], "PGOPTIONS"), "-c lock_timeout=2s"},
		{"database url from cell secret", dig(containerEnv(cs[0], "COUNSEL_DATABASE_URL"), "secretKeyRef", "name"), "counsel-alpha-pg-app"},
		{"app priority class", dig(job, "spec", "template", "spec", "priorityClassName"), "counsel-alpha-app"},
	})
}

// rule: ARC-U-018
func TestMIG_061_MigrationJobValues(t *testing.T) {
	tests := []struct {
		name    string
		set     map[string]any
		wantJob bool
		wantErr string
	}{
		{"default", map[string]any{"cell": "alpha"}, true, ""},
		{"disabled", map[string]any{"cell": "alpha", "migrate.enabled": false}, false, ""},
		{"too few retries", map[string]any{"cell": "alpha", "migrate.retries": 2}, false, "at least 3"},
		{"three retries", map[string]any{"cell": "alpha", "migrate.retries": 3}, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objs, err := renderChart(t, tt.set)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			got := false
			for _, o := range objs {
				got = got || (o.kind() == "Job" && o.label("app.kubernetes.io/component") == "migrate")
			}
			if got != tt.wantJob {
				t.Fatalf("migration Job rendered = %v, want %v", got, tt.wantJob)
			}
		})
	}
}
