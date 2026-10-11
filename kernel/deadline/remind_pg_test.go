package deadline_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/deadline/deadlinefake"
)

// sentTable answers PGSentLog's statements from an in-memory cal.deadline_reminder_sent,
// keyed like its unique (kind, deadline_id, user_id, day) with NULLS NOT DISTINCT. It
// outlives every Reminders built on it, as the table outlives a restart.
type sentTable struct {
	pgx.Tx
	rows map[string]bool
}

func sentRowKey(args []any) string {
	part := func(a any) string {
		if p, ok := a.(*string); ok && p != nil {
			return *p
		}
		return "NULL"
	}
	return fmt.Sprintf("%v|%s|%s|%v", args[0], part(args[1]), part(args[2]), args[3])
}

type boolRow struct{ v bool }

func (r boolRow) Scan(dest ...any) error { *(dest[0].(*bool)) = r.v; return nil }

func (s *sentTable) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	if !strings.Contains(sql, "FROM cal.deadline_reminder_sent") {
		panic("unexpected query: " + sql)
	}
	return boolRow{v: s.rows[sentRowKey(args)]}
}

func (s *sentTable) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if !strings.Contains(sql, "INSERT INTO cal.deadline_reminder_sent") || !strings.Contains(sql, "ON CONFLICT") {
		return pgconn.CommandTag{}, fmt.Errorf("unexpected exec: %s", sql)
	}
	k := sentRowKey(args)
	if s.rows[k] {
		return pgconn.NewCommandTag("INSERT 0 0"), nil
	}
	s.rows[k] = true
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type sentRunner struct{ tx *sentTable }

func (r sentRunner) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, r.tx)
}

// TestPGSentLogSurvivesRestart builds a fresh Reminders for every run, as a restarted worker
// would, over one durable sent-log table.
func TestPGSentLogSurvivesRestart(t *testing.T) {
	// rule: ARC-U-030
	loc := toronto(t)
	day1 := time.Date(2026, 10, 13, 9, 0, 0, 0, loc)
	tests := []struct {
		name string
		runs []time.Time // one restart before each run
		want []int       // reminders sent per run
	}{
		{name: "restart the same day sends nothing", runs: []time.Time{day1, day1.Add(time.Hour)}, want: []int{1, 0}},
		{name: "three restarts in a day send once", runs: []time.Time{day1, day1.Add(time.Hour), day1.Add(5 * time.Hour)}, want: []int{1, 0, 0}},
		{name: "the next day sends again", runs: []time.Time{day1, day1.Add(24 * time.Hour)}, want: []int{1, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			table := &sentTable{rows: map[string]bool{}}
			store := deadlinefake.New()
			seedDue(t, store, deadline.Date{Year: 2026, Month: time.October, Day: 16})
			for i, at := range tt.runs {
				n := &recordNotifier{}
				r := &deadline.Reminders{Deadlines: store, Clock: clockfake.New(at, loc), Primary: n,
					Sent: deadline.PGSentLog{DB: sentRunner{tx: table}}}
				res, err := r.Remind(ctx)
				if err != nil || res.Sent != tt.want[i] || len(n.notices) != tt.want[i] {
					t.Fatalf("run %d = %+v, %d notices, %v; want %d sent", i, res, len(n.notices), err, tt.want[i])
				}
			}
		})
	}
}

func TestPGSentLogKeys(t *testing.T) {
	ctx := context.Background()
	day := deadline.Date{Year: 2026, Month: time.October, Day: 13}
	log := deadline.PGSentLog{DB: sentRunner{tx: &sentTable{rows: map[string]bool{}}}}
	base := deadline.SentKey{Kind: deadline.NoticeReminder, DeadlineID: "d1", UserID: "u1", Day: day}
	if err := log.Mark(ctx, base); err != nil {
		t.Fatal(err)
	}
	if err := log.Mark(ctx, base); err != nil {
		t.Fatalf("second mark: %v", err)
	}
	tests := []struct {
		name string
		key  deadline.SentKey
		want bool
	}{
		{name: "same deadline, user and day", key: base, want: true},
		{name: "other user", key: deadline.SentKey{Kind: base.Kind, DeadlineID: "d1", UserID: "u2", Day: day}},
		{name: "no user", key: deadline.SentKey{Kind: base.Kind, DeadlineID: "d1", Day: day}},
		{name: "other deadline", key: deadline.SentKey{Kind: base.Kind, DeadlineID: "d2", UserID: "u1", Day: day}},
		{name: "next day", key: deadline.SentKey{Kind: base.Kind, DeadlineID: "d1", UserID: "u1", Day: day.AddDays(1)}},
		{name: "digest", key: deadline.SentKey{Kind: deadline.NoticeDigest, Day: day}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := log.Sent(ctx, tt.key)
			if err != nil || got != tt.want {
				t.Fatalf("Sent = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}
