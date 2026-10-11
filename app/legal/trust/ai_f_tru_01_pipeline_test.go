package trust_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
)

// Synthetic bank-feed lines of one trust account statement; the second carries an
// injection.
var truFeedLines = []agenttools.Quoted{
	{Source: agenttools.Citation{VersionID: "ver-feed-1", Page: 1, FromLine: 1, ToLine: 1}, Text: "2026-10-05 DEPOSIT CHEQUE 4411 REF 2026-0101 $1,250.00"},
	{Source: agenttools.Citation{VersionID: "ver-feed-1", Page: 1, FromLine: 2, ToLine: 2}, Text: truInjected},
	{Source: agenttools.Citation{VersionID: "ver-feed-1", Page: 1, FromLine: 3, ToLine: 3}, Text: "2026-10-06 WIRE IN REF SYNTH RETAINER 500000 cents"},
}

// truReceiptSink records what the pipeline files.
type truReceiptSink struct {
	calls int
	items []trust.ProposedReceipt
}

func (s *truReceiptSink) ProposeReceipts(_ context.Context, featureID string, items []trust.ProposedReceipt) error {
	if featureID != trust.FeatureBankFeedSync {
		return errors.New("wrong feature id")
	}
	s.calls++
	s.items = append(s.items, items...)
	return nil
}

const (
	truCheque  = `{"kind": "receipt", "amount_cents": 125000, "received_on": "2026-10-05", "method": "cheque", "reference": "2026-0101", "citation": {"version_id": "ver-feed-1", "page": 1, "from_line": 1, "to_line": 1}}`
	truWire    = `{"kind": "match", "amount_cents": 500000, "received_on": "2026-10-06", "method": "wire", "reference": "SYNTH RETAINER", "citation": {"version_id": "ver-feed-1", "page": 1, "from_line": 3, "to_line": 3}}`
	truFeedOut = `{"json": {"items": [`
)

func truFeedReply(items ...string) string {
	return truFeedOut + strings.Join(items, ", ") + `]}}`
}

// rule: ARC-I-038
// rule: AI-R-003
// rule: AI-R-005
// rule: ARC-U-027
func TestARC_I_038_BankFeedSyncPipeline(t *testing.T) {
	cases := []struct {
		name      string
		reply     string
		opts      []aifake.Option
		wantErr   error
		wantKinds []string
	}{
		{"cited receipt and match", truFeedReply(truCheque, truWire), nil, nil, []string{trust.ItemReceipt, trust.ItemMatch}},
		{"injection stays quoted and adds no item", truFeedReply(truCheque), nil, nil, []string{trust.ItemReceipt}},
		{"no deposits", truFeedReply(), nil, nil, nil},
		{"invalid model json", `{"text": "Two deposits came in."}`, nil, run.ErrExtractionSchema, nil},
		{"fails schema", truFeedReply(strings.Replace(truCheque, `"received_on": "2026-10-05"`, `"received_on": "5 Oct"`, 1)), nil, run.ErrExtractionSchema, nil},
		{"amount with a fraction of a cent", truFeedReply(strings.Replace(truCheque, "125000", "1250.5", 1)), nil, run.ErrExtractionSchema, nil},
		{"amount in dollars, not cents", truFeedReply(strings.Replace(truCheque, "125000", "1250", 1)), nil, agenttools.ErrUnsourcedValue, nil},
		{"item without citation", truFeedReply(truCheque, `{"kind": "receipt", "amount_cents": 500000, "received_on": "2026-10-06"}`), nil, run.ErrExtractionSchema, nil},
		{"cites a line it was not given", truFeedReply(strings.Replace(truCheque, "ver-feed-1", "ver-other", 1)), nil, run.ErrUncitedAnswer, nil},
		// rule: ARC-U-027
		{"model-invented amount", truFeedReply(strings.Replace(truCheque, "125000", "125001", 1)), nil, agenttools.ErrUnsourcedValue, nil},
		{"amount of another line", truFeedReply(strings.Replace(truCheque, "125000", "500000", 1)), nil, agenttools.ErrUnsourcedValue, nil},
		{"date the line does not state", truFeedReply(strings.Replace(truCheque, "2026-10-05", "2026-10-07", 1)), nil, agenttools.ErrUnsourcedValue, nil},
		{"date not on the calendar", truFeedReply(strings.Replace(truCheque, "2026-10-05", "2026-02-30", 1)), nil, trust.ErrBadReceipt, nil},
		{"rate limited", truFeedReply(truCheque), []aifake.Option{aifake.WithFault(trust.FeatureBankFeedSync, aifake.FaultRateLimit)}, ai.ErrRateLimited, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := truCaptureLogs(t)
			fake := truScripted(t, trust.FeatureBankFeedSync, tc.reply, tc.opts...)
			model := &truRecorder{ModelClient: fake}
			sink := &truReceiptSink{}
			p := trust.BankFeedPipeline{Loop: run.New(model, agenttoolsfake.New()), Embedder: fake, Sink: sink}

			items, err := p.Run(context.Background(), truFeedLines)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				// Nothing is written when the output does not check out.
				if items != nil || sink.calls != 0 || len(sink.items) != 0 {
					t.Fatalf("wrote %d items in %d calls on error", len(sink.items), sink.calls)
				}
				return
			}
			if len(items) != len(tc.wantKinds) || len(sink.items) != len(tc.wantKinds) || sink.calls != min(len(tc.wantKinds), 1) {
				t.Fatalf("items = %d, written %d in %d calls, want %d", len(items), len(sink.items), sink.calls, len(tc.wantKinds))
			}
			for i, it := range sink.items {
				if it.Kind != tc.wantKinds[i] {
					t.Fatalf("item %d kind = %s, want %s", i, it.Kind, tc.wantKinds[i])
				}
				// rule: ARC-I-038
				// Every item cites its own line, never the injected one.
				if err := agenttools.ValidCitation(it.Citation); err != nil || it.Citation.FromLine == truFeedLines[1].Source.FromLine {
					t.Fatalf("item %d does not cite a deposit line: %+v %v", i, it.Citation, err)
				}
				if it.AmountCents <= 0 || strings.Contains(it.Reference, "ignore previous") {
					t.Fatalf("item %d = %+v", i, it)
				}
			}
			if len(model.reqs) != 1 || model.reqs[0].Kind != ai.KindExtraction || len(model.reqs[0].Tools) != 0 {
				t.Fatalf("model requests = %d, want one extraction call without tools", len(model.reqs))
			}
			if !strings.HasPrefix(model.reqs[0].System, feature.Preamble(trust.FeatureBankFeedSync)+feature.Rules()) {
				t.Fatalf("system prompt does not start with the preamble and shared rules")
			}
			for _, s := range truFeedLines {
				truQuotedOnly(t, model.reqs, s.Text)
			}
			if e := fake.EmbedJournal(); len(e) != 1 || e[0].FeatureID != trust.FeatureBankFeedSync || e[0].Inputs != len(truFeedLines)+1 {
				t.Fatalf("embed journal = %+v, want one call of %d inputs", e, len(truFeedLines)+1)
			}
			// rule: AI-R-005
			for _, s := range []string{truFeedLines[0].Text, truInjected, "125000", "SYNTH RETAINER"} {
				if strings.Contains(logs.String(), s) {
					t.Fatalf("log carries a bank-feed line or an amount")
				}
			}
		})
	}
}

