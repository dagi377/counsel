package webhook_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dagi377/counsel/kernel/webhook"
)

const itemSchema = `{
  "type": "object",
  "required": ["item_id"],
  "properties": {"item_id": {"type": "string"}},
  "additionalProperties": false
}`

func TestTypesRegister(t *testing.T) {
	tests := []struct {
		name    string
		types   []webhook.EventType
		wantErr bool
		dup     bool
	}{
		{"ok", []webhook.EventType{{Name: "work.item_created", Schema: []byte(itemSchema)}}, false, false},
		{"duplicate", []webhook.EventType{
			{Name: "work.item_created", Schema: []byte(itemSchema)},
			{Name: "work.item_created", Schema: []byte(itemSchema)},
		}, true, true},
		{"no module prefix", []webhook.EventType{{Name: "item_created", Schema: []byte(itemSchema)}}, true, false},
		{"empty name", []webhook.EventType{{Name: "", Schema: []byte(itemSchema)}}, true, false},
		{"empty schema", []webhook.EventType{{Name: "work.item_created"}}, true, false},
		{"bad schema json", []webhook.EventType{{Name: "work.item_created", Schema: []byte(`{`)}}, true, false},
		{"bad schema keyword", []webhook.EventType{{Name: "work.item_created", Schema: []byte(`{"type": 7}`)}}, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := webhook.NewTypes()
			var err error
			for _, et := range tc.types {
				if err = r.Register(et); err != nil {
					break
				}
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("Register err = %v, wantErr %v", err, tc.wantErr)
			}
			if errors.Is(err, webhook.ErrDuplicateType) != tc.dup {
				t.Fatalf("Register err = %v, want duplicate %v", err, tc.dup)
			}
		})
	}
}

func TestTypesCheck(t *testing.T) {
	r := webhook.NewTypes()
	if err := r.Register(webhook.EventType{Name: "work.item_created", Schema: []byte(itemSchema)}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	tests := []struct {
		name    string
		typ     string
		payload string
		want    error
	}{
		{"registered and valid", "work.item_created", `{"item_id":"i1"}`, nil},
		{"unregistered type cannot be emitted", "work.item_deleted", `{"item_id":"i1"}`, webhook.ErrUnknownType},
		{"empty type", "", `{}`, webhook.ErrUnknownType},
		{"missing field", "work.item_created", `{}`, webhook.ErrInvalidPayload},
		{"extra field", "work.item_created", `{"item_id":"i1","secret_note":"x"}`, webhook.ErrInvalidPayload},
		{"wrong type", "work.item_created", `{"item_id":7}`, webhook.ErrInvalidPayload},
		{"not json", "work.item_created", `nope`, webhook.ErrInvalidPayload},
		{"empty payload", "work.item_created", ``, webhook.ErrInvalidPayload},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := r.Check(webhook.Message{ID: "m1", Type: tc.typ, Payload: json.RawMessage(tc.payload)})
			if !errors.Is(err, tc.want) {
				t.Fatalf("Check err = %v, want %v", err, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), "secret_note") {
				t.Fatalf("error leaks payload: %v", err)
			}
		})
	}
}

func TestTypesNames(t *testing.T) {
	r := webhook.NewTypes()
	for _, n := range []string{"work.b", "audit.a", "work.a"} {
		if err := r.Register(webhook.EventType{Name: n, Schema: []byte(`{}`)}); err != nil {
			t.Fatalf("Register %s: %v", n, err)
		}
	}
	if got, want := r.Names(), []string{"audit.a", "work.a", "work.b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Names = %v, want %v", got, want)
	}
}
