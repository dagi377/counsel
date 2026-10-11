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

// PGRuns is the Postgres RunStore over conflict.check_run and conflict.hit. Runs are
// append-only: Append inserts a run and its hits in one transaction, UNIQUE(check_id,
// version) refuses a second run with the same version (ErrRunExists), and nothing is
// updated. A run or hit missing a value the tables need is refused (ErrInvalidRun); nothing
// is defaulted.
type PGRuns struct {
	db db.DB
}

var _ RunStore = (*PGRuns)(nil)

// NewPGRuns returns a PGRuns on d.
func NewPGRuns(d db.DB) *PGRuns { return &PGRuns{db: d} }

// matchedKinds and matchTypes mirror the conflict.hit CHECK constraints.
var (
	matchedKinds = map[string]bool{"contact": true, "party_role": true, "sealed_prospect": true, "lateral_disclosure": true}
	matchTypes   = map[string]bool{"exact": true, "fuzzy": true, "phonetic": true, "identifier": true, "relationship": true}
)

// validRun refuses a run or hit missing any value the tables need.
func validRun(r Run) error {
	if r.CheckID == "" || r.MatterID == "" || r.PreparedBy == "" || r.IndexVersion <= 0 || !r.Complete ||
		r.Version < 0 || r.RanAt.IsZero() || len(r.Terms) == 0 {
		return ErrInvalidRun
	}
	for i, h := range r.Hits {
		if h.Term == "" || h.PartyID == "" || h.CheckPartyID == "" || !matchedKinds[h.MatchedKind] ||
			!matchTypes[h.MatchType] || h.ScorePPM <= 0 || h.ScorePPM > 1_000_000 {
			return fmt.Errorf("hit %d: %w", i, ErrInvalidRun)
		}
	}
	return nil
}

const (
	runLockCheckSQL = `SELECT matter_id::text FROM conflict.conflict_check WHERE id = $1::uuid FOR UPDATE`
	runNextSQL      = `SELECT coalesce(max(version), 0) + 1 FROM conflict.check_run WHERE check_id = $1::uuid`
	runInsertSQL    = `INSERT INTO conflict.check_run (check_id, version, index_version, ran_at, complete, prepared_by, terms)
  VALUES ($1::uuid, $2, $3, $4, $5, $6::uuid, $7) RETURNING id::text`
	// hitInsertSQL inserts a hit only for a check party of the run's own check.
	hitInsertSQL = `INSERT INTO conflict.hit (run_id, check_party_id, matched_kind, matched_id, match_type, score_ppm,
  ordinal, term, party_matter_id, party_role)
  SELECT $1::uuid, p.id, $3, $4::uuid, $5, $6, $7, $8, nullif($9, '')::uuid, nullif($10, '')
  FROM conflict.check_party p WHERE p.id = $2::uuid AND p.check_id = $11::uuid`
)

// Append implements RunStore. The check row is locked FOR UPDATE so concurrent runs of one
// check take versions one after the other; a zero Version takes the next one.
func (s *PGRuns) Append(ctx context.Context, r Run) (Run, error) {
	if err := validRun(r); err != nil {
		return Run{}, fmt.Errorf("append conflict run: %w", err)
	}
	out := r
	out.Terms = append([]string(nil), r.Terms...)
	out.Hits = append([]Hit(nil), r.Hits...)
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var matter string
		if err := tx.QueryRow(ctx, runLockCheckSQL, r.CheckID).Scan(&matter); err != nil {
			if missing(err) {
				return ErrNotFound
			}
			return err
		}
		// The run's matter must be the check's matter: Runs reads runs by the check's matter.
		if matter != r.MatterID {
			return ErrInvalidRun
		}
		if out.Version == 0 {
			if err := tx.QueryRow(ctx, runNextSQL, r.CheckID).Scan(&out.Version); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(ctx, runInsertSQL, r.CheckID, out.Version, r.IndexVersion, r.RanAt, r.Complete,
			r.PreparedBy, out.Terms).Scan(&out.ID); err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return ErrRunExists
			}
			return err
		}
		for i, h := range out.Hits {
			tag, err := tx.Exec(ctx, hitInsertSQL, out.ID, h.CheckPartyID, h.MatchedKind, h.PartyID, h.MatchType, h.ScorePPM,
				i, h.Term, h.MatterID, h.Role, r.CheckID)
			if err != nil {
				return fmt.Errorf("hit %d: %w", i, err)
			}
			if tag.RowsAffected() != 1 {
				return fmt.Errorf("hit %d: check party is not on this check: %w", i, ErrInvalidRun)
			}
		}
		return nil
	})
	if err != nil {
		return Run{}, fmt.Errorf("append conflict run: %w", err)
	}
	return out, nil
}

