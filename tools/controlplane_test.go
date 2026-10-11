package tools

import (
	"os"
	"path/filepath"
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

type rolloutConfig struct {
	Rollout struct {
		Rings []struct {
			Ring              int      `yaml:"ring"`
			Cells             []string `yaml:"cells"`
			MaxPercentOfCells int      `yaml:"max_percent_of_cells"`
			BakeHours         int      `yaml:"bake_hours"`
		} `yaml:"rings"`
		Halt struct {
			BurnRateAbove      int      `yaml:"burn_rate_above"`
			BurnWindowMinutes  int      `yaml:"burn_window_minutes"`
			IncidentSeverities []string `yaml:"incident_severities"`
			Action             string   `yaml:"action"`
		} `yaml:"halt"`
		Rollback struct {
			Method     string `yaml:"method"`
			Migrations string `yaml:"migrations"`
		} `yaml:"rollback"`
	} `yaml:"rollout"`
	RestoreDrills struct {
		EveryCell struct {
			Interval   string `yaml:"interval"`
			MinPerYear int    `yaml:"min_per_year"`
		} `yaml:"every_cell"`
		RandomCell struct {
			Interval string `yaml:"interval"`
			Count    int    `yaml:"count"`
		} `yaml:"random_cell"`
		TargetNamespace string `yaml:"target_namespace"`
		Pass            struct {
			MaxWallMinutes      int  `yaml:"max_wall_minutes"`
			MaxDataGapMinutes   int  `yaml:"max_data_gap_minutes"`
			IntegrityMismatches *int `yaml:"integrity_mismatches"`
		} `yaml:"pass"`
	} `yaml:"restore_drills"`
}

func loadRollout(t *testing.T) rolloutConfig {
	t.Helper()
	_, raw := loadControlPlane(t)
	var c rolloutConfig
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatalf("parse rollout config: %v", err)
	}
	return c
}

// rule: ARC-U-015
func TestARC_U_015_Rings(t *testing.T) {
	r := loadRollout(t).Rollout
	if len(r.Rings) != 3 {
		t.Fatalf("got %d rings, want 3", len(r.Rings))
	}
	tests := []struct {
		name      string
		got, want any
	}{
		{"ring order", [3]int{r.Rings[0].Ring, r.Rings[1].Ring, r.Rings[2].Ring}, [3]int{0, 1, 2}},
		{"ring 0 is staging + demo", slices.Equal(r.Rings[0].Cells, []string{"staging", "demo"}), true},
		{"ring 0 bakes 2 h", r.Rings[0].BakeHours, 2},
		{"ring 1 <= 10% of cells", r.Rings[1].MaxPercentOfCells > 0 && r.Rings[1].MaxPercentOfCells <= 10, true},
		{"ring 1 bakes 24 h", r.Rings[1].BakeHours, 24},
		{"ring 2 is the rest", slices.Equal(r.Rings[2].Cells, []string{"rest"}), true},
		{"halt on burn rate > 6x", r.Halt.BurnRateAbove, 6},
		{"burn window 30 min", r.Halt.BurnWindowMinutes, 30},
		{"halt on SEV1", slices.Contains(r.Halt.IncidentSeverities, "SEV1"), true},
		{"halt on SEV2", slices.Contains(r.Halt.IncidentSeverities, "SEV2"), true},
		{"halt rolls back", r.Halt.Action, "halt_and_rollback"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

// downMigration matches any step that would roll a migration back.
var downMigration = regexp.MustCompile(`(?i)down[-_ ]?migrat|migrat\w*[-_ ]down|\.down\.sql|rollback[-_ ]migrat|migrate\s+(down|rollback)`)

// rule: ARC-U-020
func TestARC_U_020_RollbackImageOnly(t *testing.T) {
	r := loadRollout(t).Rollout.Rollback
	if r.Method != "previous_image" {
		t.Errorf("rollback.method = %q, want previous_image", r.Method)
	}
	if r.Migrations != "forward_only" {
		t.Errorf("rollback.migrations = %q, want forward_only", r.Migrations)
	}
	files := []string{controlPlaneFile}
	charts, err := filepath.Glob("../deploy/helm/counsel/*/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, charts...)
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if m := downMigration.FindString(string(raw)); m != "" {
				t.Fatalf("down-migration step %q in deploy config", m)
			}
		})
	}
	for _, tt := range []struct {
		text string
		want bool
	}{
		{"run: counsel-migrate down", true},
		{"step: migrate down 1", true},
		{"step: down_migration", true},
		{"file: 0002_x.down.sql", true},
		{"method: previous_image", false},
		{"never silently downgrade", false},
	} {
		if got := downMigration.MatchString(tt.text); got != tt.want {
			t.Errorf("downMigration(%q) = %v, want %v", tt.text, got, tt.want)
		}
	}
}

// rule: ARC-U-010
func TestARC_U_010_DrillSchedule(t *testing.T) {
	d := loadRollout(t).RestoreDrills
	perYear := map[string]int{"monthly": 12, "quarterly": 4, "semiannual": 2, "yearly": 1}
	tests := []struct {
		name      string
		got, want any
	}{
		{"every cell quarterly", d.EveryCell.Interval, "quarterly"},
		{"every cell >= twice a year", d.EveryCell.MinPerYear >= 2 && perYear[d.EveryCell.Interval] >= d.EveryCell.MinPerYear, true},
		{"one random cell monthly", d.RandomCell.Interval, "monthly"},
		{"random cell count 1", d.RandomCell.Count, 1},
		{"restore into scratch namespace", d.TargetNamespace, "scratch"},
		{"wall time <= 45 min", d.Pass.MaxWallMinutes > 0 && d.Pass.MaxWallMinutes <= 45, true},
		{"data within 5 min", d.Pass.MaxDataGapMinutes > 0 && d.Pass.MaxDataGapMinutes <= 5, true},
		{"zero integrity mismatches", d.Pass.IntegrityMismatches != nil && *d.Pass.IntegrityMismatches == 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}
