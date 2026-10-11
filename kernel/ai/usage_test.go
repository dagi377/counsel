package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

const secret = "Jane Roe owes 41500 on the Acme file"

type stubClient struct {
	calls int
	resp  Response
	err   error
}

func (s *stubClient) Mode() Mode { return ModeMock }

func (s *stubClient) Complete(ctx context.Context, req Request) (Response, error) {
	s.calls++
	return s.resp, s.err
}

func (s *stubClient) Stream(ctx context.Context, req Request) (<-chan StreamEvent, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	ch := make(chan StreamEvent, 3)
	ch <- StreamEvent{TextDelta: s.resp.Text}
	resp := s.resp
	ch <- StreamEvent{Done: &resp}
	close(ch)
	return ch, nil
}

func (s *stubClient) Embed(ctx context.Context, req EmbedRequest) (EmbedResponse, error) {
	s.calls++
	return EmbedResponse{Vectors: [][]float32{{1}}, Usage: s.resp.Usage}, s.err
}

// captureRecorder keeps every record as JSON, the way a persistent store would see it.
type captureRecorder struct {
	rows []string
	err  error
}

func (c *captureRecorder) Record(ctx context.Context, rec UsageRecord) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	c.rows = append(c.rows, string(b))
	return c.err
}

func secretRequest(feature string) Request {
	return Request{
		FeatureID: feature,
		Kind:      KindToolUse,
		Model:     "drafting",
		System:    "system " + secret,
		Messages: []Message{{Role: "user", Content: []ContentBlock{
			{Type: "text", Text: secret},
			{Type: "tool_result", ToolName: "read_note", Input: json.RawMessage(`{"q":"` + secret + `"}`)},
		}}},
		Metadata: map[string]string{"run_id": "run-1"},
	}
}

