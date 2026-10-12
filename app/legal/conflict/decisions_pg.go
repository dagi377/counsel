package conflict

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/db"
)

// PGDecisions is the Postgres DecisionStore over conflict.hit_disposition, conflict.hit_consent
// and conflict.run_decision (5204_conflict_record). Each write locks the run row, so a
// disposition, a consent and the decision on one run are applied one after the other, and a
// decision checks the clearing rules on the rows it commits with. Every table is append-only.
type PGDecisions struct {
	db db.DB
}

var _ DecisionStore = (*PGDecisions)(nil)

// NewPGDecisions returns a PGDecisions on d.
func NewPGDecisions(d db.DB) *PGDecisions { return &PGDecisions{db: d} }

const (
	decLockRunSQL = `SELECT id::text FROM conflict.check_run WHERE id = $1::uuid FOR UPDATE`
	decDecidedSQL = `SELECT EXISTS (SELECT 1 FROM conflict.run_decision WHERE run_id = $1::uuid)`
	decHitSQL     = `SELECT id::text FROM conflict.hit WHERE run_id = $1::uuid AND ordinal = $2`
	dispInsertSQL = `INSERT INTO conflict.hit_disposition (run_id, hit_id, ordinal, disposition, note, disposed_by, disposed_at)
  VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::uuid, $7)`
	// dispHitSQL keeps conflict.hit's own disposition columns on the latest disposition, for
	// readers of the contract table; the history is conflict.hit_disposition.
	dispHitSQL = `UPDATE conflict.hit SET disposition = $2, disposition_note = $3, disposed_by = $4::uuid, disposed_at = $5
  WHERE id = $1::uuid`
	dispCountSQL  = `SELECT count(DISTINCT ordinal) FROM conflict.hit_disposition WHERE run_id = $1::uuid`
	dispLatestSQL = `SELECT DISTINCT ON (ordinal) ordinal, disposition, note, disposed_by::text, disposed_at
  FROM conflict.hit_disposition WHERE run_id = $1::uuid ORDER BY ordinal, disposed_at DESC, id DESC`
	consentInsertSQL = `INSERT INTO conflict.hit_consent (run_id, hit_id, ordinal, type, status, disclosure_summary, contact_id,
  document_id, signed_at, recorded_by, recorded_at)
  VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, nullif($7, '')::uuid, nullif($8, '')::uuid, $9, $10::uuid, $11)`
	consentLatestSQL = `SELECT DISTINCT ON (ordinal) ordinal, type, status, disclosure_summary, coalesce(contact_id::text, ''),
  coalesce(document_id::text, ''), signed_at, recorded_by::text, recorded_at
  FROM conflict.hit_consent WHERE run_id = $1::uuid ORDER BY ordinal, recorded_at DESC, id DESC`
	decisionInsertSQL = `INSERT INTO conflict.run_decision (run_id, decision, reason, decided_by, decided_at)
  VALUES ($1::uuid, $2, $3, $4::uuid, $5)`
	decisionSQL = `SELECT decision, reason, decided_by::text, decided_at FROM conflict.run_decision WHERE run_id = $1::uuid`
)

// lockOpenRun locks run id and fails with ErrNotFound for an unknown run and ErrDecided for a
// decided one.
func lockOpenRun(ctx context.Context, tx pgx.Tx, id string) error {
	var got string
	if err := tx.QueryRow(ctx, decLockRunSQL, id).Scan(&got); err != nil {
		if missing(err) {
			return ErrNotFound
		}
		return err
	}
	var decided bool
	if err := tx.QueryRow(ctx, decDecidedSQL, id).Scan(&decided); err != nil {
		return err
	}
	if decided {
		return ErrDecided
	}
	return nil
}

// hitID returns the id of hit ordinal of run, or ErrBadDisposition.
func hitID(ctx context.Context, tx pgx.Tx, run string, ordinal int) (string, error) {
	var id string
	if err := tx.QueryRow(ctx, decHitSQL, run, ordinal).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrBadDisposition
		}
		return "", err
	}
	return id, nil
}

// Dispose implements DecisionStore.
func (s *PGDecisions) Dispose(ctx context.Context, d DispositionRecord) (before, after int, err error) {
	err = s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := lockOpenRun(ctx, tx, d.RunID); err != nil {
			return err
		}
		hit, err := hitID(ctx, tx, d.RunID, d.Hit)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, dispCountSQL, d.RunID).Scan(&before); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, dispInsertSQL, d.RunID, hit, d.Hit, string(d.Disposition), d.Note, d.By, d.At); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, dispHitSQL, hit, string(d.Disposition), d.Note, d.By, d.At); err != nil {
			return err
		}
		return tx.QueryRow(ctx, dispCountSQL, d.RunID).Scan(&after)
	})
	if err != nil {
		return 0, 0, fmt.Errorf("store disposition: %w", err)
	}
	return before, after, nil
}

