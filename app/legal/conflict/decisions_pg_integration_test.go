//go:build integration

package conflict

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/db"
)

// Every PGDecisions statement parses and runs against the migrated schema: writes on an
// unknown run are ErrNotFound and change nothing, and an unknown run has no record.
func TestPGDecisionsIntegration(t *testing.T) {
	pool := runsTestDB(t)
	ctx := context.Background()
	s := NewPGDecisions(pool)
	const missing = "00000000-0000-7000-8000-000000000099"
	at := time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		run     func() error
		wantErr error
	}{
		{"dispose on unknown run", func() error {
			_, _, err := s.Dispose(ctx, DispositionRecord{RunID: missing, Disposition: DispositionNotAConflict, Note: "n", By: missing, At: at})
			return err
		}, ErrNotFound},
		{"consent on unknown run", func() error {
			return s.RecordConsent(ctx, Consent{RunID: missing, Type: ConsentOther, Status: ConsentRequested, DisclosureSummary: "d",
				RecordedBy: missing, RecordedAt: at})
		}, ErrNotFound},
		{"decide unknown run", func() error {
			return s.Decide(ctx, DecisionRecord{RunID: missing, By: missing, Approve: true, At: at}, clearCheck(missing, 1))
		}, ErrNotFound},
		{"unknown run has no record", func() error {
			if _, ok, err := s.Decision(ctx, missing); err != nil || ok {
				return errors.Join(err, errors.New("unexpected decision"))
			}
			if d, err := s.Dispositions(ctx, missing); err != nil || len(d) != 0 {
				return errors.Join(err, errors.New("unexpected dispositions"))
			}
			if c, err := s.Consents(ctx, missing); err != nil || len(c) != 0 {
				return errors.Join(err, errors.New("unexpected consents"))
			}
			return nil
		}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// seedCheck inserts a matter with one conflict check and one check party, and two lawyers.
// It returns the matter, check, check party, the two user ids and the client contact.
func seedCheck(t *testing.T, pool *db.Pool) (matterID, checkID, partyID, prep, law, client string) {
	t.Helper()
	ctx := context.Background()
	q, err := os.ReadFile("testdata/conflict_record_seed.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, string(q)).Scan(&matterID, &checkID, &partyID, &prep, &law, &client); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return
}

type pgOneHit struct{ party, contact string }

func (s pgOneHit) Search(_ context.Context, term string) ([]Hit, error) {
	return []Hit{{Term: term, PartyID: s.contact, CheckPartyID: s.party, MatchedKind: "contact", MatchType: "exact",
		ScorePPM: 1_000_000}}, nil
}

func (pgOneHit) IndexVersion(context.Context) (int64, error) { return 1, nil }

// rule: ARC-I-041 (LSO RPC 3.4 conflict record)
// A disposition, consent and G2 clearance written through one Checks are read by a second
// Checks on the same database (another replica, or the process after a restart); a consent
// hit without a signed consent cannot clear.
func TestPGConflictRecordRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		disp      Disposition
		signed    bool
		wantClear error
		want      Status
	}{
		{"not a conflict clears", DispositionNotAConflict, false, nil, StatusCleared},
		{"signed consent clears", DispositionNeedsConsent, true, nil, StatusCleared},
		{"consent without a signed record stays open", DispositionNeedsConsent, false, ErrConsentMissing, StatusOpen},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := runsTestDB(t)
			ctx := context.Background()
			matterID, checkID, partyID, prep, lawID, contact := seedCheck(t, pool)
			var doc string
			at := time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC)
			now := func() time.Time { return at }
			instance := func() *Checks {
				return NewChecksStored(pgOneHit{party: partyID, contact: contact}, NewPGRuns(pool), NewPGDecisions(pool), now)
			}
			a := instance()
			law := Actor{UserID: lawID, IsLawyer: true}
			run, err := a.Check(ctx, Request{MatterID: matterID, CheckID: checkID, Terms: []string{"Acme"},
				Preparer: Actor{UserID: prep, IsLawyer: true}})
			if err != nil {
				t.Fatal(err)
			}
			if err := a.Dispose(ctx, run.ID, 0, law, tt.disp, "reviewed"); err != nil {
				t.Fatal(err)
			}
			if tt.signed {
				if err := pool.QueryRow(ctx, `INSERT INTO docs.document (matter_id, title, doc_type, source) VALUES ($1::uuid, 'Consent', 'consent', 'upload')
  RETURNING id::text`, matterID).Scan(&doc); err != nil {
					t.Fatalf("seed consent document: %v", err)
				}
				if err := a.RecordConsent(ctx, run.ID, 0, law, Consent{Type: ConsentJointRetainer, Status: ConsentSigned,
					DisclosureSummary: "acts for both", ContactID: contact, DocumentID: doc, SignedAt: at}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := a.decide(ctx, run.ID, law, true, "reviewed"); !errors.Is(err, tt.wantClear) {
				t.Fatalf("decide err = %v, want %v", err, tt.wantClear)
			}
			b := instance() // a second replica, or the process after a restart
			runs, err := b.Runs(ctx, matterID)
			if err != nil || len(runs) != 1 {
				t.Fatalf("runs = %v, %v", runs, err)
			}
			if runs[0].Status != tt.want {
				t.Fatalf("status = %s, want %s", runs[0].Status, tt.want)
			}
			if _, err := b.decide(ctx, run.ID, law, true, "again"); tt.want == StatusCleared && !errors.Is(err, ErrDecided) {
				t.Fatalf("second decision err = %v, want ErrDecided", err)
			}
		})
	}
}
