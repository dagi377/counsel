package conflict

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// Hit kinds, matching the conflict.hit.matched_kind values.
const (
	HitKindContact        = "contact"         // a firm contact's name or alias matched; no conflict-relevant role
	HitKindPartyRole      = "party_role"      // a contact matched and has this role on a matter or lead
	HitKindSealedProspect = "sealed_prospect" // a sealed, declined prospect matched; nothing else is disclosed
)

// IndexHit is one Hit with the kind of index record that matched.
type IndexHit struct {
	Hit
	Kind string
}

// IndexResult is every hit for one term and the index version they were read at.
type IndexResult struct {
	Hits         []IndexHit
	IndexVersion int64 // conflict.check_run.index_version
}

var _ Searcher = (*PGSearcher)(nil)

// PGSearcher is the Postgres Searcher over party.contact legal names, display names and
// aliases, the conflict-relevant matter.party_role rows of the matching contacts, and the
// names on conflict.sealed_prospect. Names compare normalised: diacritics folded, case
// folded, surrounding and repeated whitespace collapsed. It reads names, ids and roles only:
// never notes, descriptions or any conversation text.
//
// The three reads are separate statements in one transaction (modules never join another
// module's tables), so the hits and the index version describe the same snapshot.
type PGSearcher struct {
	db db.DB
}

// NewPGSearcher returns a PGSearcher on d.
func NewPGSearcher(d db.DB) *PGSearcher { return &PGSearcher{db: d} }

// foldPairs maps each accented letter to its unaccented ASCII letter. The same table drives
// NormaliseName in Go and translate() in SQL, so both sides fold identically.
var foldPairs = [][2]string{
	{"ÀÁÂÃÄÅĀĂĄ", "AAAAAAAAA"}, {"àáâãäåāăą", "aaaaaaaaa"},
	{"ÇĆĈĊČ", "CCCCC"}, {"çćĉċč", "ccccc"},
	{"ĎĐ", "DD"}, {"ďđ", "dd"},
	{"ÈÉÊËĒĔĖĘĚ", "EEEEEEEEE"}, {"èéêëēĕėęě", "eeeeeeeee"},
	{"ĜĞĠĢ", "GGGG"}, {"ĝğġģ", "gggg"},
	{"ĤĦ", "HH"}, {"ĥħ", "hh"},
	{"ÌÍÎÏĨĪĬĮİ", "IIIIIIIII"}, {"ìíîïĩīĭįı", "iiiiiiiii"},
	{"Ĵ", "J"}, {"ĵ", "j"},
	{"Ķ", "K"}, {"ķ", "k"},
	{"ĹĻĽĿŁ", "LLLLL"}, {"ĺļľŀł", "lllll"},
	{"ÑŃŅŇ", "NNNN"}, {"ñńņň", "nnnn"},
	{"ÒÓÔÕÖØŌŎŐ", "OOOOOOOOO"}, {"òóôõöøōŏő", "ooooooooo"},
	{"ŔŖŘ", "RRR"}, {"ŕŗř", "rrr"},
	{"ŚŜŞŠ", "SSSS"}, {"śŝşš", "ssss"},
	{"ŢŤŦ", "TTT"}, {"ţťŧ", "ttt"},
	{"ÙÚÛÜŨŪŬŮŰŲ", "UUUUUUUUUU"}, {"ùúûüũūŭůűų", "uuuuuuuuuu"},
	{"Ŵ", "W"}, {"ŵ", "w"},
	{"ÝŶŸ", "YYY"}, {"ýÿŷ", "yyy"},
	{"ŹŻŽ", "ZZZ"}, {"źżž", "zzz"},
}

// foldFrom and foldTo are the translate() arguments built from foldPairs.
var foldFrom, foldTo, foldMap = buildFold()

func buildFold() (string, string, map[rune]rune) {
	var from, to strings.Builder
	m := map[rune]rune{}
	for _, p := range foldPairs {
		f, t := []rune(p[0]), []rune(p[1])
		if len(f) != len(t) {
			panic("conflict: fold table row " + p[0] + " has unequal lengths")
		}
		for i := range f {
			m[f[i]] = t[i]
		}
		from.WriteString(p[0])
		to.WriteString(p[1])
	}
	return from.String(), to.String(), m
}

// NormaliseName folds diacritics and case and collapses whitespace, as the search SQL does.
func NormaliseName(s string) string {
	folded := strings.Map(func(r rune) rune {
		if t, ok := foldMap[r]; ok {
			return t
		}
		return r
	}, s)
	return strings.Join(strings.Fields(strings.ToLower(folded)), " ")
}

// normSQL is the SQL twin of NormaliseName for column x: $2 and $3 are foldFrom and foldTo.
func normSQL(x string) string {
	return `regexp_replace(btrim(lower(translate(` + x + `, $2, $3))), '\s+', ' ', 'g')`
}

// searchVersionSQL is the index version: the oldest transaction still running when the
// search snapshot was taken. It only grows, so a later run never reports an older index.
const searchVersionSQL = `SELECT pg_snapshot_xmin(pg_current_snapshot())::text::bigint`

