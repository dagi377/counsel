// Package trigger starts AI feature runs. Job features (source "job") start from kernel
// events or a schedule; screen-action features start from a user's request.
//
// A fired trigger enqueues one River job of kind agent_feature_run. The job carries ids only
// (feature id, subject ids, acting user, event id or schedule slot), never client text, so
// nothing a model reads or writes reaches the job table (AI-R-005).
package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/outbox"
)

// JobKindRun is the River job kind that runs one job feature.
const JobKindRun = "agent_feature_run"

// TickInterval is how often the periodic job checks the scheduled bindings. A slot fires
// once however many ticks fall in it, because the job is unique by feature and slot.
const TickInterval = 15 * time.Minute

// Sentinel errors.
var (
	// ErrBadBinding: the feature is not a job feature, has neither events nor a schedule,
	// a scheduled binding has no acting user, or the feature is already bound.
	ErrBadBinding = errors.New("trigger: invalid binding")
	// ErrNoActingFor: an event names no user for the run to act for.
	ErrNoActingFor = errors.New("trigger: event has no acting user")
)

// Cadence is how often a scheduled job feature runs.
type Cadence string

// Cadences. Every cadence fires on business days only (weekdays that are not holidays, in
// the clock's business time zone, America/Toronto).
const (
	CadenceNone      Cadence = ""
	CadenceHourly    Cadence = "hourly"
	CadenceDaily     Cadence = "daily"     // once per business day
	CadenceWeekly    Cadence = "weekly"    // the first business day of the week
	CadenceMonthly   Cadence = "monthly"   // the first business day of the month
	CadenceQuarterly Cadence = "quarterly" // the first business day of the quarter
)

// ParseCadence reads the schedule part of a feature's Trigger ("schedule: weekly", "schedule
// (daily) and on role change", "schedule: weekdays 07:30"). A trigger with no schedule is
// CadenceNone.
func ParseCadence(trigger string) Cadence {
	t := strings.ToLower(trigger)
	if !strings.Contains(t, "schedule") && !strings.Contains(t, "monthly") {
		return CadenceNone
	}
	for _, c := range []struct {
		words   []string
		cadence Cadence
	}{
		{[]string{"hourly"}, CadenceHourly},
		{[]string{"daily", "nightly", "weekdays"}, CadenceDaily},
		{[]string{"weekly", "monday"}, CadenceWeekly},
		{[]string{"monthly"}, CadenceMonthly},
		{[]string{"quarterly"}, CadenceQuarterly},
	} {
		for _, w := range c.words {
			if strings.Contains(t, w) {
				return c.cadence
			}
		}
	}
	// "schedule" alone, or a per-record schedule ("review dates"): check daily.
	return CadenceDaily
}

// Binding ties one job feature to the kernel event types and the schedule that start it.
type Binding struct {
	FeatureID string
	Events    []string // kernel event types (outbox Message.Type): "document.uploaded", "gate.decided"
	Cadence   Cadence
	ActingFor string // user a scheduled run acts for; event runs act for the event's user
	// Designated: a scheduled run acts for the feature's designated lawyer, read from firm
	// settings at each tick, instead of ActingFor. Set by BindDesignated.
	Designated bool
	// PerUser: each scheduled slot runs once per active user, each run acting for that
	// user, instead of once for ActingFor. Set by BindPerUser.
	PerUser bool
}

// Bind builds d's binding: the cadence comes from d.Trigger, the event types from events,
// since the spec words events in prose and the owning module names the kernel types.
func Bind(d feature.Def, actingFor string, events ...string) (Binding, error) {
	if d.Source != feature.SourceJob {
		return Binding{}, fmt.Errorf("bind %s: source %q is not a job: %w", d.ID, d.Source, ErrBadBinding)
	}
	b := Binding{FeatureID: d.ID, Events: dedup(events), Cadence: ParseCadence(d.Trigger), ActingFor: actingFor}
	if err := b.validate(); err != nil {
		return Binding{}, err
	}
	return b, nil
}

func (b Binding) validate() error {
	switch {
	case b.FeatureID == "":
		return fmt.Errorf("bind: feature id required: %w", ErrBadBinding)
	case len(b.Events) == 0 && b.Cadence == CadenceNone:
		return fmt.Errorf("bind %s: no event and no schedule: %w", b.FeatureID, ErrBadBinding)
	case b.Designated && b.PerUser:
		return fmt.Errorf("bind %s: designated and per-user: %w", b.FeatureID, ErrBadBinding)
	case b.PerUser && b.ActingFor != "":
		return fmt.Errorf("bind %s: per-user run acts for each user, not %q: %w", b.FeatureID, b.ActingFor, ErrBadBinding)
	case b.Cadence != CadenceNone && b.ActingFor == "" && !b.Designated && !b.PerUser:
		return fmt.Errorf("bind %s: scheduled run needs an acting user: %w", b.FeatureID, ErrBadBinding)
	}
	for _, e := range b.Events {
		if e == "" {
			return fmt.Errorf("bind %s: empty event type: %w", b.FeatureID, ErrBadBinding)
		}
	}
	return nil
}

