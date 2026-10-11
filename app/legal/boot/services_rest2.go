package boot

import (
	"context"
	"fmt"
	"time"

	"github.com/riverqueue/river"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/app/legal/rulepacks/ontario"
	"github.com/dagi377/counsel/app/legal/trust/recon"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/deadline"
)

// Deadline reminder job kinds (BOOT-21).
const (
	KindDeadlineRemind = "deadline.remind"
	KindDeadlineDigest = "deadline.digest"
)

// RemindInterval is how often deadline.remind runs: hourly keeps every reminder well inside
// the 24 h SLO-04 bound.
const RemindInterval = time.Hour

// DigestHour is the business-time hour deadline.digest runs each day.
const DigestHour = 7

// DeadlinePorts are the reminder jobs' dependencies. Deadlines lists the stored deadlines;
// Primary and Fallback deliver the notices (ARC-U-030).
type DeadlinePorts struct {
	Deadlines deadline.Lister
	Primary   deadline.Notifier
	Fallback  deadline.Notifier
	// Sent remembers what went out; nil keeps an in-memory log for the process.
	Sent deadline.SentLog
}

type remindArgs struct{}

func (remindArgs) Kind() string { return KindDeadlineRemind }

type digestArgs struct{}

func (digestArgs) Kind() string { return KindDeadlineDigest }

type remindWorker struct {
	river.WorkerDefaults[remindArgs]
	r *deadline.Reminders
}

func (w remindWorker) Work(ctx context.Context, _ *river.Job[remindArgs]) error {
	if _, err := w.r.Remind(ctx); err != nil {
		return fmt.Errorf("%s: %w", KindDeadlineRemind, err)
	}
	return nil
}

type digestWorker struct {
	river.WorkerDefaults[digestArgs]
	r *deadline.Reminders
}

func (w digestWorker) Work(ctx context.Context, _ *river.Job[digestArgs]) error {
	if _, err := w.r.Digest(ctx); err != nil {
		return fmt.Errorf("%s: %w", KindDeadlineDigest, err)
	}
	return nil
}

// newReminders builds the T-031.4 reminder worker on the kernel clock. The Ontario holiday
// calendar counts business days left only while legalflags.DeadlineOntarioPack is on.
func newReminders(k *kboot.Kernel, p DeadlinePorts) *deadline.Reminders {
	sent := p.Sent
	if sent == nil {
		sent = &deadline.MemSentLog{}
	}
	return &deadline.Reminders{Deadlines: p.Deadlines, Clock: k.Clock, Primary: p.Primary, Fallback: p.Fallback, Sent: sent,
		Holidays: func(context.Context) ([]deadline.Date, error) {
			if k.Flags == nil || !k.Flags.Enabled(legalflags.DeadlineOntarioPack) {
				return nil, nil
			}
			return ontario.Holidays()
		}}
}

// addDeadlineJobs adds deadline.remind (hourly) and deadline.digest (daily at DigestHour
// business time).
func addDeadlineJobs(k *kboot.Kernel, p DeadlinePorts) error {
	if k == nil || k.Jobs == nil {
		return fmt.Errorf("deadline jobs: kernel job set: %w", ErrJobDependency)
	}
	if err := requireDeps("deadline jobs", []dep{
		{"kernel Clock", k.Clock == nil},
		{"Deadlines", p.Deadlines == nil},
		{"Primary", p.Primary == nil},
	}); err != nil {
		return err
	}
	r := newReminders(k, p)
	if err := kboot.AddWorker[remindArgs](k.Jobs, remindWorker{r: r}); err != nil {
		return fmt.Errorf("deadline jobs: %w", err)
	}
	if err := kboot.AddWorker[digestArgs](k.Jobs, digestWorker{r: r}); err != nil {
		return fmt.Errorf("deadline jobs: %w", err)
	}
	k.Jobs.AddPeriodic(periodicEvery(RemindInterval, remindArgs{}), KindDeadlineRemind)
	k.Jobs.AddPeriodic(river.NewPeriodicJob(dailyAt{hour: DigestHour, loc: k.Clock.Location()}, func() (river.JobArgs, *river.InsertOpts) {
		return digestArgs{}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByPeriod: 24 * time.Hour}}
	}, nil), KindDeadlineDigest)
	return nil
}

// dailyAt is a river schedule that fires once a day at hour in loc (business time).
type dailyAt struct {
	hour int
	loc  *time.Location
}

// Next returns the first hour:00 in loc strictly after t.
func (d dailyAt) Next(t time.Time) time.Time {
	loc := d.loc
	if loc == nil {
		loc = time.UTC
	}
	lt := t.In(loc)
	next := time.Date(lt.Year(), lt.Month(), lt.Day(), d.hour, 0, 0, 0, loc)
	if !next.After(lt) {
		next = time.Date(lt.Year(), lt.Month(), lt.Day()+1, d.hour, 0, 0, 0, loc)
	}
	return next
}

// reconImports is the Postgres reconciliation service with STMT-IMPORT's statement import in
// place of PGService's refusal.
type reconImports struct {
	recon.Service
	imports recon.PGImports
}

// ImportStatement implements recon.Service.
func (s reconImports) ImportStatement(ctx context.Context, st recon.Statement) (string, error) {
	return s.imports.ImportStatement(ctx, st)
}

// withImports fills the statement upload ports before the readers and writers: the generic
// CSV parser replaces NoParser, and the Postgres reconciliation service imports the parsed
// statement into trust.bank_import and trust.bank_line. Ports already set are kept.
func (p HTTPPorts) withImports(k *kboot.Kernel) HTTPPorts {
	if k == nil || k.DB == nil {
		return p
	}
	if p.ReconParser == nil {
		p.ReconParser = recon.CSVParser{}
	}
	if p.Recons == nil {
		p.Recons = reconImports{Service: recon.PGService{DB: k.DB}, imports: recon.PGImports{DB: k.DB}}
	}
	return p
}

// runMount is the agent run routes (HTTP-25) over runs, or nil when a port is missing.
func runMount(p HTTPPorts, runs agent.RunService, access agent.RunAccess) mount {
	if runs == nil || access == nil || p.Idem == nil || p.Problems == nil || p.Principal == nil {
		return nil
	}
	return (&agent.RunHTTP{Runs: runs, Access: access, Idem: p.Idem, Problems: p.Problems, Principal: p.Principal}).Routes
}
