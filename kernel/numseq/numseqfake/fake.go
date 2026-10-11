// Package numseqfake is an in-memory numseq.Sequencer. It ignores the transaction.
package numseqfake

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/numseq"
)

var _ numseq.Sequencer = (*Fake)(nil)

var tokenRE = regexp.MustCompile(`\{[^}]*\}`)

// Fake keeps one counter per (series, business year) in memory.
type Fake struct {
	mu     sync.Mutex
	clk    clock.Clock
	series map[string]string
	counts map[string]int
}

// New returns a Fake that reads the business year from clk.
func New(clk clock.Clock) *Fake {
	return &Fake{clk: clk, series: map[string]string{}, counts: map[string]int{}}
}

// Define registers s.
func (f *Fake) Define(_ context.Context, s numseq.Series) error {
	if err := validate(s.Format); err != nil {
		return fmt.Errorf("define %s: %w", s.Name, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if prev, ok := f.series[s.Name]; ok && prev != s.Format {
		return fmt.Errorf("define %s: %w", s.Name, numseq.ErrConflict)
	}
	f.series[s.Name] = s.Format
	return nil
}

// Next returns the next number of series; tx may be nil.
func (f *Fake) Next(ctx context.Context, _ pgx.Tx, series string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	format, ok := f.series[series]
	if !ok {
		return "", fmt.Errorf("next %s: %w", series, numseq.ErrUnknownSeries)
	}
	year := f.clk.Today(ctx).Year
	key := series
	if strings.Contains(format, "{YYYY}") {
		key = series + "/" + strconv.Itoa(year)
	}
	f.counts[key]++
	n := f.counts[key]
	return tokenRE.ReplaceAllStringFunc(format, func(tok string) string {
		if tok == "{YYYY}" {
			return fmt.Sprintf("%04d", year)
		}
		w, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(tok, "{SEQ:"), "}"))
		return fmt.Sprintf("%0*d", w, n)
	}), nil
}

func validate(format string) error {
	seq := 0
	for _, tok := range tokenRE.FindAllString(format, -1) {
		switch {
		case tok == "{YYYY}":
		case strings.HasPrefix(tok, "{SEQ:"):
			w, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(tok, "{SEQ:"), "}"))
			if err != nil || w < 1 || w > 18 {
				return numseq.ErrBadFormat
			}
			seq++
		default:
			return numseq.ErrBadFormat
		}
	}
	if seq != 1 {
		return numseq.ErrBadFormat
	}
	return nil
}
