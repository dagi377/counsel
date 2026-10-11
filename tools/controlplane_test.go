package tools

import (
	"os"
	"regexp"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	controlPlaneFile = "../deploy/controlplane/config.yaml"
	wafFile          = "../deploy/helm/counsel/templates/waf.yaml"
)

type controlPlaneConfig struct {
	Components []string `yaml:"components"`
	Telemetry  struct {
		AllowedFields []string `yaml:"allowed_fields"`
	} `yaml:"telemetry"`
	Postgres struct {
		SynchronousStandbys int `yaml:"synchronous_standbys"`
		OnOneLost           struct {
			Alert string `yaml:"alert"`
		} `yaml:"on_one_standby_lost"`
		OnBothLost struct {
			Alert              string `yaml:"alert"`
			AlertWithinSeconds int    `yaml:"alert_within_seconds"`
			Writes             string `yaml:"writes"`
			Resume             string `yaml:"resume"`
			AsyncFallback      *bool  `yaml:"async_fallback"`
		} `yaml:"on_both_standbys_lost"`
	} `yaml:"postgres"`
}

type wafRateRule struct {
	Key           string `yaml:"key"`
	Limit         int    `yaml:"limit"`
	WindowSeconds int    `yaml:"window_seconds"`
	Status        int    `yaml:"status"`
	RetryAfter    bool   `yaml:"retry_after"`
}

func loadControlPlane(t *testing.T) (controlPlaneConfig, []byte) {
	t.Helper()
	raw, err := os.ReadFile(controlPlaneFile)
	if err != nil {
		t.Fatalf("read control plane config: %v", err)
	}
	var c controlPlaneConfig
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatalf("parse control plane config: %v", err)
	}
	return c, raw
}

// rule: ARC-U-004
func TestARC_U_004_ControlPlaneCarriesNoCustomerData(t *testing.T) {
	c, raw := loadControlPlane(t)
	// Field names that would mean customer data reached the shared control plane.
	forbidden := regexp.MustCompile(`(?i)\b(name|email|phone|address|document|doc_text|prompt|amount|note|body)\b`)
	tests := []struct {
		name string
		text string
	}{
		{name: "whole file", text: string(raw)},
	}
	for _, f := range c.Telemetry.AllowedFields {
		tests = append(tests, struct {
			name string
			text string
		}{name: "telemetry field " + f, text: f})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if m := forbidden.FindString(tt.text); m != "" {
				t.Fatalf("control plane config mentions customer data field %q", m)
			}
		})
	}
	if !slices.Contains(c.Telemetry.AllowedFields, "cell_id") {
		t.Fatal("telemetry must carry cell_id")
	}
}

// rule: ARC-U-007
func TestARC_U_007_BothStandbysLostNeedsOperator(t *testing.T) {
	c, _ := loadControlPlane(t)
	pg := c.Postgres
	tests := []struct {
		name      string
		got, want any
	}{
		{"two sync standbys", pg.SynchronousStandbys, 2},
		{"one lost alert", pg.OnOneLost.Alert, "PGSyncStandbyDegraded"},
		{"both lost alert", pg.OnBothLost.Alert, "PGSyncStandbyMissing"},
		{"both lost alert within 60 s", pg.OnBothLost.AlertWithinSeconds <= 60, true},
		{"both lost blocks writes", pg.OnBothLost.Writes, "block"},
		{"resume needs operator", pg.OnBothLost.Resume, "operator_decision"},
		{"no async fallback", pg.OnBothLost.AsyncFallback != nil && !*pg.OnBothLost.AsyncFallback, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

// rule: ARC-U-038
func TestARC_U_038_WAFPerIPRateRule(t *testing.T) {
	raw, err := os.ReadFile(wafFile)
	if err != nil {
		t.Fatalf("read waf: %v", err)
	}
	var cm struct {
		Data map[string]string `yaml:"data"`
	}
	if err := yaml.Unmarshal(raw, &cm); err != nil {
		t.Fatalf("parse waf: %v", err)
	}
	var policy struct {
		ManagedRules bool          `yaml:"managed_rules"`
		RateRules    []wafRateRule `yaml:"rate_rules"`
	}
	if err := yaml.Unmarshal([]byte(cm.Data["waf.yaml"]), &policy); err != nil {
		t.Fatalf("parse waf policy: %v", err)
	}
	tests := []struct {
		name string
		want wafRateRule
	}{
		{"per-IP 2000 req / 5 min, 429 with Retry-After", wafRateRule{Key: "ip", Limit: 2000, WindowSeconds: 300, Status: 429, RetryAfter: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !policy.ManagedRules {
				t.Fatal("managed rules must be on")
			}
			if !slices.Contains(policy.RateRules, tt.want) {
				t.Fatalf("rate rules %+v do not contain %+v", policy.RateRules, tt.want)
			}
		})
	}
}
