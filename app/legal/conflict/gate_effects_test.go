package conflict

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/gates/gatesfake"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	tCheck = "00000000-0000-0000-0000-000000000001"
	tID    = "00000000-0000-0000-0000-000000000002"
	tOther = "00000000-0000-0000-0000-000000000003"
)

// rule: ARC-I-019
// rule: ARC-I-016
func TestGateEffects(t *testing.T) {
	if fx := GateEffects(nil); len(fx) != 6 {
		t.Fatalf("GateEffects() has %d kinds, want 6", len(fx))
	}
	gatesfake.RunEffectCases(t, []gatesfake.EffectCase{
		{Kind: GateClearance, Effect: applyClearance, Writes: "INSERT INTO conflict.clearance",
			Payload: map[string]any{"check_id": tCheck, "parties": []string{"Acme"}}},
		{Kind: GateConsentText, Effect: applyConsentText, Writes: "status = 'final'",
			Payload: consentTextPayload{CheckID: tCheck, DocumentVersionID: tID}},
		{Kind: GateWall, Effect: applyWall, Writes: "status = 'active'",
			Payload: wallPayload{WallID: tID, MatterID: tOther, ScreenedUserIDs: []string{tOther}, Action: "activate"}},
		{Kind: GateWall, Effect: applyWall, Writes: "status = 'lifted'",
			Payload: wallPayload{WallID: tID, MatterID: tOther, ScreenedUserIDs: []string{tOther}, Action: "lift"}},
		{Kind: GateLateral, Effect: applyLateral, Writes: "conflict.lateral_disclosure",
			Payload: lateralPayload{UserID: tOther, CheckID: tCheck}},
		{Kind: GateClientID, Effect: applyClientID, Writes: "status = 'verified'",
			Payload: clientIDPayload{ContactID: tID, Method: "government_photo_id", VerifiedOn: "2026-10-09"}},
		{Kind: GateIDDestruction, Effect: noStoredCopies, Writes: "UPDATE party.id_document", TwoApprovers: true,
			Payload: idDestructionPayload{ContactID: tID, RecordIDs: []string{tCheck, tOther, tCheck}}},
	})
}

// G14 redacts and stamps the ID artefacts; it never deletes the identity record, so the
// conflict-check history remains.
func TestIDDestructionKeepsRecord(t *testing.T) {
	g := registry.Approved{GateID: "00000000-0000-0000-0000-0000000000ff", Kind: GateIDDestruction,
		Preparer: gatesfake.Preparer, Approvers: []registry.Principal{gatesfake.Lawyer, gatesfake.Partner}}
	tests := []struct {
		name    string
		payload string
		wantErr error
		wantIDs []string
	}{
		{"records deduplicated", `{"contact_id":"` + tID + `","record_ids":["` + tOther + `","` + tCheck + `","` + tOther + `"]}`, nil, []string{tCheck, tOther}},
		{"no records", `{"contact_id":"` + tID + `","record_ids":[]}`, gates.ErrNotApplied, nil},
		{"no contact", `{"record_ids":["` + tCheck + `"]}`, gates.ErrNotApplied, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g.Payload = json.RawMessage(tt.payload)
			x := &gatesfake.Exec{StepRows: 1, ProofRows: 1}
			err := applyIDDestruction(context.Background(), &idTx{fake: x, stored: "[]"}, nil, g)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			for _, q := range x.SQL {
				if strings.Contains(strings.ToUpper(q), "DELETE") {
					t.Fatalf("statement deletes: %q", q)
				}
			}
			if tt.wantIDs == nil {
				return
			}
			if !strings.Contains(x.SQL[1], "destroyed_at = now()") || !strings.Contains(x.SQL[0], "number_ciphertext = '\\x'") {
				t.Fatalf("statements = %q, want redaction and a destroyed_at stamp", x.SQL)
			}
			if got := x.Args[0][0].([]string); !slices.Equal(got, tt.wantIDs) {
				t.Fatalf("record ids = %v, want %v", got, tt.wantIDs)
			}
		})
	}
}

// idTx is an idDestructionTx over gatesfake.Exec whose QueryRow answers the stored-copy read
// with stored (JSON). The read is not recorded, so fake.SQL holds the writes and the proof.
type idTx struct {
	fake   *gatesfake.Exec
	stored string
}

func (x *idTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return x.fake.Exec(ctx, sql, args...)
}

func (x *idTx) QueryRow(context.Context, string, ...any) pgx.Row { return idRow{x.stored} }

type idRow struct{ stored string }