// rule: AI-R-003
func TestBankFeedSyncPipelineFailsClosed(t *testing.T) {
	fake := truScripted(t, trust.FeatureBankFeedSync, truFeedReply(truCheque))
	loop := run.New(fake, agenttoolsfake.New())
	tests := []struct {
		name    string
		p       trust.BankFeedPipeline
		lines   []agenttools.Quoted
		wantErr error
	}{
		{"no sink", trust.BankFeedPipeline{Loop: loop, Embedder: fake}, truFeedLines, trust.ErrPortMissing},
		{"no embedder", trust.BankFeedPipeline{Loop: loop, Sink: &truReceiptSink{}}, truFeedLines, trust.ErrPortMissing},
		{"no loop", trust.BankFeedPipeline{Embedder: fake, Sink: &truReceiptSink{}}, truFeedLines, trust.ErrPortMissing},
		{"line without citation", trust.BankFeedPipeline{Loop: loop, Embedder: fake, Sink: &truReceiptSink{}}, []agenttools.Quoted{{Text: "2026-10-05 DEPOSIT $1.00"}}, agenttools.ErrCitation},
		{"no lines", trust.BankFeedPipeline{Loop: loop, Embedder: fake, Sink: &truReceiptSink{}}, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, err := tt.p.Run(context.Background(), tt.lines)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) || items != nil {
				t.Fatalf("items = %v, err = %v, want %v", items, err, tt.wantErr)
			}
			if e := fake.EmbedJournal(); len(e) != 0 {
				t.Fatalf("embedded %d times; want no model call", len(e))
			}
		})
	}
}

// rule: AI-R-003
// rule: ARC-I-035
func TestFTRU01_DefMatchesSpec(t *testing.T) {
	d := trust.BankFeedSyncDef()
	truSpecSync(t, d, true)
	if len(d.OutputSchema) == 0 {
		t.Fatal("extraction feature without output schema")
	}
	if !slices.Equal(d.GateKinds, []string{trust.GateReceiptClassification}) {
		t.Fatalf("gate kinds = %v, want the receipt classification gate (G6)", d.GateKinds)
	}
	if sf := truSpec(t, d.ID); !slices.Equal(sf.ToolGap, d.GateKinds) {
		t.Fatalf("tool-gap kinds = %v, want %v opened by draft_document then submit_for_approval", sf.ToolGap, d.GateKinds)
	}
}

// rule: AI-R-005
func TestFTRU01_Prompt(t *testing.T) {
	truPrompt(t, trust.BankFeedSyncDef(), "draft_document", "submit_for_approval", "integer cents")
}
