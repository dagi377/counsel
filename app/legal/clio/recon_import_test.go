package clio

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/flags"
)

func reconCtx(t *testing.T, on bool) context.Context {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: reconImportFlag}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(reconImportFlag, on); err != nil {
		t.Fatal(err)
	}
	return flags.WithSet(context.Background(), s)
}

var (
	oct2026 = recon.Period{Year: 2026, Month: time.October}
	nov2026 = recon.Period{Year: 2026, Month: time.November}
	dec2026 = recon.Period{Year: 2026, Month: time.December}
)

// rule: MIG-038 (verify)
func TestMIG_038_ImportedReconStatus(t *testing.T) {
	tests := []struct {
		name    string
		flagOn  bool
		jobID   string
		in      ClioRecon
		wantErr error
	}{
		{"flag off", false, "job-1", ClioRecon{AccountID: "A-1", Period: oct2026, SignerName: "J. Smith"}, ErrReconImportUnverified},
		{"imported, signer kept as text", true, "job-1", ClioRecon{AccountID: "A-1", Period: oct2026, SignerName: "J. Smith"}, nil},
		{"missing job", true, "", ClioRecon{AccountID: "A-1", Period: oct2026}, ErrBadReconImport},
		{"bad month", true, "job-1", ClioRecon{AccountID: "A-1", Period: recon.Period{Year: 2026}}, ErrBadReconImport},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ImportRecon(reconCtx(t, tt.flagOn), tt.jobID, tt.in)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.Status != ReconImported || got.ExternalSigner != tt.in.SignerName || got.SignedBy != "" || got.ImportJobID != tt.jobID {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

// rule: MIG-038 (verify)
func TestMIG_038_LiveFromIsMonthAfterCutover(t *testing.T) {
	tests := []struct {
		name    string
		cutover clock.Date
		want    recon.Period
	}{
		{"october cut-over", clock.Date{Year: 2026, Month: time.October, Day: 31}, nov2026},
		{"december rolls the year", clock.Date{Year: 2026, Month: time.December, Day: 31}, recon.Period{Year: 2027, Month: time.January}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LiveFrom(tt.cutover); got != tt.want {
				t.Fatalf("LiveFrom = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// rule: MIG-038 (verify)
func TestMIG_038_FirstCounselMonthSignsWithoutClioSignoff(t *testing.T) {
	liveFrom := LiveFrom(clock.Date{Year: 2026, Month: time.October, Day: 31})
	tests := []struct {
		name    string
		flagOn  bool
		p       recon.Period
		prior   map[recon.Period]ReconStatus
		wantErr error
	}{
		{"flag off", false, nov2026, map[recon.Period]ReconStatus{oct2026: ReconImported}, ErrReconImportUnverified},
		{"november signs on imported october", true, nov2026, map[recon.Period]ReconStatus{oct2026: ReconImported}, nil},
		{"december needs november signed", true, dec2026, map[recon.Period]ReconStatus{oct2026: ReconImported}, ErrPriorUnreconciled},
		{"december after signed november", true, dec2026, map[recon.Period]ReconStatus{oct2026: ReconImported, nov2026: ReconSigned}, nil},
		{"october is before live_from", true, oct2026, nil, ErrBeforeLiveFrom},
		{"november without imported october", true, nov2026, nil, ErrPriorUnreconciled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckCounselRecon(reconCtx(t, tt.flagOn), liveFrom, tt.p, tt.prior)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// rule: MIG-038 (verify)
func TestMIG_038_ImportedMonthsLocked(t *testing.T) {
	tests := []struct {
		name    string
		flagOn  bool
		on      clock.Date
		wantErr error
	}{
		{"flag off", false, clock.Date{Year: 2026, Month: time.November, Day: 1}, ErrReconImportUnverified},
		{"row in imported october", true, clock.Date{Year: 2026, Month: time.October, Day: 15}, ErrMonthLocked},
		{"row in first counsel month", true, clock.Date{Year: 2026, Month: time.November, Day: 1}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckLivePosting(reconCtx(t, tt.flagOn), nov2026, tt.on)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
