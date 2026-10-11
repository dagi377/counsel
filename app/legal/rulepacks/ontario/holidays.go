package ontario

import (
	_ "embed"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/kernel/clock"
)

//go:embed holidays.yaml
var holidaysYAML []byte

// Holidays returns the Ontario court holiday calendar from holidays.yaml (Rules of Civil
// Procedure r. 1.03 and Legislation Act, 2006 s. 88), in file order. The calendar covers
// only the years the file lists. It is unverified law: callers use it only behind the
// deadline.ontario_pack flag.
func Holidays() ([]clock.Date, error) {
	var f struct {
		TimeZone string   `yaml:"time_zone"`
		Holidays []string `yaml:"holidays"`
	}
	if err := yaml.Unmarshal(holidaysYAML, &f); err != nil {
		return nil, fmt.Errorf("ontario holidays: %w", err)
	}
	if f.TimeZone != clock.DefaultZone {
		return nil, fmt.Errorf("ontario holidays: time_zone %q, want %q", f.TimeZone, clock.DefaultZone)
	}
	out := make([]clock.Date, 0, len(f.Holidays))
	for _, h := range f.Holidays {
		d, err := time.Parse(time.DateOnly, h)
		if err != nil {
			return nil, fmt.Errorf("ontario holidays: %q: %w", h, err)
		}
		out = append(out, clock.Date{Year: d.Year(), Month: d.Month(), Day: d.Day()})
	}
	return out, nil
}