// Dispositions implements DecisionStore.
func (s *PGDecisions) Dispositions(ctx context.Context, runID string) (map[int]DispositionRecord, error) {
	var out map[int]DispositionRecord
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = latestDispositions(ctx, tx, runID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("read dispositions: %w", err)
	}
	return out, nil
}

func latestDispositions(ctx context.Context, tx pgx.Tx, runID string) (map[int]DispositionRecord, error) {
	rows, err := tx.Query(ctx, dispLatestSQL, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]DispositionRecord{}
	for rows.Next() {
		var (
			d       DispositionRecord
			ordinal int32
			disp    string
		)
		if err := rows.Scan(&ordinal, &disp, &d.Note, &d.By, &d.At); err != nil {
			return nil, err
		}
		d.RunID, d.Hit, d.Disposition = runID, int(ordinal), Disposition(disp)
		out[d.Hit] = d
	}
	return out, rows.Err()
}

// RecordConsent implements DecisionStore.
func (s *PGDecisions) RecordConsent(ctx context.Context, c Consent) error {
	var signed *time.Time
	if !c.SignedAt.IsZero() {
		signed = &c.SignedAt
	}
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := lockOpenRun(ctx, tx, c.RunID); err != nil {
			return err
		}
		hit, err := hitID(ctx, tx, c.RunID, c.Hit)
		if err != nil {
			return ErrBadConsent
		}
		_, err = tx.Exec(ctx, consentInsertSQL, c.RunID, hit, c.Hit, string(c.Type), string(c.Status), c.DisclosureSummary,
			c.ContactID, c.DocumentID, signed, c.RecordedBy, c.RecordedAt)
		var pg *pgconn.PgError
		if errors.As(err, &pg) && (pg.Code == "23503" || pg.Code == "23514" || pg.Code == "22P02") {
			return fmt.Errorf("%w: %s", ErrBadConsent, pg.ConstraintName)
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("store consent: %w", err)
	}
	return nil
}

// Consents implements DecisionStore.
func (s *PGDecisions) Consents(ctx context.Context, runID string) (map[int]Consent, error) {
	var out map[int]Consent
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = latestConsents(ctx, tx, runID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("read consents: %w", err)
	}
	return out, nil
}

func latestConsents(ctx context.Context, tx pgx.Tx, runID string) (map[int]Consent, error) {
	rows, err := tx.Query(ctx, consentLatestSQL, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]Consent{}
	for rows.Next() {
		var (
			c           Consent
			ordinal     int32
			typ, status string
			signed      *time.Time
		)
		if err := rows.Scan(&ordinal, &typ, &status, &c.DisclosureSummary, &c.ContactID, &c.DocumentID, &signed,
			&c.RecordedBy, &c.RecordedAt); err != nil {
			return nil, err
		}
		c.RunID, c.Hit, c.Type, c.Status = runID, int(ordinal), ConsentType(typ), ConsentStatus(status)
		if signed != nil {
			c.SignedAt = *signed
		}
		out[c.Hit] = c
	}
	return out, rows.Err()
}

// Decide implements DecisionStore. The run is locked, so no disposition or consent lands
// between the check and the decision.
func (s *PGDecisions) Decide(ctx context.Context, d DecisionRecord, check ClearCheck) error {
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := lockOpenRun(ctx, tx, d.RunID); err != nil {
			return err
		}
		if check != nil {
			disps, err := latestDispositions(ctx, tx, d.RunID)
			if err != nil {
				return err
			}
			consents, err := latestConsents(ctx, tx, d.RunID)
			if err != nil {
				return err
			}
			if err := check(disps, consents); err != nil {
				return err
			}
		}
		outcome := "reject"
		if d.Approve {
			outcome = "clear"
		}
		_, err := tx.Exec(ctx, decisionInsertSQL, d.RunID, outcome, d.Reason, d.By, d.At)
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return ErrDecided
		}
		return err
	})
	if err != nil {
		if errors.Is(err, ErrUndisposedHits) || errors.Is(err, ErrConsentMissing) {
			return err
		}
		return fmt.Errorf("store decision: %w", err)
	}
	return nil
}

// Decision implements DecisionStore.
func (s *PGDecisions) Decision(ctx context.Context, runID string) (DecisionRecord, bool, error) {
	var (
		out     DecisionRecord
		found   bool
		outcome string
	)
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, decisionSQL, runID).Scan(&outcome, &out.Reason, &out.By, &out.At)
		if missing(err) {
			return nil
		}
		if err != nil {
			return err
		}
		found = true
		return nil
	})
	if err != nil {
		return DecisionRecord{}, false, fmt.Errorf("read decision: %w", err)
	}
	if !found {
		return DecisionRecord{}, false, nil
	}
	out.RunID, out.Approve = runID, outcome == "clear"
	return out, true, nil
}
