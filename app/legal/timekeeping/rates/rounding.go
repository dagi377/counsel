package rates

import "fmt"

// roundMinutes rounds raw minutes to the entry's increment at save. Mode up rounds any
// part increment up; mode nearest rounds half up. Bills never re-round.
func roundMinutes(raw int, r Rounding) (int, error) {
	// rule: BIL-C-010
	if r.IncrementMinutes != 1 && r.IncrementMinutes != 6 && r.IncrementMinutes != 15 {
		return 0, fmt.Errorf("round %d min: increment %d: %w", raw, r.IncrementMinutes, ErrBadRounding)
	}
	if raw < 0 {
		return 0, fmt.Errorf("round %d min: %w", raw, ErrBadRounding)
	}
	inc := r.IncrementMinutes
	// rule: BIL-C-010
	switch r.Mode {
	case ModeUp:
		return (raw + inc - 1) / inc * inc, nil
	case ModeNearest:
		return (2*raw + inc) / (2 * inc) * inc, nil
	}
	return 0, fmt.Errorf("round %d min: mode %q: %w", raw, r.Mode, ErrBadRounding)
}