func (r idRow) Scan(dest ...any) error {
	*dest[0].(*string) = r.stored
	return nil
}

// noStoredCopies runs the ID destruction effect for records with no stored copy.
func noStoredCopies(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	return applyIDDestruction(ctx, &idTx{fake: tx.(*gatesfake.Exec), stored: "[]"}, nil, g)
}

// fakeBucket is a docs.ObjectDeleter over an in-memory bucket. A delete of a missing key
// succeeds, like S3; fail makes every delete fail.
type fakeBucket struct {
	objects map[string]bool
	deletes []string
	fail    bool
}

func (b *fakeBucket) DeleteObject(_ context.Context, key string) error {
	b.deletes = append(b.deletes, key)
	if b.fail {
		return fmt.Errorf("s3 delete %s: status 500", key)
	}
	delete(b.objects, key)
	return nil
}

// G14 purges the stored copy behind each file_ref from the document bucket through the docs
// storage port, records the purge, and is idempotent: a re-run deletes nothing new and a copy
// already gone is no error. A failed delete or a missing bucket applies nothing.
func TestIDDestructionPurgesStoredFile(t *testing.T) {
	const (
		doc1 = "00000000-0000-0000-0000-0000000000d1"
		doc2 = "00000000-0000-0000-0000-0000000000d2"
		key1 = "party/id/0001.pdf"
		key2 = "party/id/0002.pdf"
	)
	g := registry.Approved{GateID: gatesfake.GateID, Kind: GateIDDestruction,
		Payload:  json.RawMessage(`{"contact_id":"` + tID + `","record_ids":["` + tCheck + `"]}`),
		Preparer: gatesfake.Preparer, Approvers: []registry.Principal{gatesfake.Lawyer, gatesfake.Partner}}
	two := `[{"id":"` + doc1 + `","file_ref":"` + key1 + `"},{"id":"` + doc2 + `","file_ref":"` + key2 + `"}]`
	tests := []struct {
		name        string
		stored      string
		bucket      *fakeBucket
		nilBucket   bool
		wantErr     error
		wantDeletes []string
		wantPurged  []string
	}{
		{"copies purged and recorded", two, &fakeBucket{objects: map[string]bool{key1: true, key2: true}}, false, nil, []string{key1, key2}, []string{doc1, doc2}},
		{"copy already gone is no error", two, &fakeBucket{objects: map[string]bool{key2: true}}, false, nil, []string{key1, key2}, []string{doc1, doc2}},
		{"re-run after redaction purges nothing", "[]", &fakeBucket{}, false, nil, nil, nil},
		{"failed delete applies nothing", two, &fakeBucket{fail: true}, false, gates.ErrNotApplied, []string{key1}, nil},
		{"no bucket fails closed when a copy is stored", two, nil, true, gates.ErrNotApplied, nil, nil},
		{"no bucket is fine with no stored copy", "[]", nil, true, nil, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x := &gatesfake.Exec{StepRows: 1, ProofRows: 1}
			var files interface {
				DeleteObject(context.Context, string) error
			}
			if !tt.nilBucket {
				files = tt.bucket
			}
			err := applyIDDestruction(context.Background(), &idTx{fake: x, stored: tt.stored}, files, g)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil && (strings.Contains(err.Error(), key1) || strings.Contains(err.Error(), key2)) {
				t.Fatalf("error carries a file key: %v", err)
			}
			if tt.bucket != nil {
				if !slices.Equal(tt.bucket.deletes, tt.wantDeletes) {
					t.Fatalf("deletes = %v, want %v", tt.bucket.deletes, tt.wantDeletes)
				}
				if !tt.bucket.fail && len(tt.bucket.objects) != 0 {
					t.Fatalf("objects left = %v", tt.bucket.objects)
				}
			}
			var purged []string
			redacted := false
			for i, q := range x.SQL {
				if strings.Contains(q, "file_purged_at = now()") {
					purged = x.Args[i][0].([]string)
					if x.Args[i][1] != gatesfake.GateID {
						t.Fatalf("purge recorded under %v, want the gate", x.Args[i][1])
					}
				}
				if strings.Contains(q, "file_ref = NULL") {
					redacted = true
				}
			}
			if !slices.Equal(purged, tt.wantPurged) {
				t.Fatalf("purged = %v, want %v", purged, tt.wantPurged)
			}
			if redacted != (tt.wantErr == nil) {
				t.Fatalf("redacted = %v, want %v", redacted, tt.wantErr == nil)
			}
		})
	}
}
