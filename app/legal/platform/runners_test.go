package platform

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/audit/auditfake"
	"github.com/dagi377/counsel/kernel/db/dbfake"
)

// releaseFake is an in-memory ReleaseStore: gate id -> status.
type releaseFake struct {
	order  []Release
	status map[string]string
}

func newReleaseFake(rs ...Release) *releaseFake {
	f := &releaseFake{order: rs, status: map[string]string{}}
	for _, r := range rs {
		f.status[r.GateID] = "queued"
	}
	return f
}

func (f *releaseFake) NextQueued(context.Context, pgx.Tx) (Release, bool, error) {
	for _, r := range f.order {
		if f.status[r.GateID] == "queued" {
			return r, true, nil
		}
	}
	return Release{}, false, nil
}

func (f *releaseFake) Mark(_ context.Context, _ pgx.Tx, gateID, status string) (bool, error) {
	if f.status[gateID] != "queued" {
		return false, nil
	}
	f.status[gateID] = status
	return true, nil
}

// senderFake records sends and fails the gate ids in fail.
type senderFake struct {
	sent []string
	fail map[string]bool
}

func (s *senderFake) Send(_ context.Context, _ pgx.Tx, r Release) error {
	if s.fail[r.GateID] {
		return errors.New("channel down")
	}
	s.sent = append(s.sent, r.GateID)
	return nil
}

func auditActions(a *auditfake.Fake) []string {
	var out []string
	for _, e := range a.Events() {
		out = append(out, e.Action+" "+e.Subject.ID)
	}
	return out
}

func TestReleaseRunner(t *testing.T) {
	email := Release{GateID: "g-1", Kind: "outgoing_email", MatterID: "m-1"}
	memo := Release{GateID: "g-2", Kind: "research_memo", MatterID: "m-1", RefID: "v-1"}
	tests := []struct {
		name       string
		queued     []Release
		fail       map[string]bool
		passes     int
		wantSent   []string
		wantStatus map[string]string
		wantAudit  []string
	}{
		// rule: ARC-I-012
		{name: "a queued item is sent once and audited", queued: []Release{email}, passes: 1,
			wantSent: []string{"g-1"}, wantStatus: map[string]string{"g-1": "sent"},
			wantAudit: []string{ActionReleaseSent + " g-1"}},
		{name: "a retry is a no-op", queued: []Release{email}, passes: 2,
			wantSent: []string{"g-1"}, wantStatus: map[string]string{"g-1": "sent"},
			wantAudit: []string{ActionReleaseSent + " g-1"}},
		{name: "a failed send is marked failed and the pass goes on", queued: []Release{email, memo},
			fail: map[string]bool{"g-1": true}, passes: 2,
			wantSent: []string{"g-2"}, wantStatus: map[string]string{"g-1": "failed", "g-2": "sent"},
			wantAudit: []string{ActionReleaseFailed + " g-1", ActionReleaseSent + " g-2"}},
		{name: "nothing queued sends nothing", passes: 1, wantStatus: map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, send, log := newReleaseFake(tt.queued...), &senderFake{fail: tt.fail}, auditfake.New()
			r := ReleaseRunner{DB: dbfake.New(), Store: store, Sender: send, Audit: log}
			for i := 0; i < tt.passes; i++ {
				if _, err := r.Run(context.Background()); err != nil {
					t.Fatalf("pass %d: %v", i, err)
				}
			}
			if !slices.Equal(send.sent, tt.wantSent) {
				t.Fatalf("sent = %v, want %v", send.sent, tt.wantSent)
			}
			for id, want := range tt.wantStatus {
				if store.status[id] != want {
					t.Fatalf("status[%s] = %q, want %q", id, store.status[id], want)
				}
			}
			if got := auditActions(log); !slices.Equal(got, tt.wantAudit) {
				t.Fatalf("audit = %v, want %v", got, tt.wantAudit)
			}
		})
	}
}

func TestReleaseRunnerMissingDependency(t *testing.T) {
	if _, err := (ReleaseRunner{DB: dbfake.New()}).Run(context.Background()); !errors.Is(err, ErrRunnerDependency) {
		t.Fatalf("err = %v, want ErrRunnerDependency", err)
	}
}

// destructionFake is an in-memory DestructionStore.
type destructionFake struct {
	orders    []DestructionOrder
	status    map[string]string
	retention map[string]MatterRetention
	destroyed []string
}

func (f *destructionFake) NextScheduled(context.Context, pgx.Tx) (DestructionOrder, bool, error) {
	for _, o := range f.orders {
		if f.status[o.GateID] == "scheduled" {
			return o, true, nil
		}
	}
	return DestructionOrder{}, false, nil
}

func (f *destructionFake) Retention(_ context.Context, _ pgx.Tx, matterID string) (MatterRetention, error) {
	return f.retention[matterID], nil
}

func (f *destructionFake) RecordDestroyed(_ context.Context, _ pgx.Tx, matterID, _ string, _ time.Time) error {
	m := f.retention[matterID]
	m.AlreadyDestroyed = true
	f.retention[matterID] = m
	f.destroyed = append(f.destroyed, matterID)
	return nil
}

func (f *destructionFake) Mark(_ context.Context, _ pgx.Tx, gateID, status string) (bool, error) {
	if f.status[gateID] != "scheduled" {
		return false, nil
	}
	f.status[gateID] = status
	return true, nil
}

type shredFake struct{ shredded []string }

func (s *shredFake) Shred(_ context.Context, _ pgx.Tx, matterID string, _ []string) error {
	s.shredded = append(s.shredded, matterID)
	return nil
}

