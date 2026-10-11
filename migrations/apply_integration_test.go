//go:build integration

package migrations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// freshDB creates an empty database on COUNSEL_TEST_DB, applies the migrations up to upTo
// (all when empty) and returns a connection to it.
func freshDB(t *testing.T, upTo string) *pgx.Conn {
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
	name := "migrations_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.Database = name
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect %s: %v", name, err)
	}
	t.Cleanup(func() { conn.Close(context.Background()) })
	if err := Apply(ctx, conn, upTo); err != nil {
		t.Fatal(err)
	}
	return conn
}

// seed is the fixture every behaviour check shares: two human users, a matter, a workflow.
const seed = `
INSERT INTO core.firm(legal_name, display_name) VALUES ('F LLP', 'F');
INSERT INTO core.role(id, key, name, status) VALUES ('00000000-0000-7000-8000-000000000001', 'r', 'R', 'active');
INSERT INTO core.app_user(id, email, display_name, role_id, licence_type, lso_number, status, principal_kind) VALUES
 ('00000000-0000-7000-8000-00000000000a', 'a@x.ca', 'A', '00000000-0000-7000-8000-000000000001', 'lawyer', 'L1', 'active', 'human'),
 ('00000000-0000-7000-8000-00000000000b', 'b@x.ca', 'B', '00000000-0000-7000-8000-000000000001', 'lawyer', 'L2', 'active', 'human');
INSERT INTO party.contact(id, kind, legal_name, display_name)
 VALUES ('00000000-0000-7000-8000-0000000000c1', 'person', 'P', 'P');
INSERT INTO matter.practice_area(id, name) VALUES ('00000000-0000-7000-8000-0000000000a1', 'Area');
INSERT INTO matter.matter(id, number, short_name, client_id, practice_area_id, responsible_lawyer_id)
 VALUES ('00000000-0000-7000-8000-0000000000e1', '26-0001', 'M', '00000000-0000-7000-8000-0000000000c1',
         '00000000-0000-7000-8000-0000000000a1', '00000000-0000-7000-8000-00000000000a');
INSERT INTO work.workflow(id, name, version) VALUES ('00000000-0000-7000-8000-0000000000f1', 'W', 1);
INSERT INTO work.status(id, workflow_id, name, status_group) VALUES ('00000000-0000-7000-8000-0000000000f2', '00000000-0000-7000-8000-0000000000f1', 'Todo', 'todo');
INSERT INTO gl.account(code, name, type) VALUES ('1000', 'Bank', 'asset'), ('4000', 'Fees', 'revenue');
INSERT INTO gl.account(code, name, type, book) VALUES ('1500', 'Book bank', 'asset', 'trust'), ('2500', 'Book owed', 'liability', 'trust');
`

const userA = "00000000-0000-7000-8000-00000000000a"

func exec(t *testing.T, conn *pgx.Conn, sql string) {
	t.Helper()
	if _, err := conn.PgConn().Exec(context.Background(), sql).ReadAll(); err != nil {
		t.Fatalf("exec: %v\n%s", err, sql)
	}
}

