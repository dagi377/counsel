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
		if h.Year < 2025 || h.Year > 2028 {
			t.Errorf("holiday %v is outside 2025 to 2028", h)
		}
	}
}

type holidayCase struct {
	name string
	date clock.Date
	want bool
}

func checkHolidays(t *testing.T, cases []holidayCase) {
	t.Helper()
	p := loadHolidays(t)
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHoliday(p, tt.date); got != tt.want {
				t.Fatalf("holiday %v = %v, want %v", tt.date, got, tt.want)
			}
		})
	}
}

func d(y int, m time.Month, day int) clock.Date { return clock.Date{Year: y, Month: m, Day: day} }

func TestOntarioHolidays2025(t *testing.T) {
	checkHolidays(t, []holidayCase{
		{"new year's day", d(2025, time.January, 1), true},
		{"family day", d(2025, time.February, 17), true},
		{"good friday", d(2025, time.April, 18), true},
		{"easter monday", d(2025, time.April, 21), true},
		{"victoria day", d(2025, time.May, 19), true},
		{"civic holiday", d(2025, time.August, 4), true},
		{"thanksgiving", d(2025, time.October, 13), true},
		{"boxing day", d(2025, time.December, 26), true},
		{"truth and reconciliation not counted (verify)", d(2025, time.September, 30), false},
		{"monday after christmas thursday", d(2025, time.December, 29), false},
	})
}

func TestOntarioHolidays2026SepThirty(t *testing.T) {
	checkHolidays(t, []holidayCase{
		{"truth and reconciliation not counted (verify)", d(2026, time.September, 30), false},
	})
}

func TestOntarioHolidays2027(t *testing.T) {
	checkHolidays(t, []holidayCase{
		{"new year's day", d(2027, time.January, 1), true},
		{"family day", d(2027, time.February, 15), true},
		{"good friday", d(2027, time.March, 26), true},
		{"easter monday", d(2027, time.March, 29), true},
		{"victoria day", d(2027, time.May, 24), true},
		{"labour day", d(2027, time.September, 6), true},
		{"thanksgiving", d(2027, time.October, 11), true},
		{"christmas on saturday: monday", d(2027, time.December, 27), true},
		{"christmas on saturday: tuesday", d(2027, time.December, 28), true},
		{"wednesday after christmas", d(2027, time.December, 29), false},
		{"truth and reconciliation not counted (verify)", d(2027, time.September, 30), false},
	})
}

func TestOntarioHolidays2028(t *testing.T) {
	checkHolidays(t, []holidayCase{
		{"new year's on saturday: monday", d(2028, time.January, 3), true},
		{"family day", d(2028, time.February, 21), true},
		{"good friday", d(2028, time.April, 14), true},
		{"easter monday", d(2028, time.April, 17), true},
		{"victoria day", d(2028, time.May, 22), true},
		{"canada day on saturday: monday", d(2028, time.July, 3), true},
		{"civic holiday", d(2028, time.August, 7), true},
		{"thanksgiving", d(2028, time.October, 9), true},
		{"remembrance day on saturday: monday", d(2028, time.November, 13), true},
		{"boxing day", d(2028, time.December, 26), true},
		{"tuesday after boxing day", d(2028, time.December, 27), false},
	})
}

// Each of the 13 named holidays lands on a weekday or moves to a substitute weekday; with
// Christmas and Boxing Day sharing substitutes, each covered year has 12 weekday closures.
func TestOntarioHolidaysWeekdayCountPerYear(t *testing.T) {
	p := loadHolidays(t)
	want := map[int]int{2025: 12, 2026: 12, 2027: 12, 2028: 12}
	got := map[int]int{}
	for _, h := range p.Holidays {
		wd := time.Date(h.Year, h.Month, h.Day, 0, 0, 0, 0, time.UTC).Weekday()
		if wd != time.Saturday {
			got[h.Year]++
		}
	}
	for y, n := range want {
		t.Run(time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006"), func(t *testing.T) {
			if got[y] != n {
				t.Fatalf("weekday court holidays in %d = %d, want %d", y, got[y], n)
			}
		})
	}
}
