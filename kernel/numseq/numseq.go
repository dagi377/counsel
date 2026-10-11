package numseq

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
)

var _ Sequencer = (*Postgres)(nil)

var tokenRE = regexp.MustCompile(`\{[^}]*\}`)

const yearToken = "{YYYY}"

// Postgres is the production Sequencer. Series definitions are app data registered at boot
// and kept in memory; counters live in core.number_series, one row per (series, period_key).
type Postgres struct {
	clk    clock.Clock
	mu     sync.RWMutex
	series map[string]string
}

// New returns a Postgres sequencer that takes the business year from clk.
func New(clk clock.Clock) *Postgres {
	return &Postgres{clk: clk, series: map[string]string{}}
}

// Define registers s. Redefining a name with the same format is a no-op.
func (p *Postgres) Define(_ context.Context, s Series) error {
	if s.Name == "" {
		return fmt.Errorf("define: empty series name: %w", ErrBadFormat)
	}
	if err := validate(s.Format); err != nil {
		return fmt.Errorf("define %s: %w", s.Name, err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if prev, ok := p.series[s.Name]; ok && prev != s.Format {
		return fmt.Errorf("define %s: %w", s.Name, ErrConflict)
	}
	p.series[s.Name] = s.Format
	return nil
}

// Next issues the next number of series inside tx. The counter row is locked with
// SELECT FOR UPDATE until tx ends, so concurrent callers queue and a rollback returns the
// number to the series.
func (p *Postgres) Next(ctx context.Context, tx pgx.Tx, series string) (string, error) {
	p.mu.RLock()
	format, ok := p.series[series]
	p.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("next %s: %w", series, ErrUnknownSeries)
	}
	year := p.clk.Today(ctx).Year
	period := ""
	if strings.Contains(format, yearToken) {
		period = fmt.Sprintf("%04d", year)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO core.number_series (series, period_key) VALUES ($1, $2) ON CONFLICT (series, period_key) DO NOTHING`,
		series, period); err != nil {
		return "", fmt.Errorf("next %s: ensure counter: %w", series, err)
	}
	var n int64
	if err := tx.QueryRow(ctx,
		`SELECT next_value FROM core.number_series WHERE series = $1 AND period_key = $2 FOR UPDATE`,
		series, period).Scan(&n); err != nil {
		return "", fmt.Errorf("next %s: lock counter: %w", series, err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE core.number_series SET next_value = next_value + 1 WHERE series = $1 AND period_key = $2`,
		series, period); err != nil {
		return "", fmt.Errorf("next %s: advance counter: %w", series, err)
	}
	return render(format, year, n), nil
}

// render expands the format tokens for counter n in business year year.
func render(format string, year int, n int64) string {
	return tokenRE.ReplaceAllStringFunc(format, func(tok string) string {
		if tok == yearToken {
			return fmt.Sprintf("%04d", year)
		}
		w, _ := seqWidth(tok)
		return fmt.Sprintf("%0*d", w, n)
	})
}

func seqWidth(tok string) (int, bool) {
	if !strings.HasPrefix(tok, "{SEQ:") {
		return 0, false
	}
	w, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(tok, "{SEQ:"), "}"))
	if err != nil || w < 1 || w > 18 {
		return 0, false
	}
	return w, true
}

// validate checks the format has exactly one {SEQ:N} token and no unknown tokens.
func validate(format string) error {
	seq := 0
	for _, tok := range tokenRE.FindAllString(format, -1) {
		if tok == yearToken {
			continue
		}
		if _, ok := seqWidth(tok); !ok {
			return fmt.Errorf("token %s: %w", tok, ErrBadFormat)
		}
		seq++
	}
	if seq != 1 {
		return fmt.Errorf("need exactly one {SEQ:N} token: %w", ErrBadFormat)
	}
	return nil
}
