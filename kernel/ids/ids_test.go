package ids

import (
	"bytes"
	"errors"
	"regexp"
	"testing"
	"time"
)

var canonical = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewMonotonic(t *testing.T) {
	frozen := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	back := frozen
	tests := []struct {
		name string
		src  *Source
	}{
		{name: "process default", src: std},
		{name: "real clock", src: NewSource(nil, nil)},
		{name: "frozen clock", src: NewSource(func() time.Time { return frozen }, nil)},
		{name: "clock steps back", src: NewSource(func() time.Time { back = back.Add(-time.Millisecond); return back }, nil)},
		{name: "counter near exhaustion", src: NewSource(func() time.Time { return frozen }, bytes.NewReader(bytes.Repeat([]byte{0xff}, 1<<16)))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prev := tt.src.New()
			for i := 0; i < 100_000; i++ {
				id := tt.src.New()
				if id <= prev {
					t.Fatalf("id %d: %s not greater than %s", i, id, prev)
				}
				prev = id
			}
			if !canonical.MatchString(string(prev)) {
				t.Fatalf("%s is not a canonical UUIDv7", prev)
			}
		})
	}
	a, b := New(), New()
	if b <= a {
		t.Fatalf("package New not monotonic: %s then %s", a, b)
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		in  string
		err error
	}{
		{in: "01890a5d-ac96-774b-bcce-b302099a8057"},
		{in: "00000000-0000-7000-8000-000000000001"},
		{in: "01890A5D-AC96-774B-BCCE-B302099A8057", err: ErrInvalid},
		{in: "01890a5dac96774bbcceb302099a8057", err: ErrInvalid},
		{in: "01890a5d-ac96-774b-bcce-b302099a805", err: ErrInvalid},
		{in: "01890a5d-ac96-774b-bcce-b302099a805g", err: ErrInvalid},
		{in: "", err: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := Parse(tt.in)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if err == nil && string(got) != tt.in {
				t.Fatalf("got %s", got)
			}
		})
	}
}

// rule: ARC-I-004
func TestARC_I_004_IDTimeIsUTCInstant(t *testing.T) {
	toronto, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Skip("no tzdata")
	}
	tests := []struct {
		name string
		at   time.Time
		want time.Time
	}{
		{
			name: "late evening Toronto is next day UTC",
			at:   time.Date(2026, 3, 31, 23, 30, 0, 0, toronto),
			want: time.Date(2026, 4, 1, 3, 30, 0, 0, time.UTC),
		},
		{
			name: "milliseconds kept, sub-ms dropped",
			at:   time.Date(2026, 1, 2, 3, 4, 5, 6_789_000, time.UTC),
			want: time.Date(2026, 1, 2, 3, 4, 5, 6_000_000, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := NewSource(func() time.Time { return tt.at }, nil).New()
			got, err := id.Time()
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(tt.want) || got.Location() != time.UTC {
				t.Fatalf("Time() = %v, want %v UTC", got, tt.want)
			}
		})
	}
	if _, err := ID("00000000-0000-4000-8000-000000000000").Time(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("v4 id Time err = %v", err)
	}
}

// rule: BIL-C-120
// rule: BIL-I-008
// IDs are opaque: two sources in the same millisecond interleave (as SQL core.uuidv7() does),
// so they order only by millisecond and are never gapless. Document numbers do not use them.
func TestBIL_C_120_BIL_I_008_IDsAreNotDocumentNumbers(t *testing.T) {
	ms := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		seedA  byte
		seedB  byte
		aFirst bool
	}{
		{name: "lower seed sorts first", seedA: 0x00, seedB: 0x7f, aFirst: true},
		{name: "issue order does not decide", seedA: 0x7f, seedB: 0x00, aFirst: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewSource(func() time.Time { return ms }, bytes.NewReader(bytes.Repeat([]byte{tt.seedA}, 10))).New()
			b := NewSource(func() time.Time { return ms }, bytes.NewReader(bytes.Repeat([]byte{tt.seedB}, 10))).New()
			if (a < b) != tt.aFirst {
				t.Fatalf("a=%s b=%s: within one millisecond order follows random bits, not issue order", a, b)
			}
			ta, _ := a.Time()
			tb, _ := b.Time()
			if !ta.Equal(tb) {
				t.Fatalf("same millisecond expected: %v vs %v", ta, tb)
			}
		})
	}
	later := NewSource(func() time.Time { return ms.Add(time.Millisecond) }, bytes.NewReader(make([]byte, 10))).New()
	earlier := NewSource(func() time.Time { return ms }, bytes.NewReader(bytes.Repeat([]byte{0xff}, 10))).New()
	if !(earlier < later) {
		t.Fatalf("ids must sort by millisecond: %s !< %s", earlier, later)
	}
}
