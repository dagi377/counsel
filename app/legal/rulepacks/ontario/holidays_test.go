package ontario

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"
)

const holidaysFile = "holidays.yaml"

type packFile struct {
	Pack     string   `yaml:"pack"`
	Version  int      `yaml:"version"`
	TimeZone string   `yaml:"time_zone"`
	Holidays []string `yaml:"holidays"`
	Rules    []struct {
		ID         string `yaml:"id"`
		Trigger    string `yaml:"trigger"`
		OffsetDays int    `yaml:"offset_days"`
		DayCount   string `yaml:"day_count"`
		Roll       string `yaml:"roll"`
	} `yaml:"rules"`
}

// loadHolidays reads holidays.yaml, validates it against the rule-pack schema and returns
// it as a deadline.Pack.
func loadHolidays(t *testing.T) deadline.Pack {
	t.Helper()
	raw, err := os.ReadFile(holidaysFile)
	if err != nil {
		t.Fatalf("read %s: %v", holidaysFile, err)
	}

	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", holidaysFile, err)
	}
	js, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("to json: %v", err)
	}
	var v any
	if err := json.Unmarshal(js, &v); err != nil {
		t.Fatalf("from json: %v", err)
	}
	schemaPath := filepath.Join("..", "..", "..", "..", "spec", "kernel", "rulepack.schema.json")
	schemaRaw, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	c.AssertFormat = true
	const url = "file:///spec/kernel/rulepack.schema.json"
	if err := c.AddResource(url, bytes.NewReader(schemaRaw)); err != nil {
		t.Fatalf("add schema: %v", err)
	}
	s, err := c.Compile(url)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}
	if err := s.Validate(v); err != nil {
		t.Fatalf("%s fails rulepack schema: %v", holidaysFile, err)
	}

	var f packFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		t.Fatalf("decode %s: %v", holidaysFile, err)
	}
	if f.TimeZone != "America/Toronto" {
		t.Fatalf("time_zone = %q, want America/Toronto", f.TimeZone)
	}
	p := deadline.Pack{ID: f.Pack, Version: "1"}
	for _, r := range f.Rules {
		p.Rules = append(p.Rules, deadline.Rule{
			ID: r.ID, Trigger: r.Trigger, Mode: deadline.DayCount(r.DayCount),
			Days: r.OffsetDays, Roll: deadline.Roll(r.Roll),
		})
	}
	for _, h := range f.Holidays {
		d, err := time.Parse(time.DateOnly, h)
		if err != nil {
			t.Fatalf("holiday %q: %v", h, err)
		}
		p.Holidays = append(p.Holidays, clock.Date{Year: d.Year(), Month: d.Month(), Day: d.Day()})
	}
	return p
}

func isHoliday(p deadline.Pack, d clock.Date) bool {
	for _, h := range p.Holidays {
		if h == d {
			return true
		}
	}
	return false
}

func TestOntarioHolidays2026(t *testing.T) {
	p := loadHolidays(t)
	tests := []struct {
		name string
		date clock.Date
		want bool
	}{
		{"monday after christmas on a friday", clock.Date{Year: 2026, Month: time.December, Day: 28}, true},
		{"christmas day", clock.Date{Year: 2026, Month: time.December, Day: 25}, true},
		{"boxing day", clock.Date{Year: 2026, Month: time.December, Day: 26}, true},
		{"thanksgiving", clock.Date{Year: 2026, Month: time.October, Day: 12}, true},
		{"good friday", clock.Date{Year: 2026, Month: time.April, Day: 3}, true},
		{"ordinary tuesday", clock.Date{Year: 2026, Month: time.December, Day: 29}, false},
		{"day after thanksgiving", clock.Date{Year: 2026, Month: time.October, Day: 13}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHoliday(p, tt.date); got != tt.want {
				t.Fatalf("holiday %v = %v, want %v", tt.date, got, tt.want)
			}
		})
	}
}

func TestOntarioHolidaysShape(t *testing.T) {
	p := loadHolidays(t)
	if len(p.Holidays) == 0 {
		t.Fatal("no holidays loaded")
	}
	for _, h := range p.Holidays {
		wd := time.Date(h.Year, h.Month, h.Day, 0, 0, 0, 0, time.UTC).Weekday()
		if wd == time.Sunday {
			t.Errorf("holiday %v is a Sunday; weekends need not be listed", h)
		}
		if h.Year != 2026 {
			t.Errorf("holiday %v is outside 2026", h)
		}
	}
}
