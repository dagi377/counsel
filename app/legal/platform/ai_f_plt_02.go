package platform

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
)

//go:embed prompts/f-plt-02.md
var promptFPLT02 embed.FS

// FeatureMailFiling is the spec id of "Mail filing".
const FeatureMailFiling = "F-PLT-02"

// Comms-log screen actions that run F-PLT-02 on request.
const (
	ActionAutoFileEmail     = "comms-log > Auto-file email to matter"
	ActionProposeTimeFromCm = "comms-log > Propose time entry"
)

// Mail filing item types.
const (
	MailFile      = "file"       // where the email goes: filed, or the unfiled queue (G-D1)
	MailDeadline  = "deadline"   // a date the email sets, for G7
	MailTimeEntry = "time_entry" // the lawyer's work the email records, for G9
)

// ErrBadMailItem means a mail filing item is not usable; the whole output is dropped.
var ErrBadMailItem = errors.New("platform: mail filing item is not usable")

// mailSchema is the F-PLT-02 extraction output: at most one filing for the email, the
// deadlines it sets and the time it records, each citing the quoted passage it came from
// (run.ItemsField, run.CitationField).
var mailSchema = json.RawMessage(`{
  "type": "object",
  "required": ["items"],
  "additionalProperties": false,
  "properties": {
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["type", "citation"],
        "additionalProperties": false,
        "properties": {
          "type": {"enum": ["file", "deadline", "time_entry"]},
          "file_number": {"type": "string", "maxLength": 50},
          "privileged": {"type": "boolean"},
          "confidence_ppm": {"type": "integer", "minimum": 0, "maximum": 1000000},
          "title": {"type": "string", "minLength": 1, "maxLength": 200},
          "due_date": {"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"},
          "work_date": {"type": "string", "pattern": "^[0-9]{4}-[0-9]{2}-[0-9]{2}$"},
          "minutes": {"type": "integer", "minimum": 1, "maximum": 1440},
          "narrative": {"type": "string", "minLength": 1, "maxLength": 500},
          "citation": {
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
    }
  }
}`)

// MailFilingDef is F-PLT-02: when new mail arrives through a connector, one extraction call
// over the quoted email finds the matter it belongs to, the deadlines it sets and the time it
// records. A confident, non-privileged match is filed as a CommItem; anything else waits in
// the unfiled queue behind docs.filing_privilege (G-D1). Deadlines open cal.confirm (G7) and
// time entries open timekeeping.accept (G9), each with the acting user as preparer.
func MailFilingDef() feature.Def {
	return feature.Def{
		ID:      FeatureMailFiling,
		Area:    "platform",
		Name:    "Mail filing",
		Source:  feature.SourceJob,
		Trigger: "event: new mail via connector",
		Actions: []string{ActionAutoFileEmail, ActionProposeTimeFromCm},
		Kind:    ai.KindExtraction,
		Tools: []string{
			"get_matter", "search_documents", "read_document", "draft_document",
			"propose_deadline", "propose_time_entries", "report_plan", "report_step", "submit_for_approval",
		},
		// rule: ARC-I-035
		// docs.filing_privilege is a tool-gap kind with no write tool of its own: the run drafts
		// the filing review with draft_document and opens it with submit_for_approval.
		// propose_deadline opens cal.confirm and propose_time_entries opens timekeeping.accept.
		GateKinds:    []string{legaldocs.GateFilingPrivilege, agent.GateCalConfirm, agent.GateTimeAccept},
		Grant:        []string{"agent.ask", "deadlines.edit", "documents.edit", "documents.view", "matters.view", "time.record"},
		Prompt:       promptFPLT02,
		PromptPath:   "prompts/f-plt-02.md",
		Fixture:      "testdata/ai/f-plt-02/mail-filing",
		OutputSchema: mailSchema,
	}
}

// MailFiling says where the email goes. FileNumber is model text: it goes to the sink only
// and is never logged.
type MailFiling struct {
	FileNumber    string // the matter file number the email names; "" when it names none
	Privileged    bool
	ConfidencePPM int
	// Queued: no file number, privileged, or under legaldocs.AutoFileReviewBelowPPM, so the
	// email waits in the unfiled queue for G-D1 instead of being filed.
	Queued   bool
	Citation agenttools.Citation
}

// MailDeadlineItem is a date the email sets, for G7. Title is model text.
type MailDeadlineItem struct {
	Title    string
	DueDate  string // YYYY-MM-DD, America/Toronto
	Citation agenttools.Citation
}

// MailTimeItem is the lawyer's work the email records, for G9. Narrative is model text.
type MailTimeItem struct {
	WorkDate  string // YYYY-MM-DD, America/Toronto
	Minutes   int
	Narrative string
	Citation  agenttools.Citation
}

// MailFiled is one run's checked output.
type MailFiled struct {
	Filing    *MailFiling // nil: the email names nothing to file
	Deadlines []MailDeadlineItem
	Time      []MailTimeItem
}

// empty reports whether m holds nothing to write.
func (m MailFiled) empty() bool {
	return m.Filing == nil && len(m.Deadlines) == 0 && len(m.Time) == 0
}

