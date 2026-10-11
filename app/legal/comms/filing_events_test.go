package comms

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
)

// rowTx answers the filing statements from one in-memory legal.court_filing row.
type rowTx struct {
	pgx.Tx
	filed bool // the stored filing is filed; otherwise IssueClaim's UPDATE matches nothing
}

type rowResult struct {
	vals []any
	err  error
}

func (r rowResult) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(r.vals) != len(dest) {
		return fmt.Errorf("scan: %d values into %d targets", len(r.vals), len(dest))
	}
	for i, v := range r.vals {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(v))
	}
	return nil
}

var recorded = time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC)

func (t rowTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "INSERT INTO legal.court_filing"):
		return rowResult{vals: []any{"f1", recorded}}
	case strings.Contains(sql, "UPDATE legal.court_filing SET reference"):
		if !t.filed {
			return rowResult{err: pgx.ErrNoRows}
		}
		return rowResult{vals: []any{"f1", "m1", "d1", "Superior Court", FilingFiled, "u1", "2026-03-02", recorded}}
	}
	return rowResult{err: fmt.Errorf("rowTx: unexpected %q", sql)}
}

// rowDB runs fn on one rowTx, like kernel/db.DB.InTx.
type rowDB struct {
	*dbfake.Fake
	tx rowTx
}

func (d rowDB) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, d.tx)
}

// rule: ARC-I-035
// The filing write points publish the evidence events F-DL-04 starts on, each in the
// writer's tx; with no emitter they refuse before any transaction.
func TestFilingWritePointsPublishEvidenceEvents(t *testing.T) {
	ctx := clock.WithClock(context.Background(), clockfake.New(recorded, toronto(t)))
	filed := Filing{MatterID: "m1", DocumentID: "d1", Court: "Superior Court", Status: FilingFiled, FiledBy: "u1", FiledOn: recorded}
	issuedOn := time.Date(2026, 3, 4, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		run         func(*FilingStore) (Filing, error)
		noEvents    bool
		stored      bool // the stored filing is filed
		wantErr     error
		wantType    string
		wantPayload string
	}{
		{name: "filed filing: receipt", run: func(s *FilingStore) (Filing, error) { return s.RecordFiling(ctx, filed) },
			wantType: EventFilingReceiptReceived, wantPayload: `{"filing_id":"f1","matter_id":"m1","document_id":"d1","filed_on":"2026-03-02"}`},
		{name: "draft filing: no event", run: func(s *FilingStore) (Filing, error) {
			return s.RecordFiling(ctx, Filing{MatterID: "m1", Court: "Superior Court"})
		}},
		{name: "affidavit of service", run: func(s *FilingStore) (Filing, error) { return s.RecordAffidavitOfService(ctx, filed) },
			wantType: EventAffidavitOfServiceReceived, wantPayload: `{"filing_id":"f1","matter_id":"m1","document_id":"d1","filed_on":"2026-03-02"}`},
		{name: "affidavit without its document", run: func(s *FilingStore) (Filing, error) {
			f := filed
			f.DocumentID = " "
			return s.RecordAffidavitOfService(ctx, f)
		}, wantErr: ErrInvalid},
		{name: "affidavit not filed", run: func(s *FilingStore) (Filing, error) {
			return s.RecordAffidavitOfService(ctx, Filing{MatterID: "m1", DocumentID: "d1", Court: "Superior Court"})
		}, wantErr: ErrInvalid},
		{name: "affidavit with no emitter", noEvents: true, run: func(s *FilingStore) (Filing, error) { return s.RecordAffidavitOfService(ctx, filed) },
			wantErr: ErrInvalid},
		{name: "claim issued", stored: true, run: func(s *FilingStore) (Filing, error) { return s.IssueClaim(ctx, "f1", " CV-26-001 ", issuedOn) },
			wantType: EventClaimIssued, wantPayload: `{"filing_id":"f1","matter_id":"m1","document_id":"d1","court_file_no":"CV-26-001","issued_on":"2026-03-04"}`},
		{name: "claim on an unfiled filing", run: func(s *FilingStore) (Filing, error) { return s.IssueClaim(ctx, "f1", "CV-26-001", issuedOn) },
			wantErr: ErrNotFound},
		{name: "claim without a court file number", stored: true, run: func(s *FilingStore) (Filing, error) { return s.IssueClaim(ctx, "f1", " ", issuedOn) },
			wantErr: ErrInvalid},
		{name: "claim without a date", stored: true, run: func(s *FilingStore) (Filing, error) { return s.IssueClaim(ctx, "f1", "CV-26-001", time.Time{}) },
			wantErr: ErrInvalid},
		{name: "claim with no emitter", noEvents: true, stored: true, run: func(s *FilingStore) (Filing, error) {
			return s.IssueClaim(ctx, "f1", "CV-26-001", issuedOn)
		}, wantErr: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := outboxfake.New()
			s := NewFilingStore(rowDB{Fake: dbfake.New(), tx: rowTx{filed: tt.stored}})
			if !tt.noEvents {
				s.Events = ev
			}
			got, err := tt.run(s)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			msgs := ev.Messages()
			if tt.wantType == "" {
				if len(msgs) != 0 {
					t.Fatalf("emitted %+v, want none", msgs)
				}
				return
			}
			if got.ID != "f1" || len(msgs) != 1 || msgs[0].Type != tt.wantType ||
				msgs[0].Subject != (outbox.Subject{Kind: SubjectCourtFiling, ID: "f1"}) ||
				string(msgs[0].Payload) != tt.wantPayload {
				t.Fatalf("filing %+v, events %+v", got, msgs)
			}
		})
	}
}
