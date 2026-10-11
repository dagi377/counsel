package migrations

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

const billGuardFile = "5622_bill_sign_after_approval.sql"

var newColumn = regexp.MustCompile(`NEW\.(\w+)`)

// billGuard returns the body of billing.bill_guard as name defines it.
func billGuard(t *testing.T, name string) string {
	t.Helper()
	b, err := Read(name)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i := strings.Index(s, "FUNCTION billing.bill_guard()")
	if i < 0 {
		t.Fatalf("%s does not define billing.bill_guard", name)
	}
	j := strings.Index(s[i:], "END $$;")
	if j < 0 {
		t.Fatalf("%s: bill_guard has no end", name)
	}
	return s[i : i+j]
}

// tupleAfter returns the NEW.<column> names of the first row comparison after marker.
func tupleAfter(t *testing.T, body, marker string) []string {
	t.Helper()
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("marker %q not found", marker)
	}
	rest := body[i:]
	j := strings.Index(rest, "IS DISTINCT FROM")
	if j < 0 {
		t.Fatalf("no comparison after %q", marker)
	}
	var cols []string
	for _, m := range newColumn.FindAllStringSubmatch(rest[:j], -1) {
		cols = append(cols, m[1])
	}
	slices.Sort(cols)
	return slices.Compact(cols)
}

var signatureColumns = []string{"signature_hash", "signed_at", "signed_by"}

// rule: BIL-L-001
// The replaced bill_guard frees only the three signature columns: every other column 0001
// froze after approval is still in the frozen comparison, and the signature comparison names
// exactly signed_by, signed_at and signature_hash.
func TestBillGuardFreesOnlyTheSignature(t *testing.T) {
	const frozenMarker = "IF OLD.status NOT IN ('draft','preflight_failed','pending_approval')"
	initFrozen := tupleAfter(t, billGuard(t, "0001_init.sql"), frozenMarker)
	body := billGuard(t, billGuardFile)
	var wantFrozen []string
	for _, c := range initFrozen {
		if !slices.Contains(signatureColumns, c) {
			wantFrozen = append(wantFrozen, c)
		}
	}
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{"0001 froze the signature with the rest", intersect(initFrozen, signatureColumns), signatureColumns},
		{"every other column stays frozen", tupleAfter(t, body, frozenMarker), wantFrozen},
		{"signature set once names only the signature columns", tupleAfter(t, body, "-- BIL-L-001"), signatureColumns},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !slices.Equal(tt.got, tt.want) {
				t.Fatalf("columns = %v, want %v", tt.got, tt.want)
			}
		})
	}
}

// The replacement keeps every other bill_guard rule word for word, and is the last file
// that defines the function.
func TestBillGuardKeepsOtherRules(t *testing.T) {
	body := billGuard(t, billGuardFile)
	rules := []string{
		"IF OLD.status <> 'draft' THEN RAISE EXCEPTION 'DM-010: only draft bills may be deleted'; END IF;",
		"IF NEW.delivered_at > now() THEN RAISE EXCEPTION 'DM-041: delivered_at % is in the future', NEW.delivered_at; END IF;",
		"RAISE EXCEPTION 'DM-041: delivered_at of bill % cannot be rewritten without an approved delivery_correction', OLD.id;",
		"RAISE EXCEPTION 'DM-041: bill % was delivered; issue a credit note instead of voiding', OLD.id;",
		"IF OLD.status = 'void' AND NEW.status <> 'void' THEN RAISE EXCEPTION 'DM-041: void is terminal'; END IF;",
		"AND ar.gate_code = 'billing.bill_approval' AND ar.payload_type = 'billing.bill' AND ar.payload_id = NEW.id) THEN",
		"RAISE EXCEPTION 'DM-067: bill % needs an approved billing.bill_approval gate', NEW.id;",
		"(OLD.status = 'approved' AND NEW.status = 'approved'",
		"AND OLD.delivered_at IS NULL AND NEW.delivered_at IS NULL",
		"AND OLD.signed_by IS NULL AND OLD.signed_at IS NULL AND OLD.signature_hash IS NULL",
	}
	initBody := billGuard(t, "0001_init.sql")
	for i, r := range rules {
		t.Run(r, func(t *testing.T) {
			if !strings.Contains(strings.Join(strings.Fields(body), " "), strings.Join(strings.Fields(r), " ")) {
				t.Fatalf("rule missing from %s", billGuardFile)
			}
			if i < 7 && !strings.Contains(initBody, r) {
				t.Fatalf("rule is not the 0001 text")
			}
		})
	}
	var last string
	for _, n := range Names() {
		b, err := Read(n)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "FUNCTION billing.bill_guard()") {
			last = n
		}
	}
	if last != billGuardFile {
		t.Fatalf("last bill_guard definition in %s, want %s", last, billGuardFile)
	}
}

func intersect(a, b []string) []string {
	var out []string
	for _, x := range a {
		if slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}