const (
	runColumns = `r.id::text, c.matter_id::text, r.check_id::text, r.version, r.index_version, r.ran_at, r.complete,
  coalesce(r.prepared_by::text, ''), coalesce(r.terms, '{}')`
	runsByMatterSQL = `SELECT ` + runColumns + `
  FROM conflict.check_run r JOIN conflict.conflict_check c ON c.id = r.check_id
  WHERE c.matter_id = $1::uuid ORDER BY r.ran_at, r.check_id, r.version`
	runByIDSQL = `SELECT ` + runColumns + `
  FROM conflict.check_run r JOIN conflict.conflict_check c ON c.id = r.check_id
  WHERE r.id = $1::uuid`
	hitsSQL = `SELECT coalesce(term, ''), matched_id::text, coalesce(party_matter_id::text, ''), coalesce(party_role, ''),
  check_party_id::text, matched_kind, match_type, score_ppm, ordinal
  FROM conflict.hit WHERE run_id = $1::uuid ORDER BY ordinal`
)

// Runs implements RunStore.
func (s *PGRuns) Runs(ctx context.Context, matterID string) ([]Run, error) {
	var out []Run
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, runsByMatterSQL, matterID)
		if err != nil {
			return err
		}
		for rows.Next() {
			r, err := scanRun(rows)
			if err != nil {
				rows.Close()
				return err
			}
			out = append(out, r)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for i := range out {
			if out[i].Hits, err = readHits(ctx, tx, out[i].ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "22P02" {
			return nil, nil // a malformed matter id has no runs
		}
		return nil, fmt.Errorf("conflict runs: %w", err)
	}
	return out, nil
}

// Get implements RunStore.
func (s *PGRuns) Get(ctx context.Context, id string) (Run, error) {
	var out Run
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := scanRun(tx.QueryRow(ctx, runByIDSQL, id))
		if err != nil {
			if missing(err) {
				return ErrNotFound
			}
			return err
		}
		if r.Hits, err = readHits(ctx, tx, r.ID); err != nil {
			return err
		}
		out = r
		return nil
	})
	if err != nil {
		return Run{}, fmt.Errorf("conflict run %s: %w", id, err)
	}
	return out, nil
}

// scanRun reads one row selected with runColumns. A stored run without the detail columns
// (written before 5200_check_run_detail) has no preparer and fails closed.
func scanRun(row pgx.Row) (Run, error) {
	var (
		r       Run
		version int32
		ranAt   time.Time
	)
	if err := row.Scan(&r.ID, &r.MatterID, &r.CheckID, &version, &r.IndexVersion, &ranAt, &r.Complete,
		&r.PreparedBy, &r.Terms); err != nil {
		return Run{}, err
	}
	if r.PreparedBy == "" {
		return Run{}, ErrInvalidRun
	}
	r.Version = int(version)
	r.RanAt = ranAt
	r.Status = StatusOpen
	return r, nil
}

// readHits reads the hits of run id in their stored order; a gap in the order fails closed.
func readHits(ctx context.Context, tx pgx.Tx, id string) ([]Hit, error) {
	rows, err := tx.Query(ctx, hitsSQL, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var (
			h       Hit
			score   int32
			ordinal *int32
		)
		if err := rows.Scan(&h.Term, &h.PartyID, &h.MatterID, &h.Role, &h.CheckPartyID, &h.MatchedKind, &h.MatchType,
			&score, &ordinal); err != nil {
			return nil, err
		}
		if ordinal == nil || int(*ordinal) != len(out) {
			return nil, fmt.Errorf("hit order: %w", ErrInvalidRun)
		}
		h.ScorePPM = int64(score)
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// missing reports a missing row or a malformed id.
func missing(err error) bool {
	var pg *pgconn.PgError
	return errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02")
}
