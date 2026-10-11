package agenttools

import (
	"encoding/json"
	"fmt"
)

// Quote wraps untrusted text (a document passage, an email body, a client message) as data
// for the agent. The text is never spliced into instructions: it travels as a JSON string
// field inside a Quoted envelope marked untrusted, with the citation of where it came from.
//
// rule: ARC-I-038
func Quote(src Citation, text string) Quoted {
	return Quoted{Untrusted: true, Source: src, Text: text}
}

// QuoteResult builds the tool result for quoted untrusted text: the Output is the JSON
// envelope {"untrusted":true,"source":...,"text":...} and the source is listed in Citations.
// Whatever the text says, it stays inside the "text" string; JSON encoding escapes quotes and
// control characters, so the text cannot close the envelope or add fields.
func QuoteResult(quotes ...Quoted) (ToolResult, error) {
	// rule: ARC-I-038
	for i := range quotes {
		quotes[i].Untrusted = true
	}
	out, err := json.Marshal(struct {
		Quotes []Quoted `json:"quotes"`
	}{Quotes: quotesOrEmpty(quotes)})
	if err != nil {
		return ToolResult{}, fmt.Errorf("quote result: %w", err)
	}
	cites := make([]Citation, 0, len(quotes))
	for _, q := range quotes {
		cites = append(cites, q.Source)
	}
	return ToolResult{Output: out, Citations: cites}, nil
}

func quotesOrEmpty(q []Quoted) []Quoted {
	if q == nil {
		return []Quoted{}
	}
	return q
}
