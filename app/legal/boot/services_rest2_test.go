package boot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/app/legal/trust/recon"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/deadline/deadlinefake"
	"github.com/dagi377/counsel/kernel/flags"
)

type noticeLog struct{ notices []deadline.Notice }

func (n *noticeLog) Notify(_ context.Context, x deadline.Notice) error {
	n.notices = append(n.notices, x)
	return nil
}

func deadlineKernel(t *testing.T, ontarioOn bool, now time.Time) (*kboot.Kernel, *clockfake.Fake) {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	s := flags.New()
	if err := s.Define(flags.Def{Name: legalflags.DeadlineOntarioPack, Owner: "owner", Expiry: now.AddDate(1, 0, 0)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(legalflags.DeadlineOntarioPack, ontarioOn); err != nil {
		t.Fatal(err)
	}
	clk := clockfake.New(now.In(loc), loc)
	return &kboot.Kernel{Flags: s, Jobs: kboot.NewJobs(), Clock: clk}, clk
}

func TestDeadlineJobsRemindOncePerDay(t *testing.T) {
	friday := time.Date(2026, 10, 9, 11, 0, 0, 0, time.UTC) // 07:00 in Toronto, before Thanksgiving
	tests := []struct {
		name          string
		ontarioOn     bool
		wantBusinessD int // business days left to Tue Oct 13
	}{
		{"ontario pack off: weekends only", false, 2},
		{"ontario pack on: Thanksgiving is a holiday", true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			k, clk := deadlineKernel(t, tt.ontarioOn, friday)
			store := deadlinefake.New()
			due := clk.Today(ctx).AddDays(4) // Tue Oct 13
			if _, err := store.Save(ctx, nil, deadline.Proposed{PackID: "p", PackVersion: "1", RuleID: "r", Due: due,
				Subject: deadline.Subject{Kind: "matter", ID: "m1"}}); err != nil {
				t.Fatal(err)
			}
			n := &noticeLog{}
			if err := addDeadlineJobs(k, DeadlinePorts{Deadlines: store, Primary: n}); err != nil {
				t.Fatal(err)
			}
			if got := k.Jobs.Kinds(); len(got) != 2 || len(k.Jobs.Periodic()) != 2 {
				t.Fatalf("kinds = %v, periodic = %d", got, len(k.Jobs.Periodic()))
			}
			if err := k.Jobs.Validate(); err != nil {
				t.Fatal(err)
			}
			r := newReminders(k, DeadlinePorts{Deadlines: store, Primary: n})
			w := remindWorker{r: r}
			for range 2 { // a second run the same day sends nothing
				if err := w.Work(ctx, nil); err != nil {
					t.Fatal(err)
				}
				clk.Advance(3 * time.Hour)
			}
			if len(n.notices) != 1 || n.notices[0].Items[0].BusinessDaysLeft != tt.wantBusinessD {
				t.Fatalf("notices = %+v", n.notices)
			}
			clk.Advance(24 * time.Hour) // next day: one more
			if err := w.Work(ctx, nil); err != nil || len(n.notices) != 2 {
				t.Fatalf("next day: %v, %d notices", err, len(n.notices))
			}
			if err := (digestWorker{r: r}).Work(ctx, nil); err != nil || len(n.notices) != 3 || n.notices[2].Kind != deadline.NoticeDigest {
				t.Fatalf("digest: %v, %+v", err, n.notices)
			}
		})
	}
}

func TestAddDeadlineJobsDependencies(t *testing.T) {
	now := time.Date(2026, 10, 9, 11, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		ports DeadlinePorts
	}{
		{"no deadlines", DeadlinePorts{Primary: &noticeLog{}}},
		{"no primary", DeadlinePorts{Deadlines: deadlinefake.New()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, _ := deadlineKernel(t, false, now)
			if err := addDeadlineJobs(k, tt.ports); !errors.Is(err, ErrJobDependency) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestDailyAtNext(t *testing.T) {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	d := dailyAt{hour: DigestHour, loc: loc}
	tests := []struct {
		name string
		at   time.Time
		want time.Time
	}{
		{"before 07:00", time.Date(2026, 10, 9, 6, 59, 0, 0, loc), time.Date(2026, 10, 9, 7, 0, 0, 0, loc)},
		{"at 07:00 goes to tomorrow", time.Date(2026, 10, 9, 7, 0, 0, 0, loc), time.Date(2026, 10, 10, 7, 0, 0, 0, loc)},
		{"after 07:00", time.Date(2026, 10, 9, 20, 0, 0, 0, loc), time.Date(2026, 10, 10, 7, 0, 0, 0, loc)},
		{"across DST end", time.Date(2026, 10, 31, 8, 0, 0, 0, loc), time.Date(2026, 11, 1, 7, 0, 0, 0, loc)},
		{"from UTC input", time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC), time.Date(2026, 10, 9, 7, 0, 0, 0, loc)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := d.Next(tt.at); !got.Equal(tt.want) {
				t.Fatalf("Next(%v) = %v, want %v", tt.at, got, tt.want)
			}
		})
	}
}

func TestWithImports(t *testing.T) {
	k := &kboot.Kernel{DB: dbfake.New()}
	p := HTTPPorts{}.withImports(k)
	if _, ok := p.ReconParser.(recon.CSVParser); !ok {
		t.Fatalf("ReconParser = %T, want recon.CSVParser", p.ReconParser)
	}
	if _, ok := p.Recons.(reconImports); !ok {
		t.Fatalf("Recons = %T, want reconImports", p.Recons)
	}
	kept := HTTPPorts{ReconParser: recon.NoParser{}}.withImports(k)
	if _, ok := kept.ReconParser.(recon.NoParser); !ok {
		t.Fatal("a set parser must be kept")
	}
	if empty := (HTTPPorts{}).withImports(&kboot.Kernel{}); empty.ReconParser != nil || empty.Recons != nil {
		t.Fatal("no kernel DB fills nothing")
	}
}