// TestMigrationsAddColumns checks every additive column with its default or nullability.
func TestMigrationsAddColumns(t *testing.T) {
	conn := freshDB(t, "")
	tests := []struct {
		schema, table, column string
		nullable              bool
		defaultHas            string
	}{
		{"gl", "account", "book", false, "operating"},
		{"gl", "journal_entry", "book", false, "operating"},
		{"gl", "book_period", "book", false, ""},
		{"gate", "approval_request", "payload_jcs", true, ""},
		{"gate", "approval_request", "payload_sha256", true, ""},
		{"platform", "outbox", "subject_seq", true, ""},
		{"platform", "webhook_delivery", "subject", true, ""},
		{"platform", "webhook_delivery", "message_id", true, ""},
		{"platform", "webhook_delivery", "payload_ref", true, ""},
		{"platform", "webhook_delivery", "acked_at", true, ""},
		{"work", "work_item", "subject_kind", true, ""},
		{"work", "work_item", "subject_id", true, ""},
		{"legal", "court_filing", "court", false, ""},
		{"legal", "court_filing", "status", false, "draft"},
	}
	for _, tt := range tests {
		t.Run(tt.schema+"."+tt.table+"."+tt.column, func(t *testing.T) {
			var nullable string
			var def *string
			err := conn.QueryRow(context.Background(), `
SELECT is_nullable, column_default FROM information_schema.columns
 WHERE table_schema = $1 AND table_name = $2 AND column_name = $3`, tt.schema, tt.table, tt.column).Scan(&nullable, &def)
			if err != nil {
				t.Fatalf("column missing: %v", err)
			}
			if (nullable == "YES") != tt.nullable {
				t.Fatalf("is_nullable = %s, want nullable %v", nullable, tt.nullable)
			}
			if tt.defaultHas != "" && (def == nil || !strings.Contains(*def, tt.defaultHas)) {
				t.Fatalf("default = %v, want it to contain %q", def, tt.defaultHas)
			}
		})
	}
}

// expect runs sql in a savepoint-free transaction and checks the error mentions want
// (want == "" means success).
func expect(t *testing.T, conn *pgx.Conn, sql, want string) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, sql)
	if err == nil {
		err = tx.Commit(ctx)
	}
	switch {
	case want == "" && err != nil:
		t.Fatalf("%s: %v", sql, err)
	case want != "" && (err == nil || !strings.Contains(err.Error(), want)):
		t.Fatalf("%s: err = %v, want %q", sql, err, want)
	}
}

func journal(book, source, date string) string {
	return `WITH e AS (INSERT INTO gl.journal_entry(entry_date, memo, source_type, source_id, posted_by, book)
VALUES ('` + date + `', 'x', '` + source + `', core.uuidv7(), '` + userA + `', '` + book + `') RETURNING id)
INSERT INTO gl.journal_line(entry_id, account_id, debit_cents, credit_cents)
SELECT e.id, a.id, CASE WHEN a.type = 'asset' THEN 100 ELSE 0 END, CASE WHEN a.type = 'asset' THEN 0 ELSE 100 END
  FROM e, gl.account a WHERE a.book = '` + book + `'`
}

// TestLedgerBooksClosePerBook checks the trust source kind and that a close refuses only
// postings in its own book; a legacy gl.period row still closes the operating book.
func TestLedgerBooksClosePerBook(t *testing.T) {
	conn := freshDB(t, "")
	exec(t, conn, seed)
	exec(t, conn, `INSERT INTO gl.book_period(book, month, status, closed_by, closed_at)
VALUES ('trust', '2026-03-01', 'closed', '`+userA+`', now());
INSERT INTO gl.period(month, status, closed_by, closed_at) VALUES ('2026-05-01', 'closed', '`+userA+`', now());`)
	tests := []struct {
		name, book, source, date, want string
	}{
		{"trust source kind in the trust book", "trust", "trust", "2026-04-02", ""},
		{"closed trust month refuses trust", "trust", "trust", "2026-03-15", "DM-062"},
		{"closed trust month keeps operating open", "operating", "manual", "2026-03-15", ""},
		{"legacy period closes operating", "operating", "manual", "2026-05-10", "DM-062"},
		{"legacy period leaves trust open", "trust", "trust", "2026-05-10", ""},
		{"unknown source kind still refused", "operating", "nonsense", "2026-04-02", "check"},
		{"bad book name refused", "Not A Book", "manual", "2026-04-02", "journal_entry_book_ck"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expect(t, conn, journal(tt.book, tt.source, tt.date), tt.want)
		})
	}
	var operating int
	if err := conn.QueryRow(context.Background(),
		`SELECT count(*) FROM gl.operating_journal_entry WHERE book <> 'operating'`).Scan(&operating); err != nil || operating != 0 {
		t.Fatalf("operating view shows %d other-book entries (%v)", operating, err)
	}
	var closed bool
	if err := conn.QueryRow(context.Background(), `SELECT gl.period_closed('trust', '2026-03-31')`).Scan(&closed); err != nil || !closed {
		t.Fatalf("gl.period_closed(trust, March) = %v, %v", closed, err)
	}
}

