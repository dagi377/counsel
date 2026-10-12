package books_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/eventroute/eventroutetest"
)

const opBank = "0192a000-0000-7000-8000-0000000000a1"

type memRecons struct {
	book   int64
	exists bool
	drafts []books.OpReconDraft
}

func (m *memRecons) BookBalance(_ context.Context, _ pgx.Tx, accountID, _ string) (int64, error) {
	if accountID != opBank {
		return 0, books.ErrNotOperatingBank
	}
	return m.book, nil
}

func (m *memRecons) Prepare(_ context.Context, _ pgx.Tx, d books.OpReconDraft) (string, error) {
	if m.exists {
		return "", books.ErrReconExists
	}
	m.drafts = append(m.drafts, d)
	return "0192a000-0000-7000-8000-0000000000f2", nil
}

func TestOpReconHTTPCreate(t *testing.T) {
	body := func(account string, bank int64, items ...int64) string {
		var its []map[string]any
		for _, c := range items {
			its = append(its, map[string]any{"description": "item", "amount_cents": c})
		}
		b, _ := json.Marshal(map[string]any{"account_id": account, "period_end": "2026-09-30",
			"bank_balance_cents": bank, "outstanding_items": its})
		return string(b)
	}
	tests := []struct {
		name   string
		user   string
		body   string
		book   int64
		exists bool
		status int
	}{
		// rule: ARC-I-027
		{"reconciled month opens the sign-off gate", "bookkeeper", body(opBank, 100000, 2500, -7500), 95000, false, http.StatusAccepted},
		// rule: BIL-I-009
		{"unreconciled month refused", "bookkeeper", body(opBank, 100000, 2500), 95000, false, http.StatusUnprocessableEntity},
		// rule: BIL-L-034
		{"not the operating bank refused", "bookkeeper", body("0192a000-0000-7000-8000-0000000000a2", 0), 0, false, http.StatusUnprocessableEntity},
		{"period already prepared", "bookkeeper", body(opBank, 95000), 95000, true, http.StatusConflict},
		// rule: BIL-C-064
		{"zero item refused", "bookkeeper", body(opBank, 95000, 0), 95000, false, http.StatusBadRequest},
		{"negative bank refused", "bookkeeper", body(opBank, -1), 0, false, http.StatusBadRequest},
		// rule: ARC-I-051
		{"no capability", eventroutetest.NoFirmCap, body(opBank, 95000), 95000, false, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := eventroutetest.User(tt.user)
			gates, recons := &eventroutetest.Gates{}, &memRecons{book: tt.book, exists: tt.exists}
			h := &books.OpReconHTTP{Route: eventroutetest.Route(p), DB: eventroutetest.Tx{}, Gates: gates, Recons: recons}
			rec := eventroutetest.Post(h.Routes, "/operating-reconciliations", tt.body)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.status, rec.Body)
			}
			if tt.status != http.StatusAccepted {
				if len(gates.Opened) != 0 {
					t.Fatal("a refused request opened a gate")
				}
				return
			}
			o := gates.Opened[0]
			var pl map[string]any
			_ = json.Unmarshal(o.Payload, &pl)
			var raw map[string]json.RawMessage
			_ = json.Unmarshal(o.Payload, &raw)
			d := recons.drafts[0]
			if d.PreparedBy != tt.user || d.AdjustedBankCents != tt.book || d.BookBalanceCents != tt.book ||
				o.Kind != books.KindOperatingReconciliation || !reflect.DeepEqual(o.Preparer, p) ||
				string(raw["book_balance_cents"]) != fmt.Sprint(tt.book) || pl["account_id"] != opBank {
				t.Fatalf("draft %+v opened %+v payload %v", d, o, pl)
			}
		})
	}
}
