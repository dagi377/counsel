//go:build integration

package webhook_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/webhook"
	"github.com/dagi377/counsel/migrations"
)

const evtType = "work.item_changed"

// queueDB creates a fresh database on COUNSEL_TEST_DB with every migration applied and
// returns a pool on it.
func queueDB(t *testing.T) *db.Pool {
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
	name := "webhook_deliver_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	ccfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ccfg.Database = name
	conn, err := pgx.ConnectConfig(ctx, ccfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, conn, ""); err != nil {
		t.Fatal(err)
	}
	_ = conn.Close(ctx)
	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pcfg.ConnConfig.Database = name
	pool, err := db.OpenConfig(ctx, pcfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

type fixture struct {
	pool  *db.Pool
	q     *webhook.Queue
	hooks map[string]string // name -> platform.webhook id
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	pool := queueDB(t)
	types := webhook.NewTypes()
	if err := types.Register(webhook.EventType{Name: evtType, Schema: []byte(`{"type":"object","required":["n"],"properties":{"n":{"type":"integer"}}}`)}); err != nil {
		t.Fatal(err)
	}
	f := fixture{pool: pool, q: webhook.NewQueue(pool, types, webhook.OutboxPayloads{}), hooks: map[string]string{}}
	for _, h := range []struct{ name, events, status string }{
		{"all", "{}", "active"},
		{"other", "{work.other_thing}", "active"},
		{"off", "{}", "disabled"},
	} {
		var id string
		err := pool.InTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`INSERT INTO platform.webhook (url, events, secret_ref, status)
				 VALUES ('https://example.test/hook', $1::text[], 'ref:test', $2) RETURNING id::text`,
				h.events, h.status).Scan(&id)
		})
		if err != nil {
			t.Fatalf("insert webhook %s: %v", h.name, err)
		}
		f.hooks[h.name] = id
	}
	return f
}

// emit writes the payload to the outbox and enqueues the message in the same transaction.
func (f fixture) emit(t *testing.T, subject string, n int) webhook.Message {
	t.Helper()
	m := webhook.Message{Type: evtType, Subject: webhook.Subject{Kind: "item", ID: subject}, Payload: json.RawMessage(`{"n":` + strconv.Itoa(n) + `}`)}
	err := f.pool.InTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO platform.outbox (topic, aggregate, aggregate_id, payload)
			 VALUES ($1, 'item', $2::uuid, $3) RETURNING id::text`,
			evtType, subject, []byte(m.Payload)).Scan(&m.ID); err != nil {
			return err
		}
		_, err := f.q.Enqueue(ctx, tx, m)
		return err
	})
	if err != nil {
		t.Fatalf("emit %s #%d: %v", subject, n, err)
	}
	return m
}

func ready(t *testing.T, f fixture) []webhook.Delivery {
	t.Helper()
	ds, err := f.q.Ready(context.Background(), f.hooks["all"])
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	return ds
}

const (
	subjA = "00000000-0000-7000-8000-0000000000a1"
	subjB = "00000000-0000-7000-8000-0000000000b1"
)

func TestDeliverOrderedPerEndpointAndSubject(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m1 := f.emit(t, subjA, 1)
	m2 := f.emit(t, subjA, 2)
	b1 := f.emit(t, subjB, 1)

	steps := []struct {
		name    string
		act     func(ds []webhook.Delivery) error
		wantIDs []string // message ids Ready hands out after act, in subject order
	}{
		{name: "one head per subject", wantIDs: []string{m1.ID, b1.ID}},
		{name: "failed head keeps message 2 waiting", act: func(ds []webhook.Delivery) error {
			return f.q.Fail(ctx, ds[0].ID)
		}, wantIDs: []string{m1.ID, b1.ID}},
		{name: "ack of message 1 releases message 2", act: func(ds []webhook.Delivery) error {
			return f.q.Ack(ctx, ds[0].ID)
		}, wantIDs: []string{m2.ID, b1.ID}},
		{name: "all acked", act: func(ds []webhook.Delivery) error {
			for _, d := range ds {
				if err := f.q.Ack(ctx, d.ID); err != nil {
					return err
				}
			}
			return nil
		}},
	}
	ds := ready(t, f)
	for _, st := range steps {
		if st.act != nil {
			if err := st.act(ds); err != nil {
				t.Fatalf("%s: %v", st.name, err)
			}
		}
		ds = ready(t, f)
		if len(ds) != len(st.wantIDs) {
			t.Fatalf("%s: ready = %d deliveries, want %d", st.name, len(ds), len(st.wantIDs))
		}
		for i, d := range ds {
			if d.Message.ID != st.wantIDs[i] {
				t.Errorf("%s: ready[%d] = %s, want %s", st.name, i, d.Message.ID, st.wantIDs[i])
			}
			if len(d.Message.Payload) == 0 {
				t.Errorf("%s: ready[%d] has no payload", st.name, i)
			}
		}
	}
}

func TestDeliverAckOutOfOrderRefused(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.emit(t, subjA, 1)
	f.emit(t, subjA, 2)
	var second string
	err := f.pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT id::text FROM platform.webhook_delivery WHERE webhook_id = $1::uuid ORDER BY at DESC, id DESC LIMIT 1`,
			f.hooks["all"]).Scan(&second)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.q.Ack(ctx, webhook.DeliveryID(second)); !errors.Is(err, webhook.ErrOutOfOrder) {
		t.Fatalf("ack of message 2 before 1: err = %v, want ErrOutOfOrder", err)
	}
}

