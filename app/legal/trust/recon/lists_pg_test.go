package recon

import (
	"context"
	"errors"
	"testing"
)

func TestNoParserRefuses(t *testing.T) {
	tests := []struct {
		name string
		file []byte
	}{
		{"empty file", nil},
		{"csv file", []byte("date,amount\n2026-10-01,100\n")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NoParser{}.Parse(context.Background(), "acct", tt.file)
			if !errors.Is(err, ErrNoParser) {
				t.Fatalf("err = %v, want ErrNoParser", err)
			}
		})
	}
}

func TestStatusVersionMovesForward(t *testing.T) {
	steps := []string{"draft", "ready", "signed", "superseded"}
	for i := 1; i < len(steps); i++ {
		if statusVersion[steps[i]] <= statusVersion[steps[i-1]] {
			t.Errorf("%s -> %s does not raise the version", steps[i-1], steps[i])
		}
	}
}

func TestDecodeItems(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int64
		wantErr bool
	}{
		{"empty", "", 0, false},
		{"none", "[]", 0, false},
		{"two", `[{"kind":"outstanding_cheque","amount_cents":-500},{"kind":"in_transit","amount_cents":200}]`, -300, false},
		{"bad", "{", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := decodeItems([]byte(tt.raw))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			var sum int64
			for _, it := range items {
				sum += it.AmountCents
			}
			if sum != tt.want {
				t.Fatalf("sum = %d, want %d", sum, tt.want)
			}
		})
	}
}