// SubjectRef is one record a run is about, by id.
type SubjectRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// RunArgs is the agent_feature_run job. It holds ids only (AI-R-005). EventID is set for an
// event run and Slot for a scheduled run; the job is unique by feature, event id and slot,
// so a redelivered event or a second tick in one slot inserts no second run.
type RunArgs struct {
	FeatureID string       `json:"feature_id" river:"unique"`
	Subjects  []SubjectRef `json:"subjects,omitempty"`
	ActingFor string       `json:"acting_for"`
	EventID   string       `json:"event_id,omitempty" river:"unique"`
	Slot      string       `json:"slot,omitempty" river:"unique"`
	// ForUser is set on a per-user run to its acting user, so each user's run in one slot is
	// its own job. A run whose ForUser differs from ActingFor is refused (CheckOwnUser).
	ForUser string `json:"for_user,omitempty" river:"unique"`
}

// ErrCrossUser: a per-user run names one user to act for and another whose time it is for.
var ErrCrossUser = errors.New("trigger: per-user run acts for another user")

// CheckOwnUser returns ErrCrossUser when a per-user run would act for one user on another
// user's time. A run that is not per-user (ForUser empty) passes.
func (a RunArgs) CheckOwnUser() error {
	// rule: LSO-3.6
	if a.ForUser != "" && a.ForUser != a.ActingFor {
		return fmt.Errorf("run %s: %w", a.FeatureID, ErrCrossUser)
	}
	return nil
}

// Kind implements river.JobArgs.
func (RunArgs) Kind() string { return JobKindRun }

