package clio

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/flags"
)

var (
	// ErrReconImportUnverified is returned while the By-Law 9 flag is off.
	ErrReconImportUnverified = errors.New("clio: imported reconciliations: by-law 9 rules not verified")
	// ErrBeforeLiveFrom is returned for a Counsel reconciliation of a month before live_from.
	ErrBeforeLiveFrom = errors.New("clio: month before live_from")
	// ErrMonthLocked is returned for a live row dated in an imported (locked) month.
	ErrMonthLocked = errors.New("clio: month locked")
	// ErrPriorUnreconciled is returned when month M-1 is neither signed nor imported.
	ErrPriorUnreconciled = errors.New("clio: prior month not reconciled")
	// ErrBadReconImport is returned for a malformed Clio reconciliation.
	ErrBadReconImport = errors.New("clio: bad imported reconciliation")
)

// ReconStatus is the status of a month's reconciliation as the order check sees it.
type ReconStatus string

// Statuses. Imported is a Clio month; Counsel never re-signs it.
const (
	ReconImported ReconStatus = "imported"
	ReconSigned   ReconStatus = "signed"
)

// ClioRecon is one reconciliation as read from Clio.
type ClioRecon struct {
	AccountID  string
	Period     recon.Period
	SignerName string // Clio's signer, kept as text; never a Counsel user
}

// ImportedRecon is a Clio reconciliation stored in Counsel.
type ImportedRecon struct {
	AccountID      string
	Period         recon.Period
	Status         ReconStatus
	ExternalSigner string
	ImportJobID    string
	SignedBy       string // always empty: no Counsel user signed an imported month
}

// ImportRecon stores a Clio reconciliation as status imported, signer kept as text.
func ImportRecon(ctx context.Context, jobID string, c ClioRecon) (ImportedRecon, error) {
	if !flags.Enabled(ctx, FlagBylaw9Verified) {
		return ImportedRecon{}, ErrReconImportUnverified
	}
	if c.AccountID == "" || jobID == "" || c.Period.Month < time.January || c.Period.Month > time.December {
		return ImportedRecon{}, fmt.Errorf("import recon: %w", ErrBadReconImport)
	}
	// rule: MIG-038 (verify)
	return ImportedRecon{
		AccountID:      c.AccountID,
		Period:         c.Period,
		Status:         ReconImported,
		ExternalSigner: c.SignerName,
		ImportJobID:    jobID,
	}, nil
}

// LiveFrom is the first Counsel month: the month after the cut-over date.
func LiveFrom(cutover clock.Date) recon.Period {
	// rule: MIG-038 (verify)
	if cutover.Month == time.December {
		return recon.Period{Year: cutover.Year + 1, Month: time.January}
	}
	return recon.Period{Year: cutover.Year, Month: cutover.Month + 1}
}

func periodBefore(a, b recon.Period) bool {
	if a.Year != b.Year {
		return a.Year < b.Year
	}
	return a.Month < b.Month
}

func prevPeriod(p recon.Period) recon.Period {
	if p.Month == time.January {
		return recon.Period{Year: p.Year - 1, Month: time.December}
	}
	return recon.Period{Year: p.Year, Month: p.Month - 1}
}

// CheckCounselRecon checks that a Counsel reconciliation for p may be created and signed.
// prior maps each earlier month to its reconciliation status. Month M-1 must be signed or
// imported, so the first Counsel month (live_from) signs on an imported Clio month.
func CheckCounselRecon(ctx context.Context, liveFrom, p recon.Period, prior map[recon.Period]ReconStatus) error {
	if !flags.Enabled(ctx, FlagBylaw9Verified) {
		return ErrReconImportUnverified
	}
	// rule: MIG-038 (verify)
	if periodBefore(p, liveFrom) {
		return fmt.Errorf("check recon %d-%02d: %w", p.Year, p.Month, ErrBeforeLiveFrom)
	}
	switch prior[prevPeriod(p)] {
	case ReconSigned, ReconImported:
		return nil
	default:
		return fmt.Errorf("check recon %d-%02d: %w", p.Year, p.Month, ErrPriorUnreconciled)
	}
}

// CheckLivePosting refuses a live trust row dated before live_from: those months are
// imported and month-locked like signed months.
func CheckLivePosting(ctx context.Context, liveFrom recon.Period, on clock.Date) error {
	if !flags.Enabled(ctx, FlagBylaw9Verified) {
		return ErrReconImportUnverified
	}
	// rule: MIG-038 (verify)
	if periodBefore(recon.Period{Year: on.Year, Month: on.Month}, liveFrom) {
		return fmt.Errorf("check live posting %s: %w", on, ErrMonthLocked)
	}
	return nil
}
