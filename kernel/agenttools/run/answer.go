package run

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dagi377/counsel/kernel/agenttools"
)

// ErrUncitedAnswer means a chat answer cites a source no tool result of the same run returned,
// carries a citation in the wrong shape, or carries no citation when its Spec requires one.
var ErrUncitedAnswer = errors.New("run: answer citation not returned by this run")

// AnswerSchema is the JSON Schema of a final chat answer, for Spec.OutputSchema.
var AnswerSchema = json.RawMessage(`{
  "type": "object",
  "required": ["answer", "citations"],
  "additionalProperties": false,
  "properties": {
    "answer": {"type": "string", "minLength": 1},
    "citations": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["version_id", "page", "from_line", "to_line"],
        "additionalProperties": false,
        "properties": {
          "version_id": {"type": "string", "minLength": 1},
          "page": {"type": "integer"},
          "from_line": {"type": "integer"},
          "to_line": {"type": "integer"}
        }
      }
    }
  }
}`)

// Answer is a checked chat answer. Text goes back to the caller only; nothing stores or logs
// it (AI-R-005).
type Answer struct {
	Text      string
	Citations []agenttools.Citation
}

type answerJSON struct {
	Answer    string         `json:"answer"`
	Citations []citationJSON `json:"citations"`
}

type citationJSON struct {
	VersionID string `json:"version_id"`
	Page      int    `json:"page"`
	FromLine  int    `json:"from_line"`
	ToLine    int    `json:"to_line"`
}

// CheckAnswer reads the final chat answer of out ({answer, citations[]}, from out.JSON, else
// out.Text) and checks every citation against the citations this run's tool results carried.
// A citation must have the kernel shape and fall inside a citation of the same run: same
// version and page, lines within the returned range. An answer with no citations passes only
// when spec.AnswerMayBeUncited. Errors never carry the answer text.
func CheckAnswer(spec Spec, out Outcome) (Answer, error) {
	raw := bytes.TrimSpace(out.JSON)
	if len(raw) == 0 {
		raw = bytes.TrimSpace([]byte(out.Text))
	}
	var a answerJSON
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	// The decoder's detail can echo answer text, so it stays out of the error.
	if err := dec.Decode(&a); err != nil || strings.TrimSpace(a.Answer) == "" {
		return Answer{}, fmt.Errorf("answer %s: %w", spec.FeatureID, ErrBadOutput)
	}
	// rule: AI-R-005
	// The answer must be grounded: zero citations only when the spec allows it.
	if len(a.Citations) == 0 && !spec.AnswerMayBeUncited {
		return Answer{}, fmt.Errorf("answer %s: no citations: %w", spec.FeatureID, ErrUncitedAnswer)
	}
	ans := Answer{Text: a.Answer, Citations: make([]agenttools.Citation, 0, len(a.Citations))}
	for i, cj := range a.Citations {
		c := agenttools.Citation{VersionID: cj.VersionID, Page: cj.Page, FromLine: cj.FromLine, ToLine: cj.ToLine}
		// rule: ARC-I-038
		if err := agenttools.ValidCitation(c); err != nil {
			return Answer{}, fmt.Errorf("answer %s: citation %d: %w: %w", spec.FeatureID, i, ErrUncitedAnswer, err)
		}
		// rule: ARC-I-038
		// Only a source a tool result of this run returned can be cited.
		if !returned(out.Citations, c) {
			return Answer{}, fmt.Errorf("answer %s: citation %d (%s p%d): %w", spec.FeatureID, i, c.VersionID, c.Page, ErrUncitedAnswer)
		}
		ans.Citations = append(ans.Citations, c)
	}
	return ans, nil
}

// returned reports whether c falls inside one of the run's citations.
func returned(run []agenttools.Citation, c agenttools.Citation) bool {
	for _, r := range run {
		if r.VersionID == c.VersionID && r.Page == c.Page && r.FromLine <= c.FromLine && c.ToLine <= r.ToLine {
			return true
		}
	}
	return false
}