var (
	runNow  = time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC) // 2026-10-11 in Toronto
	toronto = func() *time.Location {
		z, err := time.LoadLocation("America/Toronto")
		if err != nil {
			panic(err)
		}
		return z
	}()
)

func day(y int, m time.Month, d int) *time.Time {
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return &t
}

func dueMatter() MatterRetention {
	return MatterRetention{Found: true, Status: "archived", RetentionClass: "RC-FILE-7", DestroyAfter: day(2026, 10, 11),
		KeepSet: []string{"trust", "id"}}
}

func TestDestructionRunner(t *testing.T) {
	order := DestructionOrder{GateID: "g-1", MatterID: "m-1", RetentionClass: "RC-FILE-7"}
	with := func(f func(*MatterRetention)) MatterRetention { m := dueMatter(); f(&m); return m }
	tests := []struct {
		name       string
		matter     MatterRetention
		passes     int
		wantStatus string
		wantShred  int
		wantAudit  []string
	}{
		// rule: ARC-I-012
		{name: "a due order runs once and is audited", matter: dueMatter(), passes: 1, wantStatus: "done", wantShred: 1,
			wantAudit: []string{ActionDestructionDone + " g-1"}},
		{name: "a retry is a no-op", matter: dueMatter(), passes: 2, wantStatus: "done", wantShred: 1,
			wantAudit: []string{ActionDestructionDone + " g-1"}},
		// spec/legal archive-retention: a legal hold suspends destruction.
		{name: "a matter hold blocks destruction", matter: with(func(m *MatterRetention) { m.MatterHold = true }), passes: 2,
			wantStatus: "blocked", wantAudit: []string{ActionDestructionBlocked + "." + BlockHold + " g-1"}},
		{name: "a retention-record hold blocks destruction", matter: with(func(m *MatterRetention) { m.RecordHold = true }), passes: 1,
			wantStatus: "blocked", wantAudit: []string{ActionDestructionBlocked + "." + BlockHold + " g-1"}},
		{name: "a document hold blocks the whole order", matter: with(func(m *MatterRetention) { m.DocumentHold = true }), passes: 1,
			wantStatus: "blocked", wantAudit: []string{ActionDestructionBlocked + "." + BlockHold + " g-1"}},
		{name: "destroy-after tomorrow in Toronto is not due", matter: with(func(m *MatterRetention) { m.DestroyAfter = day(2026, 10, 12) }), passes: 1,
			wantStatus: "blocked", wantAudit: []string{ActionDestructionBlocked + "." + BlockNotDue + " g-1"}},
		{name: "an open related matter blocks", matter: with(func(m *MatterRetention) { m.RelatedOpen = true }), passes: 1,
			wantStatus: "blocked", wantAudit: []string{ActionDestructionBlocked + "." + BlockRelatedOpen + " g-1"}},
		{name: "an open matter blocks", matter: with(func(m *MatterRetention) { m.Status = "open" }), passes: 1,
			wantStatus: "blocked", wantAudit: []string{ActionDestructionBlocked + "." + BlockNotClosed + " g-1"}},
		{name: "a class other than the approved one blocks", matter: with(func(m *MatterRetention) { m.RetentionClass = "RC-PERM" }), passes: 1,
			wantStatus: "blocked", wantAudit: []string{ActionDestructionBlocked + "." + BlockClassMismatch + " g-1"}},
		{name: "no retention record blocks", matter: MatterRetention{}, passes: 1,
			wantStatus: "blocked", wantAudit: []string{ActionDestructionBlocked + "." + BlockNoRecord + " g-1"}},
		{name: "already destroyed closes the order without shredding", matter: with(func(m *MatterRetention) { m.AlreadyDestroyed = true }), passes: 1,
			wantStatus: "done", wantAudit: []string{ActionDestructionDone + " g-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &destructionFake{orders: []DestructionOrder{order}, status: map[string]string{"g-1": "scheduled"},
				retention: map[string]MatterRetention{"m-1": tt.matter}}
			shred, log := &shredFake{}, auditfake.New()
			r := DestructionRunner{DB: dbfake.New(), Store: store, Records: store, Shredder: shred, Audit: log,
				Now: func() time.Time { return runNow }, Zone: toronto}
			for i := 0; i < tt.passes; i++ {
				if _, err := r.Run(context.Background()); err != nil {
					t.Fatalf("pass %d: %v", i, err)
				}
			}
			if store.status["g-1"] != tt.wantStatus {
				t.Fatalf("status = %q, want %q", store.status["g-1"], tt.wantStatus)
			}
			if len(shred.shredded) != tt.wantShred {
				t.Fatalf("shredded %v, want %d", shred.shredded, tt.wantShred)
			}
			if got := auditActions(log); !slices.Equal(got, tt.wantAudit) {
				t.Fatalf("audit = %v, want %v", got, tt.wantAudit)
			}
		})
	}
}

func TestDestructionRunnerWithoutShredderKeepsOrderScheduled(t *testing.T) {
	store := &destructionFake{orders: []DestructionOrder{{GateID: "g-1", MatterID: "m-1", RetentionClass: "RC-FILE-7"}},
		status: map[string]string{"g-1": "scheduled"}, retention: map[string]MatterRetention{"m-1": dueMatter()}}
	r := DestructionRunner{DB: dbfake.New(), Store: store, Records: store, Audit: auditfake.New(), Now: func() time.Time { return runNow }, Zone: toronto}
	if _, err := r.Run(context.Background()); !errors.Is(err, ErrNoShredder) {
		t.Fatalf("err = %v, want ErrNoShredder", err)
	}
	if store.status["g-1"] != "scheduled" || len(store.destroyed) != 0 {
		t.Fatalf("status = %q destroyed = %v, want scheduled and nothing destroyed", store.status["g-1"], store.destroyed)
	}
}
