package ids

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"time"
)

var _ Generator = (*Source)(nil)

// counterMask keeps the 74 bits after the timestamp: 12 bits rand_a and 62 bits rand_b.
const (
	randABits = 12
	randBBits = 62
	randBMask = uint64(1)<<randBBits - 1
)

// Source is a UUIDv7 Generator (RFC 9562, method 3: monotonic random). Within one Source
// the 74 bits after the millisecond timestamp act as a counter seeded from random, so IDs
// strictly increase even when many are issued in the same millisecond or the wall clock
// steps back. IDs from different processes, or from SQL core.uuidv7(), sort by millisecond
// only; no code may rely on order within a millisecond.
//
// IDs are opaque keys. They are not gapless and never serve as document numbers: those
// come from a per-sequence row taken FOR UPDATE in the approving transaction (kernel/numseq).
//
// rule: BIL-C-120
// rule: BIL-I-008
type Source struct {
	mu    sync.Mutex
	now   func() time.Time
	rnd   io.Reader
	ms    uint64 // timestamp of the last issued ID
	randA uint64 // 12 bits
	randB uint64 // 62 bits
}

// NewSource returns a Source. now defaults to time.Now and rnd to crypto/rand.
func NewSource(now func() time.Time, rnd io.Reader) *Source {
	if now == nil {
		now = time.Now
	}
	if rnd == nil {
		rnd = rand.Reader
	}
	return &Source{now: now, rnd: rnd}
}

var std = NewSource(nil, nil)

// New returns a new ID from the process-wide Source. IDs from New strictly increase within
// one process.
func New() ID { return std.New() }

// New returns an ID greater than every ID this Source issued before.
//
// rule: ARC-I-004
// The timestamp is the instant in UTC milliseconds (instants are stored UTC); it carries no
// business date.
func (s *Source) New() ID {
	s.mu.Lock()
	defer s.mu.Unlock()
	ms := uint64(s.now().UTC().UnixMilli())
	if ms > s.ms {
		s.ms = ms
		s.reseed()
	} else {
		// Same millisecond or clock stepped back: keep the last timestamp, count up.
		s.randB++
		if s.randB > randBMask {
			s.randB = 0
			s.randA++
			if s.randA >= 1<<randABits {
				// Counter exhausted: borrow the next millisecond.
				s.ms++
				s.reseed()
			}
		}
	}
	return s.format()
}

// reseed draws fresh random bits, leaving the top bit of rand_a clear so the counter has
// at least 2^73 steps of headroom within the millisecond.
func (s *Source) reseed() {
	var b [10]byte
	if _, err := io.ReadFull(s.rnd, b[:]); err != nil {
		// Without randomness, a zero seed still yields strictly increasing IDs here.
		b = [10]byte{}
	}
	s.randA = uint64(binary.BigEndian.Uint16(b[0:2])) & (1<<(randABits-1) - 1)
	s.randB = binary.BigEndian.Uint64(b[2:10]) & (randBMask >> 1)
}

func (s *Source) format() ID {
	var u [16]byte
	binary.BigEndian.PutUint64(u[0:8], s.ms<<16|0x7000|s.randA)
	binary.BigEndian.PutUint64(u[8:16], 0x8000000000000000|s.randB)
	return ID(encode(u))
}

func encode(u [16]byte) string {
	var buf [36]byte
	hex.Encode(buf[0:8], u[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], u[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], u[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], u[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], u[10:16])
	return string(buf[:])
}

// Parse checks that s is a canonical lower-case UUID (8-4-4-4-12) and returns it as an ID.
func Parse(s string) (ID, error) {
	if _, err := bytesOf(s); err != nil {
		return "", err
	}
	return ID(s), nil
}

func bytesOf(s string) ([16]byte, error) {
	var u [16]byte
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return u, fmt.Errorf("parse %q: %w", s, ErrInvalid)
	}
	j := 0
	for i := 0; i < 36; i += 2 {
		if s[i] == '-' {
			i--
			continue
		}
		hi, ok1 := lowerHex(s[i])
		lo, ok2 := lowerHex(s[i+1])
		if !ok1 || !ok2 {
			return u, fmt.Errorf("parse %q: %w", s, ErrInvalid)
		}
		u[j] = hi<<4 | lo
		j++
	}
	return u, nil
}

func lowerHex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	}
	return 0, false
}

// Time returns the UTC instant, to the millisecond, encoded in a UUIDv7 id.
//
// rule: ARC-I-004
func (id ID) Time() (time.Time, error) {
	u, err := bytesOf(string(id))
	if err != nil {
		return time.Time{}, err
	}
	if u[6]>>4 != 7 {
		return time.Time{}, fmt.Errorf("time of %q: not version 7: %w", string(id), ErrInvalid)
	}
	ms := int64(binary.BigEndian.Uint64(u[0:8]) >> 16)
	return time.UnixMilli(ms).UTC(), nil
}
