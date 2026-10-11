package bill

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/outbox"
)

// scriptRow scans scripted values into the destinations by reflection.
type scriptRow struct {
	vals []any
	err  error
}

func (r scriptRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.vals) {
		return fmt.Errorf("scan: %d dest, %d values", len(dest), len(r.vals))
	}
	for i, v := range r.vals {
		d := reflect.ValueOf(dest[i]).Elem()
		if v == nil {
			d.Set(reflect.Zero(d.Type()))
			continue
		}
		d.Set(reflect.ValueOf(v).Convert(d.Type()))
	}
	return nil
}

// noRows is an empty result set.
type noRows struct{ pgx.Rows }

func (noRows) Close()                        {}
func (noRows) Err() error                    { return nil }
func (noRows) Next() bool                    { return false }
func (noRows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }

// billTx answers the PGService statements for one bill row held in memory.
type billTx struct {
	pgx.Tx
	id      string
	status  Status
	signed  string
	deliv   *time.Time
	creator string
	execs   []string
}

func (t *billTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	if args[0] != t.id {
		return scriptRow{err: pgx.ErrNoRows}
	}
	switch sql {
	case billLockSQL:
		return scriptRow{vals: []any{string(t.status)}}
	case billReadSQL:
		var signedAt *time.Time
		if t.signed != "" {
			at := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
			signedAt = &at
		}
		return scriptRow{vals: []any{t.id, "", "m1", "p1", string(t.status), t.creator,
			nil, nil, nil, nil,
			int64(100000), int64(0), int64(0), int64(0), int64(13000), int64(113000),
			nil, "ON", "", "", nil, t.signed, signedAt,
			"", t.deliv, "", 1, time.Time{}, []byte(`[]`), int64(113000)}}
	}
	return scriptRow{err: fmt.Errorf("unexpected query %q", sql)}
}

func (t *billTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return noRows{}, nil }

func (t *billTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.execs = append(t.execs, sql)
	switch sql {
	case billSubmitSQL:
		if t.status != StatusDraft {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		t.status = StatusPendingApproval
	case billDeliverSQL:
		at := args[1].(time.Time)
		t.status, t.deliv = StatusDelivered, &at
	default:
		return pgconn.CommandTag{}, fmt.Errorf("unexpected exec %q", sql)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type txRunner struct{ tx pgx.Tx }

func (r txRunner) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, r.tx)
}

var pgNow = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

func TestPGServiceSubmit(t *testing.T) {
	tests := []struct {
		name   string
		id     ID
		status Status
		want   error
		after  Status
	}{
		{"draft moves to pending approval", "b1", StatusDraft, nil, StatusPendingApproval},
		{"pending is not submittable", "b1", StatusPendingApproval, ErrNotSubmittable, StatusPendingApproval},
		{"approved is not submittable", "b1", StatusApproved, ErrNotSubmittable, StatusApproved},
		{"unknown bill", "zz", StatusDraft, ErrNotFound, StatusDraft},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &billTx{id: "b1", status: tt.status, creator: "prep"}
			s := NewPGService(txRunner{tx}, func() time.Time { return pgNow })
			b, err := s.Submit(context.Background(), tx, tt.id)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Submit err = %v, want %v", err, tt.want)
			}
			if tx.status != tt.after {
				t.Fatalf("status = %s, want %s", tx.status, tt.after)
			}
			if err == nil && (b.Status != StatusPendingApproval || b.PreparedBy != "prep" || b.TotalCents != 113000) {
				t.Fatalf("bill = %+v", b)
			}
		})
	}
}

