package payment

import (
	"errors"
	"testing"
)

// rule: BIL-C-068
// rule: ARC-I-048
func TestBIL_C_068_ARC_I_048_EventsApplyByPrecedence(t *testing.T) {
	tests := []struct {
		name         string
		events       []ProviderState // fetched state per event, in arrival order
		wantState    ProviderState
		wantCreates  int
		wantReverses int
		wantErr      error
	}{
		{name: "dispute before capture", events: []ProviderState{ProviderReversed, ProviderPosted}, wantState: ProviderReversed, wantCreates: 1, wantReverses: 1},
		{name: "capture then dispute", events: []ProviderState{ProviderPosted, ProviderReversed}, wantState: ProviderReversed, wantCreates: 1, wantReverses: 1},
		{name: "dispute replayed", events: []ProviderState{ProviderPosted, ProviderReversed, ProviderReversed}, wantState: ProviderReversed, wantCreates: 1, wantReverses: 1},
		{name: "capture replayed after refund", events: []ProviderState{ProviderPosted, ProviderRefunded, ProviderPosted}, wantState: ProviderRefunded, wantCreates: 1},
		{name: "partial then full refund", events: []ProviderState{ProviderReceived, ProviderPartiallyRefunded, ProviderRefunded}, wantState: ProviderRefunded, wantCreates: 1},
		{name: "full refund then late partial refund", events: []ProviderState{ProviderPosted, ProviderRefunded, ProviderPartiallyRefunded}, wantState: ProviderRefunded, wantCreates: 1},
		{name: "chargeback after refund", events: []ProviderState{ProviderPosted, ProviderRefunded, ProviderReversed}, wantState: ProviderReversed, wantCreates: 1, wantReverses: 1},
		{name: "pending then posted", events: []ProviderState{ProviderPending, ProviderPosted}, wantState: ProviderPosted, wantCreates: 1},
		{name: "unknown state", events: []ProviderState{"settled"}, wantErr: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cur ProviderState
			creates, reverses := 0, 0
			for _, ev := range tt.events {
				st, err := ApplyEvent(cur, ev)
				if err != nil {
					if !errors.Is(err, tt.wantErr) {
						t.Fatalf("err = %v, want %v", err, tt.wantErr)
					}
					return
				}
				if st.Create {
					creates++
				}
				if st.Reverse {
					reverses++
				}
				if st.NoOp() && st.State != cur {
					t.Errorf("no-op step changed state %q -> %q", cur, st.State)
				}
				cur = st.State
			}
			if tt.wantErr != nil {
				t.Fatalf("err = nil, want %v", tt.wantErr)
			}
			if cur != tt.wantState || creates != tt.wantCreates || reverses != tt.wantReverses {
				t.Errorf("state %q creates %d reverses %d, want %q %d %d", cur, creates, reverses, tt.wantState, tt.wantCreates, tt.wantReverses)
			}
		})
	}
}

// rule: BIL-C-068
func TestBIL_C_068_LowerPrecedenceIsNoOp(t *testing.T) {
	tests := []struct {
		current, fetched ProviderState
		noop             bool
	}{
		{ProviderRefunded, ProviderPosted, true},
		{ProviderPosted, ProviderPosted, true},
		{ProviderReversed, ProviderRefunded, true},
		{ProviderReversed, ProviderReversed, true},
		{ProviderPosted, ProviderPartiallyRefunded, false},
		{"captured", ProviderPosted, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.current)+"->"+string(tt.fetched), func(t *testing.T) {
			st, err := ApplyEvent(tt.current, tt.fetched)
			if tt.current == "captured" {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("err = %v, want ErrInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if st.NoOp() != tt.noop {
				t.Errorf("noop = %v, want %v", st.NoOp(), tt.noop)
			}
		})
	}
}