// TestGatePayloadAndRecommend checks the stored payload pair and that 'recommend' is
// recorded without closing the request.
func TestGatePayloadAndRecommend(t *testing.T) {
	conn := freshDB(t, "")
	exec(t, conn, seed)
	open := func(id, jcs, sha string) string {
		return `INSERT INTO gate.approval_request(id, gate_code, spec_gate, payload_type, payload_id, payload_hash, requested_by, expires_at, payload_jcs, payload_sha256)
VALUES ('` + id + `', 'desk.refund', 'G9', 'desk.refund', core.uuidv7(), sha256('{"a":1}'::bytea), '` + userA + `', now() + interval '1 day', ` + jcs + `, ` + sha + `)`
	}
	tests := []struct {
		name, sql, want string
	}{
		{"payload with matching hash", open("00000000-0000-7000-8000-000000000101", `'{"a":1}'::bytea`, `sha256('{"a":1}'::bytea)`), ""},
		{"payload without hash", open("00000000-0000-7000-8000-000000000102", `'{"a":1}'::bytea`, "NULL"), "approval_request_payload_pair_ck"},
		{"hash of other bytes", open("00000000-0000-7000-8000-000000000103", `'{"a":2}'::bytea`, `sha256('{"a":2}'::bytea)`), "approval_request_payload_sha_ck"},
		{"legacy row without payload", open("00000000-0000-7000-8000-000000000104", "NULL", "NULL"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { expect(t, conn, tt.sql, tt.want) })
	}
	exec(t, conn, open("00000000-0000-7000-8000-000000000105", `'{"a":1}'::bytea`, `sha256('{"a":1}'::bytea)`))
	exec(t, conn, `INSERT INTO gate.gate_decision(approval_id, decided_by, decision, reason, payload_hash, mfa_verified_at)
VALUES ('00000000-0000-7000-8000-000000000105', '00000000-0000-7000-8000-00000000000b', 'recommend', 'above limit', sha256('{"a":1}'::bytea), now())`)
	var status string
	if err := conn.QueryRow(context.Background(),
		`SELECT status FROM gate.approval_request WHERE id = '00000000-0000-7000-8000-000000000105'`).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("status after recommend = %q, %v; want pending", status, err)
	}
	expect(t, conn, `INSERT INTO gate.gate_decision(approval_id, decided_by, decision, reason, payload_hash, mfa_verified_at)
VALUES ('00000000-0000-7000-8000-000000000105', '00000000-0000-7000-8000-00000000000a', 'endorse', 'x', sha256('{"a":1}'::bytea), now())`, "gate_decision_verdict_ck")
}

