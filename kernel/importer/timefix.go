package importer

import (
	"errors"
	"fmt"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
)

// ErrBadTime is returned for a staged time value that is not a zoned instant or a plain date.
var ErrBadTime = errors.New("importer: bad time value")

// ParseInstant parses an RFC 3339 timestamp and returns it in UTC. A timestamp without an
// offset is ambiguous and is refused rather than guessed.
//
// rule: MIG-006
func ParseInstant(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: instant %q", ErrBadTime, s)
	}
	return t.UTC(), nil
}

// ParseDate parses a date-only value (YYYY-MM-DD) as a civil date. It is never shifted by a
// time zone; a value carrying a time part is refused.
//
// rule: MIG-006
func ParseDate(s string) (clock.Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return clock.Date{}, fmt.Errorf("%w: date %q", ErrBadTime, s)
	}
	y, m, d := t.Date()
	return clock.Date{Year: y, Month: m, Day: d}, nil
}

// DerivedDate returns the business date of an instant in loc (the business time zone).
//
// rule: MIG-006
func DerivedDate(t time.Time, loc *time.Location) clock.Date {
	return clock.DateOf(t, loc)
}

// FixTimes returns a copy of r whose instant fields hold UTC time.Time values and whose
// date fields hold clock.Date values. Missing or nil fields are left as they are. The input
// row is not modified.
//
// rule: MIG-006
func FixTimes(r Row, instants, dates []string) (Row, error) {
	out := r
	out.Fields = make(map[string]any, len(r.Fields))
	for k, v := range r.Fields {
		out.Fields[k] = v
	}
	for _, f := range instants {
		s, ok, err := stringField(r, f)
		if err != nil || !ok {
			if err != nil {
				return Row{}, err
			}
			continue
		}
		t, err := ParseInstant(s)
		if err != nil {
			return Row{}, fmt.Errorf("%s:%s field %s: %w", r.Resource, r.ExternalID, f, err)
		}
		out.Fields[f] = t
	}
	for _, f := range dates {
		s, ok, err := stringField(r, f)
		if err != nil || !ok {
			if err != nil {
				return Row{}, err
			}
			continue
		}
		d, err := ParseDate(s)
		if err != nil {
			return Row{}, fmt.Errorf("%s:%s field %s: %w", r.Resource, r.ExternalID, f, err)
		}
		out.Fields[f] = d
	}
	return out, nil
}

func stringField(r Row, f string) (string, bool, error) {
	v, ok := r.Fields[f]
	if !ok || v == nil {
		return "", false, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", false, fmt.Errorf("%s:%s field %s: %w: not a string", r.Resource, r.ExternalID, f, ErrBadTime)
	}
	return s, true, nil
}