// MailFilingSink files the email or queues it for G-D1, proposes the deadlines for G7 and the
// time for G9, with the acting user as preparer. It is called once per run, only after the
// whole output checked out.
type MailFilingSink interface {
	FileMail(ctx context.Context, featureID string, m MailFiled) error
}

// MailFilingPipeline is F-PLT-02's extraction pipeline.
type MailFilingPipeline struct {
	Loop *run.Loop
	Sink MailFilingSink
}

// Run sends the quoted email to one extraction call and hands the cited filing, deadlines
// and time to the Sink. Output that fails the schema, cites a passage it was not given,
// carries an impossible date or holds two filings writes nothing.
func (p MailFilingPipeline) Run(ctx context.Context, sources []agenttools.Quoted) (MailFiled, error) {
	def := MailFilingDef()
	spec, err := def.Spec()
	if err != nil {
		return MailFiled{}, err
	}
	if len(sources) == 0 {
		return MailFiled{}, nil
	}
	// rule: ARC-I-038
	// The email reaches the model only as quoted sources; Extract checks every item cites one.
	out, err := p.Loop.Extract(ctx, spec, sources)
	if err != nil {
		return MailFiled{}, err
	}
	m, err := decodeMail(out)
	if err != nil {
		return MailFiled{}, fmt.Errorf("%s: %w", def.ID, err)
	}
	if m.empty() {
		return MailFiled{}, nil
	}
	if p.Sink == nil {
		return MailFiled{}, fmt.Errorf("%s: no sink: %w", def.ID, ErrBadMailItem)
	}
	// rule: AI-R-003
	// A queued email opens G-D1, every deadline G7 and every time entry G9; a person other
	// than the preparer decides them.
	if err := p.Sink.FileMail(ctx, def.ID, m); err != nil {
		return MailFiled{}, fmt.Errorf("%s: file: %w", def.ID, err)
	}
	// rule: AI-R-005
	// Only the feature id and counts are logged, never file numbers, titles or passages.
	slog.InfoContext(ctx, "mail filed", "feature", def.ID,
		"queued", m.Filing != nil && m.Filing.Queued, "deadlines", len(m.Deadlines), "time_entries", len(m.Time))
	return m, nil
}

// decodeMail reads checked output. Errors carry positions only, never model text.
func decodeMail(out json.RawMessage) (MailFiled, error) {
	var doc struct {
		Items []struct {
			Type          string `json:"type"`
			FileNumber    string `json:"file_number"`
			Privileged    bool   `json:"privileged"`
			ConfidencePPM *int   `json:"confidence_ppm"`
			Title         string `json:"title"`
			DueDate       string `json:"due_date"`
			WorkDate      string `json:"work_date"`
			Minutes       int    `json:"minutes"`
			Narrative     string `json:"narrative"`
			Citation      struct {
				VersionID string `json:"version_id"`
				Page      int    `json:"page"`
				FromLine  int    `json:"from_line"`
				ToLine    int    `json:"to_line"`
			} `json:"citation"`
		} `json:"items"`
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return MailFiled{}, fmt.Errorf("decode output: %w", ErrBadMailItem)
	}
	var m MailFiled
	for i, it := range doc.Items {
		cite := agenttools.Citation{VersionID: it.Citation.VersionID, Page: it.Citation.Page,
			FromLine: it.Citation.FromLine, ToLine: it.Citation.ToLine}
		switch it.Type {
		case MailFile:
			if m.Filing != nil || it.ConfidencePPM == nil || it.Title != "" || it.DueDate != "" || it.Minutes != 0 {
				return MailFiled{}, fmt.Errorf("item %d: file: %w", i, ErrBadMailItem)
			}
			conf := *it.ConfidencePPM
			m.Filing = &MailFiling{
				FileNumber: it.FileNumber, Privileged: it.Privileged, ConfidencePPM: conf,
				Queued:   it.FileNumber == "" || it.Privileged || conf < legaldocs.AutoFileReviewBelowPPM,
				Citation: cite,
			}
		case MailDeadline:
			if it.Title == "" || !mailDate(it.DueDate) {
				return MailFiled{}, fmt.Errorf("item %d: deadline: %w", i, ErrBadMailItem)
			}
			m.Deadlines = append(m.Deadlines, MailDeadlineItem{Title: it.Title, DueDate: it.DueDate, Citation: cite})
		case MailTimeEntry:
			if it.Minutes <= 0 || it.Narrative == "" || !mailDate(it.WorkDate) {
				return MailFiled{}, fmt.Errorf("item %d: time entry: %w", i, ErrBadMailItem)
			}
			m.Time = append(m.Time, MailTimeItem{WorkDate: it.WorkDate, Minutes: it.Minutes, Narrative: it.Narrative, Citation: cite})
		default:
			return MailFiled{}, fmt.Errorf("item %d: type: %w", i, ErrBadMailItem)
		}
	}
	return m, nil
}

// mailDate reports whether s is a real calendar date, YYYY-MM-DD.
func mailDate(s string) bool {
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}