// TestOutboxWebhookAndFiling checks subject_seq uniqueness, the delivery pointer and the
// court filing rules.
func TestOutboxWebhookAndFiling(t *testing.T) {
	conn := freshDB(t, "")
	exec(t, conn, seed)
	exec(t, conn, `INSERT INTO platform.outbox(id, topic, aggregate, aggregate_id, payload, subject_seq)
VALUES ('00000000-0000-7000-8000-000000000201', 't', 'desk', '00000000-0000-7000-8000-000000000301', '{}', 1);
INSERT INTO platform.webhook(id, url, events, secret_ref) VALUES ('00000000-0000-7000-8000-000000000401', 'https://h.example/x', '{t}', 's');`)
	tests := []struct {
		name, sql, want string
	}{
		{"next seq of the subject", `INSERT INTO platform.outbox(topic, aggregate, aggregate_id, payload, subject_seq) VALUES ('t', 'desk', '00000000-0000-7000-8000-000000000301', '{}', 2)`, ""},
		{"same seq twice in one subject", `INSERT INTO platform.outbox(topic, aggregate, aggregate_id, payload, subject_seq) VALUES ('t', 'desk', '00000000-0000-7000-8000-000000000301', '{}', 1)`, "outbox_subject_seq_uq"},
		{"same seq in another subject", `INSERT INTO platform.outbox(topic, aggregate, aggregate_id, payload, subject_seq) VALUES ('t', 'desk', '00000000-0000-7000-8000-000000000302', '{}', 1)`, ""},
		{"seq zero", `INSERT INTO platform.outbox(topic, aggregate, aggregate_id, payload, subject_seq) VALUES ('t', 'desk', '00000000-0000-7000-8000-000000000303', '{}', 0)`, "outbox_subject_seq_ck"},
		{"delivery points at the outbox row", `INSERT INTO platform.webhook_delivery(webhook_id, event, subject, message_id, payload_ref, acked_at) VALUES ('00000000-0000-7000-8000-000000000401', 't', 'desk:1', core.uuidv7(), '00000000-0000-7000-8000-000000000201', now())`, ""},
		{"delivery points nowhere", `INSERT INTO platform.webhook_delivery(webhook_id, event, payload_ref) VALUES ('00000000-0000-7000-8000-000000000401', 't', core.uuidv7())`, "foreign key"},
		{"ack without message", `INSERT INTO platform.webhook_delivery(webhook_id, event, acked_at) VALUES ('00000000-0000-7000-8000-000000000401', 't', now())`, "webhook_delivery_ack_ck"},
		{"draft filing", `INSERT INTO legal.court_filing(matter_id, court) VALUES ('00000000-0000-7000-8000-0000000000e1', 'Superior')`, ""},
		{"filed without date", `INSERT INTO legal.court_filing(matter_id, court, status) VALUES ('00000000-0000-7000-8000-0000000000e1', 'Superior', 'filed')`, "check"},
		{"unknown filing status", `INSERT INTO legal.court_filing(matter_id, court, status) VALUES ('00000000-0000-7000-8000-0000000000e1', 'Superior', 'lost')`, "check"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { expect(t, conn, tt.sql, tt.want) })
	}
}

// TestWorkItemSubjectBackfill applies the kernel columns, adds items, then runs the app
// backfill: an item on a matter gets ('matter', matter_id); one without stays empty.
func TestWorkItemSubjectBackfill(t *testing.T) {
	conn := freshDB(t, "6000_court_filing.sql")
	exec(t, conn, seed)
	exec(t, conn, `INSERT INTO work.work_item(id, key, type, title, workflow_id, status_id, matter_id, reporter_id, rank) VALUES
 ('00000000-0000-7000-8000-000000000501', 'W-1', 'task', 'T', '00000000-0000-7000-8000-0000000000f1', '00000000-0000-7000-8000-0000000000f2', '00000000-0000-7000-8000-0000000000e1', '`+userA+`', 'a'),
 ('00000000-0000-7000-8000-000000000502', 'W-2', 'task', 'T', '00000000-0000-7000-8000-0000000000f1', '00000000-0000-7000-8000-0000000000f2', NULL, '`+userA+`', 'b');`)
	sql, err := Read("6100_work_item_subject_backfill.sql")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, conn, string(sql))
	exec(t, conn, string(sql)) // a rerun changes nothing
	tests := []struct {
		id       string
		wantKind *string
	}{
		{"00000000-0000-7000-8000-000000000501", ptr("matter")},
		{"00000000-0000-7000-8000-000000000502", nil},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			var kind *string
			var sameID bool
			if err := conn.QueryRow(context.Background(),
				`SELECT subject_kind, subject_id IS NOT DISTINCT FROM matter_id FROM work.work_item WHERE id = $1`, tt.id).Scan(&kind, &sameID); err != nil {
				t.Fatal(err)
			}
			if (kind == nil) != (tt.wantKind == nil) || (kind != nil && *kind != *tt.wantKind) || !sameID {
				t.Fatalf("subject = (%v, same id %v), want kind %v", kind, sameID, tt.wantKind)
			}
		})
	}
	expect(t, conn, `UPDATE work.work_item SET subject_kind = 'x' WHERE id = '00000000-0000-7000-8000-000000000502'`, "work_item_subject_pair_ck")
}

func ptr(s string) *string { return &s }
