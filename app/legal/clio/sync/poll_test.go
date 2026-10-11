package sync

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memSync struct{ rows map[string]SourceRecord }

func (m *memSync) Stored(_ context.Context, res, id string) (SourceRecord, bool, error) {
	r, ok := m.rows[res+"/"+id]
	return r, ok, nil
}

func (m *memSync) Apply(_ context.Context, r SourceRecord) error {
	m.rows[r.Resource+"/"+r.ID] = r
	return nil
}

type memChanges struct {
	recs  []SourceRecord
	since []time.Time
	err   error
}

func (m *memChanges) Changed(_ context.Context, res string, since time.Time) ([]SourceRecord, error) {
	m.since = append(m.since, since)
	var out []SourceRecord
	for _, r := range m.recs {
		if r.Resource == res && !r.UpdatedAt.Before(since) {
			out = append(out, r)
		}
	}
	return out, m.err
}

func toronto(t *testing.T, s string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	v, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// rule: MIG-043
func TestMIG_043_PollSinceAndInterval(t *testing.T) {
	tests := []struct {
		name, watermark, delivered, want string
	}{
		{"steady state", "2026-11-02 14:00", "", "2026-11-02 13:50"},
		{"webhook lapse", "2026-11-02 14:00", "2026-11-02 10:00", "2026-11-02 09:50"},
		{"delivered after watermark", "2026-11-02 10:00", "2026-11-02 11:00", "2026-11-02 09:50"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var d time.Time
			if tc.delivered != "" {
				d = toronto(t, tc.delivered)
			}
			got := PollSince(toronto(t, tc.watermark), d)
			if !got.Equal(toronto(t, tc.want)) {
				t.Errorf("since = %v, want %s", got, tc.want)
			}
		})
	}
	if PollInterval(true) != time.Hour || PollInterval(false) != 15*time.Minute {
		t.Errorf("intervals = %v/%v", PollInterval(true), PollInterval(false))
	}
}

// rule: MIG-043
func TestMIG_043_GapFillNoDuplicates(t *testing.T) {
	ctx := context.Background()
	src := &memChanges{}
	for i, at := range []string{"09:55", "10:30", "11:00", "11:45", "12:10", "13:00", "13:59"} {
		src.recs = append(src.recs, SourceRecord{Resource: "matters", ID: string(rune('a' + i)), ETag: "v1", UpdatedAt: toronto(t, "2026-11-02 "+at)})
	}
	src.recs = append(src.recs, SourceRecord{Resource: "matters", ID: "old", ETag: "v1", UpdatedAt: toronto(t, "2026-11-02 09:00")})
	store := &memSync{rows: map[string]SourceRecord{}}
	p := &Poller{Source: src, Store: store}
	since := PollSince(toronto(t, "2026-11-02 14:00"), toronto(t, "2026-11-02 10:00"))
	tests := []struct {
		name            string
		applied, ignore int
	}{
		{"gap fill", 7, 0},
		{"overlapping repoll", 0, 7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := p.Poll(ctx, "matters", since)
			if err != nil {
				t.Fatal(err)
			}
			if res.Applied != tc.applied || res.Ignored != tc.ignore {
				t.Errorf("result = %+v", res)
			}
			if len(store.rows) != 7 {
				t.Errorf("rows = %d, want 7 (no duplicates)", len(store.rows))
			}
		})
	}
	if !src.since[0].Equal(toronto(t, "2026-11-02 09:50")) {
		t.Errorf("polled since %v, want 09:50", src.since[0])
	}
}

// rule: MIG-044
func TestMIG_044_OnlyNewerApplied(t *testing.T) {
	ctx := context.Background()
	stored := SourceRecord{Resource: "matters", ID: "7", ETag: "v5", UpdatedAt: toronto(t, "2026-11-02 10:00")}
	tests := []struct {
		name    string
		got     SourceRecord
		applied bool
	}{
		{"older ignored", SourceRecord{Resource: "matters", ID: "7", ETag: "v4", UpdatedAt: toronto(t, "2026-11-02 09:58")}, false},
		{"equal ignored", SourceRecord{Resource: "matters", ID: "7", ETag: "v5b", UpdatedAt: toronto(t, "2026-11-02 10:00")}, false},
		{"newer applied", SourceRecord{Resource: "matters", ID: "7", ETag: "v6", UpdatedAt: toronto(t, "2026-11-02 10:05")}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &memSync{rows: map[string]SourceRecord{"matters/7": stored}}
			p := &Poller{Source: &memChanges{recs: []SourceRecord{tc.got}}, Store: store}
			res, err := p.Poll(ctx, "matters", toronto(t, "2026-11-02 09:00"))
			if err != nil {
				t.Fatal(err)
			}
			if (res.Applied == 1) != tc.applied {
				t.Errorf("result = %+v, applied want %v", res, tc.applied)
			}
			want := stored.ETag
			if tc.applied {
				want = tc.got.ETag
			}
			if store.rows["matters/7"].ETag != want {
				t.Errorf("stored etag = %s, want %s", store.rows["matters/7"].ETag, want)
			}
		})
	}
}

func TestPollErrors(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("boom")
	tests := []struct {
		name string
		src  *memChanges
		want error
	}{
		{"source error", &memChanges{err: boom}, boom},
		{"bad record", &memChanges{recs: []SourceRecord{{Resource: "matters", UpdatedAt: time.Now()}}}, ErrBadRecord},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &Poller{Source: tc.src, Store: &memSync{rows: map[string]SourceRecord{}}}
			if _, err := p.Poll(ctx, "matters", time.Time{}); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
