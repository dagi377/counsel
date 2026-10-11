package metrics_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/kernel/metrics"
	"github.com/dagi377/counsel/kernel/metrics/metricsfake"
)

const sloRulesFile = "../../deploy/observability/slo-rules.yaml"

type promRule struct {
	Record string            `yaml:"record"`
	Alert  string            `yaml:"alert"`
	Expr   string            `yaml:"expr"`
	Labels map[string]string `yaml:"labels"`
}

func loadRules(t *testing.T) []promRule {
	t.Helper()
	raw, err := os.ReadFile(sloRulesFile)
	if err != nil {
		t.Fatalf("read slo rules: %v", err)
	}
	var doc struct {
		Groups []struct {
			Rules []promRule `yaml:"rules"`
		} `yaml:"groups"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse slo rules: %v", err)
	}
	var out []promRule
	for _, g := range doc.Groups {
		out = append(out, g.Rules...)
	}
	return out
}

func findRule(rules []promRule, slo, name string) (promRule, bool) {
	for _, r := range rules {
		if r.Labels["slo"] == slo && (r.Record == name || r.Alert == name) {
			return r, true
		}
	}
	return promRule{}, false
}

// rule: ARC-U-034
func TestARC_U_034_EverySLOHasBurnRateRulesAndAlerts(t *testing.T) {
	rules := loadRules(t)
	slos := []string{"SLO-01", "SLO-02", "SLO-03", "SLO-04", "SLO-05", "SLO-05b", "SLO-06", "SLO-07", "SLO-08", "SLO-09", "SLO-10"}
	for _, slo := range slos {
		t.Run(slo, func(t *testing.T) {
			for _, rec := range []string{"slo:error_ratio:rate5m", "slo:error_ratio:rate30m", "slo:error_ratio:rate1h", "slo:error_ratio:rate6h", "slo:error_ratio:rate3d"} {
				if _, ok := findRule(rules, slo, rec); !ok {
					t.Errorf("missing recording rule %s", rec)
				}
			}
			tests := []struct {
				alert, factor string
				windows       []string
			}{
				{"SLOFastBurn", "14.4", []string{"rate1h", "rate5m"}},
				{"SLOSlowBurn", "6", []string{"rate6h", "rate30m"}},
				{"SLOBudgetBurn", "1", []string{"rate3d"}},
			}
			for _, tt := range tests {
				r, ok := findRule(rules, slo, tt.alert)
				if !ok {
					t.Errorf("missing alert %s", tt.alert)
					continue
				}
				if r.Labels["burn_rate"] != tt.factor {
					t.Errorf("%s burn_rate = %q, want %q", tt.alert, r.Labels["burn_rate"], tt.factor)
				}
				for _, w := range tt.windows {
					if !strings.Contains(r.Expr, w) {
						t.Errorf("%s expr lacks window %s: %s", tt.alert, w, r.Expr)
					}
				}
			}
		})
	}
	if len(metrics.SLIMetrics) == 0 {
		t.Fatal("no SLI metrics declared")
	}
}

// rule: ARC-U-029
func TestARC_U_029_AgentQueueDoesNotBurnWhileDependencyDown(t *testing.T) {
	rules := loadRules(t)
	tests := []struct {
		name, slo, rule, want string
	}{
		{"SLO-08 ratio excludes outage", "SLO-08", "slo:error_ratio:rate1h", `dependency_up{dependency="agent_platform"} == 0`},
		{"queued jobs ticket while down", "SLO-08", "AgentJobsQueuedWhileDown", "job_queue_age_seconds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, ok := findRule(rules, tt.slo, tt.rule)
			if !ok {
				t.Fatalf("missing %s", tt.rule)
			}
			if !strings.Contains(r.Expr, tt.want) {
				t.Fatalf("expr %q lacks %q", r.Expr, tt.want)
			}
		})
	}
}

// rule: ARC-U-036
// rule: ARC-U-037
func TestARC_U_036_ARC_U_037_AlertsMapToStatusComponentAndSeverity(t *testing.T) {
	for _, r := range loadRules(t) {
		if r.Alert == "" {
			continue
		}
		t.Run(r.Labels["slo"]+"/"+r.Alert, func(t *testing.T) {
			if !slices.Contains(metrics.StatusComponents, r.Labels["status_component"]) {
				t.Errorf("status_component %q not a status page component", r.Labels["status_component"])
			}
			if !slices.Contains(metrics.IncidentSeverities, r.Labels["incident_severity"]) {
				t.Errorf("incident_severity %q unknown", r.Labels["incident_severity"])
			}
			if r.Alert == "SLOFastBurn" && r.Labels["severity"] == "page" && r.Labels["incident_severity"] != "SEV2" {
				t.Errorf("fast burn page must be SEV2")
			}
		})
	}
}

func TestARC_U_036_StatusComponents(t *testing.T) {
	for _, c := range []string{"app", "documents", "payments", "e-sign", "email", "agent"} {
		if !slices.Contains(metrics.StatusComponents, c) {
			t.Errorf("missing component %s", c)
		}
	}
}

func TestARC_U_037_IncidentSeverities(t *testing.T) {
	if !slices.Equal(metrics.IncidentSeverities, []string{"SEV1", "SEV2", "SEV3"}) {
		t.Fatalf("severities = %v", metrics.IncidentSeverities)
	}
}

func okStep(context.Context) error { return nil }

// rule: ARC-U-035
func TestARC_U_035_SyntheticLogsInEvery60s(t *testing.T) {
	if metrics.SyntheticInterval != 60*time.Second {
		t.Fatalf("interval = %v, want 60s", metrics.SyntheticInterval)
	}
	tests := []struct {
		name      string
		loginErr  error
		ticks     int
		wantLogin int
		wantNames []string
	}{
		{"login then steps each tick", nil, 3, 3, []string{"login", "list", "entry", "document"}},
		{"failed login skips steps", errors.New("denied"), 2, 2, []string{"login"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logins := 0
			s := &metrics.Synthetic{
				Location: "external",
				User:     metrics.SyntheticUser{Login: "synthetic@example.test", Secret: "s3cret"},
				Login: func(_ context.Context, u metrics.SyntheticUser) error {
					logins++
					return tt.loginErr
				},
				Steps:    []metrics.SyntheticStep{{"list", okStep}, {"entry", okStep}, {"document", okStep}},
				Recorder: metricsfake.New(),
			}
			var names []string
			for _, r := range s.RunOnce(context.Background()) {
				names = append(names, r.Name)
			}
			if !slices.Equal(names, tt.wantNames) {
				t.Fatalf("names = %v, want %v", names, tt.wantNames)
			}
			logins = 0
			tick := make(chan time.Time)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { s.Run(ctx, tick); close(done) }()
			for range tt.ticks {
				tick <- time.Time{}
			}
			tick <- time.Time{} // blocks until the previous run finished
			cancel()
			<-done
			if logins != tt.wantLogin+1 {
				t.Fatalf("logins = %d, want %d", logins, tt.wantLogin+1)
			}
		})
	}
}

// rule: ARC-U-031
func TestARC_U_031_SyntheticStepTimesOut(t *testing.T) {
	if metrics.SyntheticStepTimeout > 10*time.Second {
		t.Fatalf("step timeout %v over 10 s", metrics.SyntheticStepTimeout)
	}
	tests := []struct {
		name   string
		step   metrics.CheckFunc
		wantOK bool
	}{
		{"fast step passes", okStep, true},
		{"hung step fails at timeout", func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := metricsfake.New()
			s := &metrics.Synthetic{
				Login:       func(context.Context, metrics.SyntheticUser) error { return nil },
				Steps:       []metrics.SyntheticStep{{"step", tt.step}},
				Recorder:    rec,
				StepTimeout: 20 * time.Millisecond,
			}
			start := time.Now()
			res := s.RunOnce(context.Background())
			if time.Since(start) > time.Second {
				t.Fatal("step did not stop at timeout")
			}
			if res[1].OK != tt.wantOK {
				t.Fatalf("ok = %v, want %v", res[1].OK, tt.wantOK)
			}
			wantFailed := 0
			if !tt.wantOK {
				wantFailed = 1
			}
			if got := rec.Count(metrics.SyntheticFailedMetric); got != wantFailed {
				t.Fatalf("failed count = %d, want %d", got, wantFailed)
			}
		})
	}
}

// rule: ARC-U-040
func TestARC_U_040_SyntheticSecretNeverLogged(t *testing.T) {
	const secret = "tok-ZZCANARY-SECRET"
	tests := []struct {
		name string
		err  error
	}{
		{"login error echoes secret", errors.New("bad credentials " + secret)},
		{"login passes", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
			defer slog.SetDefault(prev)
			u := metrics.SyntheticUser{Login: "synthetic", Secret: secret}
			s := &metrics.Synthetic{
				User:  u,
				Login: func(context.Context, metrics.SyntheticUser) error { return tt.err },
			}
			s.RunOnce(context.Background())
			slog.Info("user", "u", u)
			if strings.Contains(buf.String(), secret) || strings.Contains(u.String(), secret) {
				t.Fatalf("secret leaked: %s", buf.String())
			}
		})
	}
}