// searchContactsSQL finds contacts whose legal name, display name or any alias matches the
// normalised term $1. Archived and merged contacts still count: former clients and
// adverse parties are exactly what a conflict search must find.
var searchContactsSQL = `SELECT c.id::text
FROM party.contact c
WHERE ` + normSQL("c.legal_name") + ` = $1
   OR ` + normSQL("c.display_name") + ` = $1
   OR EXISTS (SELECT 1 FROM unnest(c.aliases) AS a(name) WHERE ` + normSQL("a.name") + ` = $1)
ORDER BY c.id`

// searchRolesSQL lists the conflict-relevant roles of contacts $1, archived roles included
// (former matters). It reads ids and the side or role only, never notes.
const searchRolesSQL = `SELECT r.contact_id::text, COALESCE(r.matter_id::text, ''), COALESCE(r.side, r.role)
FROM matter.party_role r
WHERE r.contact_id = ANY($1::uuid[]) AND r.conflict_relevant
ORDER BY r.contact_id, r.matter_id, r.id`

// searchSealedSQL counts sealed prospects whose names or adverse names match the term. Only
// the count leaves the database: no id, name or nature of a sealed prospect is disclosed.
var searchSealedSQL = `SELECT count(*)
FROM conflict.sealed_prospect s
WHERE EXISTS (SELECT 1 FROM unnest(s.names || s.adverse_names) AS n(name) WHERE ` + normSQL("n.name") + ` = $1)`

// Search returns the hits for term. A sealed prospect hit carries only its kind, in Role,
// with no party or matter, so G2 still needs a disposition for it without disclosing it.
func (s *PGSearcher) Search(ctx context.Context, term string) ([]Hit, error) {
	res, err := s.SearchIndex(ctx, term)
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(res.Hits))
	for _, h := range res.Hits {
		hits = append(hits, h.Hit)
	}
	return hits, nil
}

// SearchIndex returns the hits for term with their kinds and the index version they were
// read at. Errors never carry the term.
func (s *PGSearcher) SearchIndex(ctx context.Context, term string) (IndexResult, error) {
	norm := NormaliseName(term)
	if norm == "" {
		return IndexResult{}, fmt.Errorf("conflict search: %w", ErrNoTerms)
	}
	if s == nil || s.db == nil {
		return IndexResult{}, errors.New("conflict search: no database")
	}
	var res IndexResult
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		r, err := searchTx(ctx, tx, term, norm)
		res = r
		return err
	})
	if err != nil {
		return IndexResult{}, err
	}
	return res, nil
}

func searchTx(ctx context.Context, tx pgx.Tx, term, norm string) (IndexResult, error) {
	var res IndexResult
	if err := tx.QueryRow(ctx, searchVersionSQL).Scan(&res.IndexVersion); err != nil {
		return IndexResult{}, fmt.Errorf("conflict search: index version: %w", err)
	}
	if res.IndexVersion <= 0 {
		return IndexResult{}, errors.New("conflict search: no index version")
	}
	contacts, err := queryStrings(ctx, tx, searchContactsSQL, norm, foldFrom, foldTo)
	if err != nil {
		return IndexResult{}, fmt.Errorf("conflict search: contacts: %w", err)
	}
	if len(contacts) > 0 {
		hits, err := roleHits(ctx, tx, term, contacts)
		if err != nil {
			return IndexResult{}, err
		}
		res.Hits = append(res.Hits, hits...)
	}
	var sealed int64
	if err := tx.QueryRow(ctx, searchSealedSQL, norm, foldFrom, foldTo).Scan(&sealed); err != nil {
		return IndexResult{}, fmt.Errorf("conflict search: sealed prospects: %w", err)
	}
	if sealed < 0 {
		return IndexResult{}, errors.New("conflict search: negative sealed prospect count")
	}
	for range sealed {
		res.Hits = append(res.Hits, IndexHit{Hit: Hit{Term: term, Role: HitKindSealedProspect}, Kind: HitKindSealedProspect})
	}
	return res, nil
}

// roleHits turns matching contacts into hits: one per conflict-relevant role, or one
// contact hit for a contact with no such role.
func roleHits(ctx context.Context, tx pgx.Tx, term string, contacts []string) ([]IndexHit, error) {
	rows, err := tx.Query(ctx, searchRolesSQL, contacts)
	if err != nil {
		return nil, fmt.Errorf("conflict search: roles: %w", err)
	}
	defer rows.Close()
	byContact := map[string][]IndexHit{}
	for rows.Next() {
		var contact, matter, role string
		if err := rows.Scan(&contact, &matter, &role); err != nil {
			return nil, fmt.Errorf("conflict search: roles: %w", err)
		}
		byContact[contact] = append(byContact[contact], IndexHit{
			Hit:  Hit{Term: term, PartyID: contact, MatterID: matter, Role: role},
			Kind: HitKindPartyRole,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("conflict search: roles: %w", err)
	}
	var hits []IndexHit
	for _, c := range contacts {
		if hs, ok := byContact[c]; ok {
			hits = append(hits, hs...)
			continue
		}
		hits = append(hits, IndexHit{Hit: Hit{Term: term, PartyID: c}, Kind: HitKindContact})
	}
	return hits, nil
}

// queryStrings runs sql with args and returns its single text column.
func queryStrings(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]string, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
