package sync

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dagi377/counsel/test/fakes/practiceapi"
	"github.com/dagi377/counsel/test/fakes/practiceapi/practiceapifake"
)

// handlerTransport sends requests straight to a handler; no socket is opened.
type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, r)
	return rec.Result(), nil
}

func TestMIG_040_AlongsideNeverWritesToClio(t *testing.T) {
	// rule: MIG-040
	tests := []struct {
		method    string
		path      string
		wantErr   error
		wantNonGT int // non-GET calls the fake should record
	}{
		{http.MethodGet, "/api/v4/matters.json", nil, 0},
		{http.MethodHead, "/api/v4/matters.json", ErrAlongsideWrite, 0},
		{http.MethodPost, "/api/v4/matters.json", ErrAlongsideWrite, 0},
		{http.MethodPatch, "/api/v4/matters/7.json", ErrAlongsideWrite, 0},
		{http.MethodPut, "/api/v4/contacts/7.json", ErrAlongsideWrite, 0},
		{http.MethodDelete, "/api/v4/contacts/7.json", ErrAlongsideWrite, 0},
		{http.MethodPost, "/api/v4/webhooksx.json", ErrAlongsideWrite, 0},
		{http.MethodPost, "/api/v4/webhooks.json", nil, 1},
		{http.MethodPatch, "/api/v4/webhooks/3.json", nil, 1},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			fake := practiceapifake.New(10)
			fake.Load("matters", []json.RawMessage{json.RawMessage(`{"id":1}`)})
			c := Client(ModeAlongside, handlerTransport{fake.Handler()})
			req, err := http.NewRequest(tt.method, "http://clio.test"+tt.path, strings.NewReader("{}"))
			if err != nil {
				t.Fatal(err)
			}
			resp, err := c.Do(req)
			if resp != nil {
				resp.Body.Close()
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got := nonGET(fake.Requests()); got != tt.wantNonGT {
				t.Errorf("fake recorded %d non-GET calls, want %d", got, tt.wantNonGT)
			}
		})
	}
}

func TestMIG_040_MigratedModeDoesNotWrap(t *testing.T) {
	// rule: MIG-040
	fake := practiceapifake.New(10)
	c := Client(ModeMigrated, handlerTransport{fake.Handler()})
	resp, err := c.Post("http://clio.test/api/v4/matters.json", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if got := nonGET(fake.Requests()); got != 1 {
		t.Errorf("non-GET = %d, want 1", got)
	}
}

func TestMIG_040_ImportedEntitiesReadOnly(t *testing.T) {
	// rule: MIG-040
	tests := []struct {
		mode     Mode
		imported bool
		want     error
	}{
		{ModeAlongside, true, ErrSourceOfRecord},
		{ModeAlongside, false, nil},
		{ModeMigrated, true, nil},
	}
	for _, tt := range tests {
		if got := CheckEditable(tt.mode, tt.imported); !errors.Is(got, tt.want) {
			t.Errorf("CheckEditable(%s, %v) = %v, want %v", tt.mode, tt.imported, got, tt.want)
		}
	}
	if ErrSourceOfRecord.Error() != "source of record is Clio" {
		t.Errorf("message = %q", ErrSourceOfRecord)
	}
}

func nonGET(reqs []practiceapi.Request) int {
	n := 0
	for _, r := range reqs {
		if r.Method != http.MethodGet {
			n++
		}
	}
	return n
}