func TestDeliverEnqueueSubscriptionAndErrors(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.emit(t, subjA, 1)
	tests := []struct {
		name     string
		endpoint string
		want     int
		wantErr  error
	}{
		{name: "subscribed to all types", endpoint: f.hooks["all"], want: 1},
		{name: "subscribed to another type", endpoint: f.hooks["other"], want: 0},
		{name: "disabled endpoint", endpoint: f.hooks["off"], want: 0},
		{name: "unknown endpoint", endpoint: "00000000-0000-7000-8000-00000000dead", wantErr: webhook.ErrUnknownEndpoint},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ds, err := f.q.Ready(ctx, tc.endpoint)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(ds) != tc.want {
				t.Errorf("ready = %d, want %d", len(ds), tc.want)
			}
		})
	}
	// Enqueueing the same message again adds nothing.
	err := f.pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		ids, err := f.q.Enqueue(ctx, tx, m)
		if err == nil && len(ids) != 0 {
			t.Errorf("re-enqueue queued %d deliveries, want 0", len(ids))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	bad := []struct {
		name string
		m    webhook.Message
		want error
	}{
		{name: "unregistered type", m: webhook.Message{ID: m.ID, Type: "work.nope", Subject: m.Subject, Payload: m.Payload}, want: webhook.ErrUnknownType},
		{name: "payload fails schema", m: webhook.Message{ID: m.ID, Type: evtType, Subject: m.Subject, Payload: json.RawMessage(`{}`)}, want: webhook.ErrInvalidPayload},
		{name: "no subject", m: webhook.Message{ID: m.ID, Type: evtType, Payload: m.Payload}, want: webhook.ErrInvalidPayload},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			err := f.pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				_, err := f.q.Enqueue(ctx, tx, tc.m)
				return err
			})
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
	if err := f.q.Ack(ctx, "00000000-0000-7000-8000-00000000dead"); !errors.Is(err, webhook.ErrNotFound) {
		t.Errorf("ack unknown: err = %v, want ErrNotFound", err)
	}
	if err := f.q.Fail(ctx, "00000000-0000-7000-8000-00000000dead"); !errors.Is(err, webhook.ErrNotFound) {
		t.Errorf("fail unknown: err = %v, want ErrNotFound", err)
	}
}