func TestBIL_L_003_PGServiceDeliver(t *testing.T) {
	past, future := pgNow.Add(-time.Hour), pgNow.Add(time.Hour)
	tests := []struct {
		name   string
		status Status
		signed string
		ev     DeliveryEvidence
		want   error
		exec   bool
	}{
		{"signed approved bill is delivered", StatusApproved, "lawyer", DeliveryEvidence{Channel: ChannelEmail, AcceptedAt: past}, nil, true},
		{"bounce leaves it undelivered", StatusApproved, "lawyer", DeliveryEvidence{Channel: ChannelEmail, AcceptedAt: past, Bounced: true}, nil, false},
		{"signature required", StatusApproved, "", DeliveryEvidence{Channel: ChannelEmail, AcceptedAt: past}, ErrSignatureRequired, false},
		{"future evidence", StatusApproved, "lawyer", DeliveryEvidence{Channel: ChannelEmail, AcceptedAt: future}, ErrFutureEvidence, false},
		{"draft is not approved", StatusDraft, "", DeliveryEvidence{Channel: ChannelEmail, AcceptedAt: past}, ErrNotApproved, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &billTx{id: "b1", status: tt.status, signed: tt.signed, creator: "prep"}
			s := NewPGService(txRunner{tx}, func() time.Time { return pgNow })
			_, err := s.Deliver(context.Background(), tx, "b1", tt.ev)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Deliver err = %v, want %v", err, tt.want)
			}
			if got := len(tx.execs) == 1; got != tt.exec {
				t.Fatalf("delivered write = %v, want %v", got, tt.exec)
			}
		})
	}
	tx := &billTx{id: "b1", status: StatusApproved, signed: "lawyer", deliv: &past}
	if _, err := NewPGService(txRunner{tx}, func() time.Time { return pgNow }).Deliver(context.Background(), tx, "b1",
		DeliveryEvidence{Channel: ChannelEmail, AcceptedAt: past}); !errors.Is(err, ErrAlreadyDelivered) {
		t.Fatalf("second delivery err = %v, want ErrAlreadyDelivered", err)
	}
}

func TestBIL_C_084_PGServicePreparerNeverApproves(t *testing.T) {
	tx := &billTx{id: "b1", status: StatusPendingApproval, creator: "prep"}
	s := NewPGService(txRunner{tx}, func() time.Time { return pgNow })
	s.Next = func(context.Context, pgx.Tx, string, int) (string, error) {
		t.Fatal("a number was taken for a refused approval")
		return "", nil
	}
	tests := []struct {
		name string
		a    Approval
		want error
	}{
		{"preparer", Approval{BillID: "b1", ApprovalID: "g1", ApprovedBy: "prep", ApprovedAt: pgNow, Prefix: "INV"}, ErrPreparerApproves},
		{"no approver", Approval{BillID: "b1", ApprovalID: "g1", ApprovedAt: pgNow, Prefix: "INV"}, ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.Approve(context.Background(), tx, tt.a); !errors.Is(err, tt.want) {
				t.Fatalf("Approve err = %v, want %v", err, tt.want)
			}
		})
	}
}

// recordingService records Deliver calls; every other method is unused.
type recordingService struct {
	Service
	delivered []DeliveryEvidence
	err       error
}

func (r *recordingService) Deliver(_ context.Context, _ pgx.Tx, _ ID, ev DeliveryEvidence) (Bill, error) {
	r.delivered = append(r.delivered, ev)
	return Bill{}, r.err
}

func TestDeliverConsumer(t *testing.T) {
	at := pgNow.Add(-time.Hour)
	body := []byte(`{"bill_id":"b1","channel":"email","accepted_at":"` + at.Format(time.RFC3339) + `"}`)
	tests := []struct {
		name    string
		msg     outbox.Message
		svcErr  error
		wantErr bool
		calls   int
	}{
		{"delivered message calls Deliver", outbox.Message{ID: "m1", Type: MessageDelivered, Payload: body}, nil, false, 1},
		{"other type skipped", outbox.Message{ID: "m2", Type: MessageDeliveryRequested, Payload: body}, nil, false, 0},
		{"malformed body", outbox.Message{ID: "m3", Type: MessageDelivered, Payload: []byte(`{}`)}, nil, true, 0},
		{"already delivered is a no-op", outbox.Message{ID: "m4", Type: MessageDelivered, Payload: body}, ErrAlreadyDelivered, false, 1},
		{"service error fails the message", outbox.Message{ID: "m5", Type: MessageDelivered, Payload: body}, ErrSignatureRequired, true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &recordingService{err: tt.svcErr}
			c := DeliverConsumer(s)
			err := c.Handle(context.Background(), nil, tt.msg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Handle err = %v, wantErr %v", err, tt.wantErr)
			}
			if len(s.delivered) != tt.calls {
				t.Fatalf("Deliver calls = %d, want %d", len(s.delivered), tt.calls)
			}
			if tt.calls == 1 && (!s.delivered[0].AcceptedAt.Equal(at) || s.delivered[0].Channel != ChannelEmail) {
				t.Fatalf("evidence = %+v", s.delivered[0])
			}
		})
	}
}
