package matter

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

var (
	// ErrUnknownStage is returned for a stage that is not on the matter's active stage list.
	ErrUnknownStage = errors.New("matter: stage not on the stage list")
	// ErrStageSkip is returned for a forward move that skips a stage on the list.
	ErrStageSkip = errors.New("matter: stage move skips a stage")
	// ErrSameStage is returned for a move to the stage the matter is already in.
	ErrSameStage = errors.New("matter: already in this stage")
)

// Stage is one matter.stage row: a step in a practice area's ordered stage list.
type Stage struct {
	ID             string
	PracticeAreaID string
	Name           string
	Ordinal        int
	Archived       bool
}

// StageChange is one append-only matter.stage_change row. FromStageID is empty for the first
// stage a matter enters.
type StageChange struct {
	MatterID    ID
	FromStageID string
	ToStageID   string
	ApprovedBy  string
	At          time.Time
}

// StageList is a practice area's active stages in ordinal order.
type StageList []Stage

// NewStageList returns the active stages of practiceAreaID from all, ordered by ordinal.
func NewStageList(practiceAreaID string, all []Stage) StageList {
	var out StageList
	for _, s := range all {
		if s.PracticeAreaID == practiceAreaID && !s.Archived {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ordinal < out[j].Ordinal })
	return out
}

func (l StageList) index(id string) int {
	for i, s := range l {
		if s.ID == id {
			return i
		}
	}
	return -1
}

// Move checks a stage move from (empty when the matter has no stage yet) to to and returns the
// StageChange to append. Moves follow the list: forward one stage at a time (a new matter
// enters the first stage), and back to any earlier stage.
func (l StageList) Move(matterID ID, from, to, by string, at time.Time) (StageChange, error) {
	ti := l.index(to)
	if ti < 0 {
		return StageChange{}, fmt.Errorf("stage move: %w", ErrUnknownStage)
	}
	fi := -1
	if from != "" {
		if fi = l.index(from); fi < 0 {
			return StageChange{}, fmt.Errorf("stage move: from: %w", ErrUnknownStage)
		}
	}
	switch {
	case ti == fi:
		return StageChange{}, ErrSameStage
	case ti > fi+1:
		return StageChange{}, fmt.Errorf("stage move: %w", ErrStageSkip)
	}
	return StageChange{MatterID: matterID, FromStageID: from, ToStageID: to, ApprovedBy: by, At: at}, nil
}
