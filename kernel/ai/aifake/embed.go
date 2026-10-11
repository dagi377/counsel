package aifake

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"strings"

	"github.com/dagi377/counsel/kernel/ai"
	aifixture "github.com/dagi377/counsel/kernel/ai/fixture"
)

// clusterSpread is the share of a cluster member's vector that is its own; the rest is the
// cluster centre. Two members then have cos >= 1 - 2*clusterSpread^2 (0.96 here), and the
// centres of different clusters, like unrelated texts, are independent hash vectors.
const clusterSpread = 0.14

// embedKey is the text normalisation shared by Vector and fixture pins.
func embedKey(text string) string { return strings.ToLower(strings.TrimSpace(text)) }

// EmbedCall is one Embed journal entry. It never holds input text.
type EmbedCall struct {
	FeatureID string
	Inputs    int   // number of inputs
	Status    int   // 200 on success, else the fault's HTTP status (0 for a timeout)
	Fault     Fault // empty on success
}

// EmbedJournal returns the Embed calls answered so far, oldest first.
func (f *Fake) EmbedJournal() []EmbedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]EmbedCall(nil), f.embeds...)
}

func (f *Fake) journalEmbed(c EmbedCall) {
	f.mu.Lock()
	f.embeds = append(f.embeds, c)
	f.mu.Unlock()
}

// pins are a feature's embedding pins: vectors by text, the cluster each text sits in, and
// the faults scripted for inputs matching a fixture's last_user_regex.
type pins struct {
	vectors  map[string][]float32
	clusters map[string]string
	faults   []aifixture.Fixture
}

// fault returns the first scripted fault whose last_user_regex matches any input.
func (p pins) fault(inputs []string) Fault {
	for _, fx := range p.faults {
		for _, in := range inputs {
			if fx.Matches("", in) {
				return Fault(fx.Fault)
			}
		}
	}
	return ""
}

// embedFault is the error for a scripted Embed fault. A timeout also unwraps to
// context.DeadlineExceeded, as a provider call cut off by its deadline would.
func embedFault(fault Fault) error {
	if fault == FaultTimeout {
		return &Error{Fault: fault, err: fmt.Errorf("%w: %w", ai.ErrTimeout, context.DeadlineExceeded)}
	}
	if err := faultError(fault); err != nil {
		return err
	}
	return &Error{Fault: fault, Status: 500, err: ErrServer}
}

// embedPins loads the pinned vectors and clusters from featureID's fixtures. A feature with
// no fixture directory has no pins. A cluster name is one cluster across the feature's
// variants. When two fixtures pin one text, the first by path wins.
func (f *Fake) embedPins(featureID string) (pins, error) {
	p := pins{vectors: map[string][]float32{}, clusters: map[string]string{}}
	fxs, err := aifixture.LoadFeature(f.fsys, featureID)
	if errors.Is(err, fs.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, fmt.Errorf("aifake: embed fixtures %s: %w", featureID, err)
	}
	for _, fx := range fxs {
		if fx.Fault != aifixture.FaultNone && fx.Match.LastUserRegex != "" {
			p.faults = append(p.faults, fx)
		}
		for text, v := range fx.Vectors {
			if _, ok := p.vectors[embedKey(text)]; !ok {
				p.vectors[embedKey(text)] = v
			}
		}
		for name, texts := range fx.Clusters {
			for _, text := range texts {
				if _, ok := p.clusters[embedKey(text)]; !ok {
					p.clusters[embedKey(text)] = name
				}
			}
		}
	}
	return p, nil
}

// pinned returns v zero-padded to dim and normalised.
func pinned(v []float32, dim int) ([]float32, error) {
	if len(v) > dim {
		return nil, fmt.Errorf("aifake: pinned vector has %d values, dim is %d", len(v), dim)
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return nil, fmt.Errorf("aifake: pinned vector is zero")
	}
	norm = math.Sqrt(norm)
	out := make([]float32, dim)
	for i, x := range v {
		out[i] = float32(float64(x) / norm)
	}
	return out, nil
}

// clustered places text near its cluster centre: sqrt(1-s^2)*centre + s*u, where u is the
// text's own hash vector made orthogonal to the centre.
func clustered(text, cluster string, dim int) []float32 {
	c := Vector("\x00cluster\x00"+cluster, dim)
	t := Vector(text, dim)
	var dot float64
	for i := range c {
		dot += float64(c[i]) * float64(t[i])
	}
	u := make([]float64, dim)
	var un float64
	for i := range u {
		u[i] = float64(t[i]) - dot*float64(c[i])
		un += u[i] * u[i]
	}
	un = math.Sqrt(un)
	a := math.Sqrt(1 - clusterSpread*clusterSpread)
	v := make([]float64, dim)
	var norm float64
	for i := range v {
		v[i] = a * float64(c[i])
		if un > 0 {
			v[i] += clusterSpread * u[i] / un
		}
		norm += v[i] * v[i]
	}
	norm = math.Sqrt(norm)
	out := make([]float32, dim)
	for i, x := range v {
		out[i] = float32(x / norm)
	}
	return out
}

// Embed returns one unit vector per input (spec/ai/mocks.md sec. 6). A text pinned in the
// feature's fixtures gets its pinned vector, a text in a fixture cluster sits near the other
// members, and any other text gets the deterministic hash Vector. Texts match after
// lowercase(trim(text)). A fixture with a fault and a last_user_regex matching any input
// fails the call with that fault and no vectors; WithFault wins over fixtures. Usage is
// counted only on success, and every call is journalled without its input text.
func (f *Fake) Embed(ctx context.Context, req ai.EmbedRequest) (ai.EmbedResponse, error) {
	if err := ctx.Err(); err != nil {
		return ai.EmbedResponse{}, err
	}
	if err := req.Validate(); err != nil {
		return ai.EmbedResponse{}, err
	}
	// rule: AI-R-002
	call := EmbedCall{FeatureID: req.FeatureID, Inputs: len(req.Inputs), Status: 200}
	var p pins
	fault, scripted := f.faults[req.FeatureID] // a WithFault option wins over fixtures
	if !scripted {
		var err error
		if p, err = f.embedPins(req.FeatureID); err != nil {
			return ai.EmbedResponse{}, err
		}
		fault = p.fault(req.Inputs)
	}
	if fault != "" {
		err := embedFault(fault)
		var fe *Error
		if errors.As(err, &fe) {
			call.Status, call.Fault = fe.Status, fault
		}
		f.journalEmbed(call)
		return ai.EmbedResponse{}, err
	}
	resp := ai.EmbedResponse{Vectors: make([][]float32, len(req.Inputs))}
	for i, in := range req.Inputs {
		key := embedKey(in)
		switch v, ok := p.vectors[key]; {
		case ok:
			var err error
			if resp.Vectors[i], err = pinned(v, f.dim); err != nil {
				return ai.EmbedResponse{}, err
			}
		case p.clusters[key] != "":
			resp.Vectors[i] = clustered(key, p.clusters[key], f.dim)
		default:
			resp.Vectors[i] = Vector(in, f.dim)
		}
		resp.Usage.InputTokens += (len(in) + 3) / 4
	}
	f.journalEmbed(call)
	return resp, nil
}
