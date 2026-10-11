//go:build integration

package conflict

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/migrations"
)

// searchPGDB creates a migrated database on COUNSEL_TEST_DB and returns a pool on it.
func searchPGDB(t *testing.T) *db.Pool {
	t.Helper()
	dsn := os.Getenv("COUNSEL_TEST_DB")
	if dsn == "" {
		t.Skip("COUNSEL_TEST_DB not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "conflict_search_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.ConnConfig.Database = name
	conn, err := pgx.ConnectConfig(ctx, cfg.ConnConfig)
	if err != nil {
		t.Fatalf("connect %s: %v", name, err)
	}
	err = migrations.Apply(ctx, conn, "")
	_ = conn.Close(ctx)
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	pool, err := db.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// Roles are covered by the scripted-row tests; seeding a matter needs most of the schema.
func TestPGSearcherIntegration(t *testing.T) {
	pool := searchPGDB(t)
	ctx := context.Background()
	ids := map[string]string{}
	err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		contacts := []struct {
			key, kind, legal, display string
			aliases                   []string
		}{
			{"acme", "organization", "Acme Société Inc", "Acme", []string{"ACME Holdings"}},
			{"dupre", "person", "François Dupré", "F. Dupré", nil},
			{"lone", "person", "Lone Contact", "Lone Contact", nil},
		}
		for _, c := range contacts {
			var id string
			if err := tx.QueryRow(ctx, `INSERT INTO party.contact(kind, legal_name, display_name, aliases)
VALUES ($1, $2, $3, $4) RETURNING id::text`, c.kind, c.legal, c.display, c.aliases).Scan(&id); err != nil {
				return err
			}
			ids[c.key] = id
		}
		if _, err := tx.Exec(ctx, `INSERT INTO conflict.sealed_prospect(names, adverse_names, nature, declined_at)
VALUES ('{"Sealed Prospect Ltd"}', '{"Dupré Holdings"}', 'secret nature', now())`); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	s := NewPGSearcher(pool)
	tests := []struct {
		name  string
		term  string
		kinds []string
		party string
	}{
		{name: "legal name, case and diacritics folded", term: "acme societe inc", kinds: []string{HitKindContact}, party: ids["acme"]},
		{name: "alias", term: "  acme   holdings ", kinds: []string{HitKindContact}, party: ids["acme"]},
		{name: "person without diacritics", term: "FRANCOIS DUPRE", kinds: []string{HitKindContact}, party: ids["dupre"]},
		{name: "contact with no role", term: "lone contact", kinds: []string{HitKindContact}, party: ids["lone"]},
		{name: "sealed prospect name", term: "sealed prospect ltd", kinds: []string{HitKindSealedProspect}},
		{name: "sealed adverse name, accent folded", term: "dupre holdings", kinds: []string{HitKindSealedProspect}},
		{name: "no match", term: "nobody at all"},
	}
	var lastVersion int64
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := s.SearchIndex(ctx, tt.term)
			if err != nil {
				t.Fatalf("SearchIndex: %v", err)
			}
			if res.IndexVersion <= 0 || res.IndexVersion < lastVersion {
				t.Fatalf("index version = %d after %d", res.IndexVersion, lastVersion)
			}
			lastVersion = res.IndexVersion
			var kinds []string
			for _, h := range res.Hits {
				kinds = append(kinds, h.Kind)
				if h.Term != tt.term {
					t.Fatalf("hit term = %q, want %q", h.Term, tt.term)
				}
				switch h.Kind {
				case HitKindSealedProspect:
					if h.PartyID != "" || h.MatterID != "" || h.Role != HitKindSealedProspect {
						t.Fatalf("sealed prospect disclosed: %+v", h)
					}
				default:
					if h.PartyID != tt.party {
						t.Fatalf("party = %q, want %q", h.PartyID, tt.party)
					}
				}
			}
			sort.Strings(kinds)
			if len(kinds) != len(tt.kinds) {
				t.Fatalf("kinds = %v, want %v", kinds, tt.kinds)
			}
			for i := range kinds {
				if kinds[i] != tt.kinds[i] {
					t.Fatalf("kinds = %v, want %v", kinds, tt.kinds)
				}
			}
		})
	}
}