// InsertOpts makes the job unique by its unique fields.
func (RunArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// Enqueuer inserts River jobs; *river.Client satisfies it.
type Enqueuer interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// ActorOf returns the user an event's run acts for.
type ActorOf func(m outbox.Message) (string, error)

// PayloadActor reads "acting_for", then "user_id", from the event payload.
func PayloadActor(m outbox.Message) (string, error) {
	var p struct {
		ActingFor string `json:"acting_for"`
		UserID    string `json:"user_id"`
	}
	if len(m.Payload) > 0 {
		if err := json.Unmarshal(m.Payload, &p); err != nil {
			return "", fmt.Errorf("event %s: payload: %w", m.ID, err)
		}
	}
	if p.ActingFor != "" {
		return p.ActingFor, nil
	}
	if p.UserID != "" {
		return p.UserID, nil
	}
	return "", fmt.Errorf("event %s (%s): %w", m.ID, m.Type, ErrNoActingFor)
}

// Bindings is the set of job-feature bindings boot builds. It enqueues runs for kernel
// events (OnEvent) and for schedule slots (Tick). Boot adds every binding before serving.
type Bindings struct {
	q        Enqueuer
	clk      clock.Clock
	holidays []clock.Date
	actor    ActorOf
	byEvent  map[string][]Binding
	features map[string]bool
	sched    []Binding
	lawyers  DesignatedLawyers
	users    ActiveUsers
}

// New returns an empty set that enqueues on q, reads time from clk and skips holidays.
func New(q Enqueuer, clk clock.Clock, holidays []clock.Date) *Bindings {
	return &Bindings{
		q: q, clk: clk, holidays: holidays, actor: PayloadActor,
		byEvent: map[string][]Binding{}, features: map[string]bool{},
	}
}

// WithActor sets how an event names its acting user.
func (s *Bindings) WithActor(a ActorOf) *Bindings {
	s.actor = a
	return s
}

// Add puts b in the set. A feature bound twice is ErrBadBinding.
func (s *Bindings) Add(b Binding) error {
	if err := b.validate(); err != nil {
		return err
	}
	if s.features[b.FeatureID] {
		return fmt.Errorf("bind %s: already bound: %w", b.FeatureID, ErrBadBinding)
	}
	s.features[b.FeatureID] = true
	for _, e := range b.Events {
		s.byEvent[e] = append(s.byEvent[e], b)
	}
	if b.Cadence != CadenceNone {
		s.sched = append(s.sched, b)
	}
	return nil
}

// EventTypes returns the kernel event types some binding listens to, sorted, for the outbox
// consumer to subscribe.
func (s *Bindings) EventTypes() []string {
	out := make([]string, 0, len(s.byEvent))
	for e := range s.byEvent {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

// OnEvent enqueues one run per feature bound to m.Type, about m.Subject, acting for the
// event's user. It returns how many runs it enqueued, duplicates included.
func (s *Bindings) OnEvent(ctx context.Context, m outbox.Message) (int, error) {
	bound := s.byEvent[m.Type]
	if len(bound) == 0 {
		return 0, nil
	}
	actingFor, err := s.actor(m)
	if err != nil {
		return 0, err
	}
	var subjects []SubjectRef
	if m.Subject.ID != "" {
		subjects = []SubjectRef{{Kind: m.Subject.Kind, ID: m.Subject.ID}}
	}
	for _, b := range bound {
		// rule: AI-R-005
		// The job carries the event's ids, never its payload.
		args := RunArgs{FeatureID: b.FeatureID, Subjects: subjects, ActingFor: actingFor, EventID: m.ID}
		if err := s.insert(ctx, args); err != nil {
			return 0, err
		}
	}
	return len(bound), nil
}

// Tick enqueues the scheduled runs due at the clock's now. It returns how many it asked to
// enqueue; a slot already enqueued is a River duplicate and inserts nothing. A designated
// binding whose feature has no designated lawyer is skipped (fail closed); Readiness
// reports it.
func (s *Bindings) Tick(ctx context.Context) (int, error) {
	now := s.clk.Now()
	n := 0
	for _, b := range s.sched {
		slot, ok := s.slot(b.Cadence, now)
		if !ok {
			continue
		}
		if b.PerUser {
			m, err := s.tickPerUser(ctx, b, slot)
			n += m
			if err != nil {
				return n, err
			}
			continue
		}
		actingFor, err := s.actingFor(ctx, b)
		if errors.Is(err, ErrNoDesignatedLawyer) {
			slog.WarnContext(ctx, "scheduled agent feature not started: no designated lawyer", "feature", b.FeatureID, "slot", slot)
			continue
		}
		if err != nil {
			return n, err
		}
		args := RunArgs{FeatureID: b.FeatureID, ActingFor: actingFor, Slot: slot}
		if err := s.insert(ctx, args); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// PeriodicJob is the River periodic job that calls Tick every TickInterval. Boot adds it
// with kind JobKindTick and a TickWorker.
func (s *Bindings) PeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(TickInterval), func() (river.JobArgs, *river.InsertOpts) {
		return TickArgs{}, nil
	}, &river.PeriodicJobOpts{ID: JobKindTick})
}

// JobKindTick is the periodic job kind that checks the scheduled bindings.
const JobKindTick = "agent_feature_tick"

// TickArgs is the agent_feature_tick job. It has no fields.
type TickArgs struct{}

// Kind implements river.JobArgs.
func (TickArgs) Kind() string { return JobKindTick }

// TickWorker runs Tick for each agent_feature_tick job.
type TickWorker struct {
	river.WorkerDefaults[TickArgs]
	Bindings *Bindings
}

// Work implements river.Worker.
func (w *TickWorker) Work(ctx context.Context, _ *river.Job[TickArgs]) error {
	_, err := w.Bindings.Tick(ctx)
	return err
}

func (s *Bindings) insert(ctx context.Context, args RunArgs) error {
	if err := args.CheckOwnUser(); err != nil {
		return err
	}
	opts := args.InsertOpts()
	res, err := s.q.Insert(ctx, args, &opts)
	if err != nil {
		return fmt.Errorf("trigger %s: enqueue run: %w", args.FeatureID, err)
	}
	slog.InfoContext(ctx, "agent feature run enqueued", "feature", args.FeatureID, "event", args.EventID,
		"slot", args.Slot, "duplicate", res != nil && res.UniqueSkippedAsDuplicate)
	return nil
}

// slot names the schedule slot now falls in, in the business time zone. ok is false when
// now is not a business day, or not the first business day of a weekly, monthly or
// quarterly slot.
func (s *Bindings) slot(c Cadence, now time.Time) (string, bool) {
	loc := s.clk.Location()
	today := clock.DateOf(now, loc)
	if !deadline.IsBusinessDay(today, s.holidays) {
		return "", false
	}
	var start clock.Date
	switch c {
	case CadenceHourly:
		return now.In(loc).Format("2006-01-02T15"), true
	case CadenceDaily:
		return today.String(), true
	case CadenceWeekly:
		wd := (int(today.Start(time.UTC).Weekday()) + 6) % 7 // Monday = 0
		start = today.AddDays(-wd)
	case CadenceMonthly:
		start = clock.Date{Year: today.Year, Month: today.Month, Day: 1}
	case CadenceQuarterly:
		start = clock.Date{Year: today.Year, Month: today.Month - (today.Month-1)%3, Day: 1}
	default:
		return "", false
	}
	// The slot fires on its first business day only.
	for d := start; d.Before(today); d = d.AddDays(1) {
		if deadline.IsBusinessDay(d, s.holidays) {
			return "", false
		}
	}
	return start.String(), true
}

func dedup(in []string) []string {
	var out []string
	for _, s := range in {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}
