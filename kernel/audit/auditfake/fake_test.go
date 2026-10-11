package auditfake

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/audit"
)

func ev(action string) audit.Event {
	return audit.Event{ActorKind: "user", ActorID: "u1", Action: action, Subject: audit.Subject{Kind: "ticket", ID: "t1"},
		Detail: map[string]string{"status": "open", "body": "secret text"}}
}

func TestAppend(t *testing.T) {
	tests := []struct {
		name string
		e    audit.Event
		err  error
	}{
		{name: "valid", e: ev("gate.decide")},
		{name: "no actor", e: audit.Event{ActorKind: "user", Action: "x.y"}, err: audit.ErrInvalidEvent},
		{name: "no action", e: audit.Event{ActorKind: "user", ActorID: "u1"}, err: audit.ErrInvalidEvent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New("status")
			err := f.Append(context.Background(), nil, tt.e)
			if !errors.Is(err, tt.err) {
				t.Fatalf("Append err = %v, want %v", err, tt.err)
			}
			if err != nil {
				return
			}
			got := f.Events()[0]
			if got.Seq != 1 || len(got.Hash) == 0 || got.PrevHash != nil {
				t.Fatalf("first event = seq %d hash %x prev %x", got.Seq, got.Hash, got.PrevHash)
			}
			if got.Detail["status"] != "open" || got.Detail["body"] != Masked {
				t.Fatalf("detail not masked: %v", got.Detail)
			}
		})
	}
}

func TestVerify(t *testing.T) {
	tests := []struct {
		name     string
		tamper   func(f *Fake)
		from, to audit.Seq
		err      error
	}{
		{name: "intact", from: 1, to: 3},
		{name: "flipped byte in row 2", tamper: func(f *Fake) {
			f.Tamper(2, func(e *audit.Event) { e.Action = "gate.decidE" })
		}, from: 1, to: 3, err: audit.ErrChainBroken},
		{name: "relinked row 3", tamper: func(f *Fake) {
			f.Tamper(3, func(e *audit.Event) { e.PrevHash = []byte{1} })
		}, from: 1, to: 3, err: audit.ErrChainBroken},
		{name: "range past end", from: 1, to: 4, err: audit.ErrBadRange},
		{name: "inverted range", from: 3, to: 1, err: audit.ErrBadRange},
		{name: "zero from", from: 0, to: 1, err: audit.ErrBadRange},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			f := New()
			for _, a := range []string{"a.one", "gate.decide", "a.three"} {
				if err := f.Append(ctx, nil, ev(a)); err != nil {
					t.Fatal(err)
				}
			}
			if tt.tamper != nil {
				tt.tamper(f)
			}
			if err := f.Verify(ctx, tt.from, tt.to); !errors.Is(err, tt.err) {
				t.Fatalf("Verify err = %v, want %v", err, tt.err)
			}
		})
	}
}

func TestMask(t *testing.T) {
	tests := []struct {
		name string
		in   map[string]string
		want map[string]string
	}{
		{name: "nil", in: nil, want: nil},
		{name: "allowed kept", in: map[string]string{"status": "x"}, want: map[string]string{"status": "x"}},
		{name: "other masked", in: map[string]string{"name": "Jane"}, want: map[string]string{"name": Masked}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := New("status").Mask(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("Mask = %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Fatalf("Mask[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}