// rule: AI-R-005
func TestAI_R_005_UsageRecordedPerFeature(t *testing.T) {
	tests := []struct {
		name  string
		calls []struct {
			feature string
			usage   Usage
		}
		want []FeatureUsage
	}{
		{
			name: "two features",
			calls: []struct {
				feature string
				usage   Usage
			}{
				{"F-TIM-01", Usage{InputTokens: 812, OutputTokens: 64, CostMicroCents: 1200}},
				{"F-TIM-01", Usage{InputTokens: 100, OutputTokens: 10, CacheReadTokens: 5, CostMicroCents: 300}},
				{"F-BIL-01", Usage{InputTokens: 7, OutputTokens: 3, CostMicroCents: 9}},
			},
			want: []FeatureUsage{
				{FeatureID: "F-BIL-01", Calls: 1, Usage: Usage{InputTokens: 7, OutputTokens: 3, CostMicroCents: 9}},
				{FeatureID: "F-TIM-01", Calls: 2, Usage: Usage{InputTokens: 912, OutputTokens: 74, CacheReadTokens: 5, CostMicroCents: 1500}},
			},
		},
		{name: "no calls", want: []FeatureUsage{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var ledger UsageLedger
			for _, c := range tc.calls {
				stub := &stubClient{resp: Response{Text: secret, Usage: c.usage}}
				if _, err := WithUsage(stub, &ledger).Complete(context.Background(), secretRequest(c.feature)); err != nil {
					t.Fatalf("Complete: %v", err)
				}
			}
			if got := ledger.All(); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("All() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// rule: AI-R-005
func TestAI_R_005_NoFeatureRejectedBeforeModelCall(t *testing.T) {
	tests := []struct {
		name string
		call func(c ModelClient, e Embedder) error
	}{
		{name: "complete", call: func(c ModelClient, e Embedder) error {
			_, err := c.Complete(context.Background(), Request{Model: "fast"})
			return err
		}},
		{name: "stream", call: func(c ModelClient, e Embedder) error {
			_, err := c.Stream(context.Background(), Request{Model: "fast"})
			return err
		}},
		{name: "embed", call: func(c ModelClient, e Embedder) error {
			_, err := e.Embed(context.Background(), EmbedRequest{Inputs: []string{"x"}})
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubClient{}
			var ledger UsageLedger
			err := tc.call(WithUsage(stub, &ledger), WithEmbedUsage(stub, &ledger, ModeMock))
			if !errors.Is(err, ErrNoFeature) {
				t.Fatalf("err = %v, want ErrNoFeature", err)
			}
			if stub.calls != 0 {
				t.Fatalf("model called %d times, want 0", stub.calls)
			}
			if got := ledger.All(); len(got) != 0 {
				t.Fatalf("ledger = %+v, want empty", got)
			}
		})
	}
}

// rule: AI-R-005
func TestAI_R_005_NoPromptOrCompletionTextPersisted(t *testing.T) {
	usage := Usage{InputTokens: 10, OutputTokens: 2, CostMicroCents: 40}
	tests := []struct {
		name      string
		recordErr error
		call      func(c ModelClient, e Embedder) error
	}{
		{name: "complete", call: func(c ModelClient, e Embedder) error {
			_, err := c.Complete(context.Background(), secretRequest("F-TIM-01"))
			return err
		}},
		{name: "complete recorder fails", recordErr: errors.New("store down"), call: func(c ModelClient, e Embedder) error {
			_, err := c.Complete(context.Background(), secretRequest("F-TIM-01"))
			return err
		}},
		{name: "stream", call: func(c ModelClient, e Embedder) error {
			ch, err := c.Stream(context.Background(), secretRequest("F-TIM-01"))
			if err != nil {
				return err
			}
			for range ch {
			}
			return nil
		}},
		{name: "embed", call: func(c ModelClient, e Embedder) error {
			_, err := e.Embed(context.Background(), EmbedRequest{FeatureID: "F-TIM-01", Inputs: []string{secret}})
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			defer slog.SetDefault(prev)

			stub := &stubClient{resp: Response{Text: secret, ToolCalls: []ToolCall{{Name: "x", Input: json.RawMessage(`"` + secret + `"`)}}, Usage: usage}}
			rec := &captureRecorder{err: tc.recordErr}
			if err := tc.call(WithUsage(stub, rec), WithEmbedUsage(stub, rec, ModeMock)); err != nil {
				t.Fatalf("call: %v", err)
			}
			if len(rec.rows) != 1 {
				t.Fatalf("records = %d, want 1", len(rec.rows))
			}
			persisted := strings.Join(rec.rows, "\n") + logs.String()
			for _, part := range []string{secret, "Jane Roe", "41500", "Acme"} {
				if strings.Contains(persisted, part) {
					t.Fatalf("persisted output holds client text %q: %s", part, persisted)
				}
			}
			var got UsageRecord
			if err := json.Unmarshal([]byte(rec.rows[0]), &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got.FeatureID != "F-TIM-01" || got.Usage != usage {
				t.Fatalf("record = %+v, want feature F-TIM-01 usage %+v", got, usage)
			}
		})
	}
}

// rule: AI-R-005
func TestAI_R_005_UsageRecordHasNoTextFields(t *testing.T) {
	allowed := map[string]reflect.Type{
		"FeatureID": reflect.TypeOf(""),
		"Kind":      reflect.TypeOf(CallKind("")),
		"Model":     reflect.TypeOf(""),
		"Mode":      reflect.TypeOf(Mode("")),
		"Usage":     reflect.TypeOf(Usage{}),
	}
	rt := reflect.TypeOf(UsageRecord{})
	tests := make([]reflect.StructField, 0, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		tests = append(tests, rt.Field(i))
	}
	for _, f := range tests {
		t.Run(f.Name, func(t *testing.T) {
			want, ok := allowed[f.Name]
			if !ok || f.Type != want {
				t.Fatalf("UsageRecord.%s (%s) is not an allowed identifier or count field", f.Name, f.Type)
			}
		})
	}
}

func TestUsageWrapperPassesErrorsAndMode(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantErr error
	}{
		{name: "rate limited", err: fmt.Errorf("upstream: %w", ErrRateLimited), wantErr: ErrRateLimited},
		{name: "timeout", err: ErrTimeout, wantErr: ErrTimeout},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var ledger UsageLedger
			c := WithUsage(&stubClient{err: tc.err}, &ledger)
			if c.Mode() != ModeMock {
				t.Fatalf("Mode() = %q, want mock", c.Mode())
			}
			if _, err := c.Complete(context.Background(), Request{FeatureID: "F-TIM-01"}); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Complete err = %v, want %v", err, tc.wantErr)
			}
			if _, err := c.Stream(context.Background(), Request{FeatureID: "F-TIM-01"}); !errors.Is(err, tc.wantErr) {
				t.Fatalf("Stream err = %v, want %v", err, tc.wantErr)
			}
			if got := ledger.Feature("F-TIM-01"); got.Calls != 0 {
				t.Fatalf("failed call recorded: %+v", got)
			}
		})
	}
}
