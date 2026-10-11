package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dagi377/counsel/kernel/ai/fixture"
)

// Fault layer (spec/ai/mocks.md sec. 5): scripted failures, latency and per-chunk delay,
// applied around every protocol handler so each protocol fails in its own error shape.

// timeoutCap bounds a timeout fault when the client sets no deadline, so shutdown never hangs.
const timeoutCap = 2 * time.Minute

// decodeFunc reduces a protocol request body to the protocol-neutral call and its stream flag.
type decodeFunc func(body []byte, r *http.Request) (c call, stream bool, err error)

// errorWriter renders an apiError in a protocol's own error shape.
type errorWriter func(w http.ResponseWriter, e *apiError)

// decodeMessagesCall is the decodeFunc of POST /v1/messages.
func decodeMessagesCall(body []byte, r *http.Request) (call, bool, error) {
	var q messagesRequest
	if err := json.Unmarshal(body, &q); err != nil {
		return call{}, false, fmt.Errorf("decode body: %w", err)
	}
	c, err := q.toCall(r)
	return c, q.Stream, err
}

// faultState remembers which X-Fake-Fault-Once faults already fired.
type faultState struct {
	mu    sync.Mutex
	fired map[string]bool
}

func (f *faultState) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fired = map[string]bool{}
}

// once reports whether a fault keyed by key should fire now, and marks it fired.
func (f *faultState) once(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fired == nil {
		f.fired = map[string]bool{}
	}
	if f.fired[key] {
		return false
	}
	f.fired[key] = true
	return true
}

// plan is what the fault layer does to one call.
type plan struct {
	fault      fixture.Fault
	latency    time.Duration
	chunkDelay time.Duration
}

// peek finds the fixture a call routes to without advancing any run sequence, for its fault
// and delays. A miss is not an error here: the protocol handler reports it.
func (s *server) peek(c call) (fixture.Fixture, string, bool) {
	hash, err := fixture.PromptHash(fixture.Prompt{Feature: c.feature, System: c.system, Messages: c.messages, Tools: c.tools})
	if err != nil || c.feature == "" {
		return fixture.Fixture{}, hash, false
	}
	fxs, err := fixture.LoadFeature(s.fsys, c.feature)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fixture.Fixture{}, hash, false
	}
	fx, ok := pick(fxs, hash, lastUserText(c.messages))
	return fx, hash, ok
}

// planFor merges the fixture's fault and delays with the X-Fake-Fault header (which wins).
// With X-Fake-Fault-Once: true only the first call of a feature and run faults.
func (s *server) planFor(r *http.Request, c call, fx fixture.Fixture) (plan, error) {
	p := plan{fault: fx.Fault, latency: time.Duration(fx.LatencyMS) * time.Millisecond, chunkDelay: time.Duration(fx.ChunkDelayMS) * time.Millisecond}
	if h := strings.TrimSpace(r.Header.Get("X-Fake-Fault")); h != "" {
		f := fixture.Fault(h)
		if !slices.Contains(knownFaults, f) {
			return plan{}, fmt.Errorf("X-Fake-Fault: unknown fault %q", h)
		}
		p.fault = f
	}
	if p.fault != fixture.FaultNone && strings.EqualFold(r.Header.Get("X-Fake-Fault-Once"), "true") {
		if !s.faults.once(c.feature + "\x00" + c.runID + "\x00" + string(p.fault)) {
			p.fault = fixture.FaultNone
		}
	}
	p.latency = time.Duration(float64(p.latency) * s.speed)
	p.chunkDelay = time.Duration(float64(p.chunkDelay) * s.speed)
	return p, nil
}

// knownFaults are the faults X-Fake-Fault accepts (the fixture format's list).
var knownFaults = []fixture.Fault{fixture.FaultRateLimit, fixture.FaultOverloaded, fixture.FaultServerError,
	fixture.FaultDisconnectMidStream, fixture.FaultMalformed, fixture.FaultTimeout}

// statusFaults are the faults answered with an error status before any body.
var statusFaults = map[fixture.Fault]apiError{
	fixture.FaultRateLimit:   {status: http.StatusTooManyRequests, typ: "rate_limit_error", message: "fake: rate limited"},
	fixture.FaultServerError: {status: http.StatusInternalServerError, typ: "api_error", message: "fake: internal server error"},
	fixture.FaultOverloaded:  {status: 529, typ: "overloaded_error", message: "fake: overloaded"},
}

// retryAfterSeconds is the retry-after a rate_limit or overloaded fault advertises.
const retryAfterSeconds = "1"

// instrument wraps a protocol handler with the fault layer and the request journal.
// rule: AI-R-001
func (s *server) instrument(protocol string, decode decodeFunc, writeErr errorWriter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
		if err != nil {
			writeErr(w, &apiError{status: http.StatusBadRequest, typ: "invalid_request_error", message: "read body: " + err.Error()})
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		ent := entry{Protocol: protocol}
		defer func() {
			ent.Status = rec.status
			ent.LatencyMS = time.Since(start).Milliseconds()
			if ent.Usage == nil {
				ent.Usage = usageOf(rec.body.Bytes())
			}
			s.journal.add(ent, body, rec.body.Bytes())
		}()

		c, stream, err := decode(body, r)
		if err != nil {
			next(rec, r) // the protocol handler reports its own decode error
			return
		}
		ent.Feature = c.feature
		fx, hash, _ := s.peek(c)
		ent.Hash = hash
		p, err := s.planFor(r, c, fx)
		if err != nil {
			writeErr(rec, &apiError{status: http.StatusBadRequest, typ: "invalid_request_error", message: err.Error()})
			return
		}
		ent.Fault = string(p.fault)
		if p.latency > 0 && !sleep(r, p.latency) {
			return
		}
		if e, ok := statusFaults[p.fault]; ok {
			if p.fault != fixture.FaultServerError {
				rec.Header().Set("Retry-After", retryAfterSeconds)
			}
			writeErr(rec, &e)
			return
		}
		switch p.fault {
		case fixture.FaultTimeout:
			// Hold until the client gives up; nothing is written.
			ctx := r.Context()
			select {
			case <-ctx.Done():
			case <-time.After(timeoutCap):
			}
			rec.status = http.StatusGatewayTimeout
			return
		case fixture.FaultMalformed:
			// Answer normally into a buffer, then send the first half as a 200: invalid JSON.
			buf := &recorder{ResponseWriter: discard{h: http.Header{}}, status: http.StatusOK}
			s.serve(next, buf, r, p)
			b := buf.body.Bytes()
			rec.Header().Set("Content-Type", "application/json")
			rec.WriteHeader(http.StatusOK)
			_, _ = rec.Write(b[:len(b)/2])
			return
		case fixture.FaultDisconnectMidStream:
			rec.abortAfter = 2 // flushes (stream) or half the body (plain) before the connection drops
			if !stream {
				rec.abortAfter = -1
			}
		}
		s.serve(next, rec, r, p)
		if rec.abortAfter == -1 {
			b := rec.body.Bytes()
			_, _ = rec.ResponseWriter.Write(b[:len(b)/2])
			if fl, ok := rec.ResponseWriter.(http.Flusher); ok {
				fl.Flush()
			}
			ent.Status = rec.status
			panic(http.ErrAbortHandler) // net/http closes the connection
		}
	}
}

// serve runs the protocol handler with the chunk delay applied between stream chunks.
func (s *server) serve(next http.HandlerFunc, rec *recorder, r *http.Request, p plan) {
	rec.chunkDelay, rec.req = p.chunkDelay, r
	next(rec, r)
}

// sleep waits d or until the client goes away; false means the client is gone.
func sleep(r *http.Request, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-r.Context().Done():
		return false
	}
}

// recorder captures status and body for the journal and applies chunk delay and
// mid-stream disconnect. abortAfter > 0 drops the connection after that many flushes; -1
// buffers the whole body so the caller can send part of it.
type recorder struct {
	http.ResponseWriter
	status     int
	wrote      bool
	body       bytes.Buffer
	chunkDelay time.Duration
	req        *http.Request
	flushes    int
	abortAfter int
}

func (r *recorder) WriteHeader(code int) {
	if r.wrote {
		return
	}
	r.wrote, r.status = true, code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	r.body.Write(b)
	if r.abortAfter == -1 {
		return len(b), nil
	}
	return r.ResponseWriter.Write(b)
}

// Flush is called once per stream chunk: it waits chunk_delay_ms between chunks and drops
// the connection for disconnect_mid_stream.
func (r *recorder) Flush() {
	if r.abortAfter == -1 {
		return
	}
	if r.flushes > 0 && r.chunkDelay > 0 && r.req != nil {
		sleep(r.req, r.chunkDelay)
	}
	r.flushes++
	if fl, ok := r.ResponseWriter.(http.Flusher); ok {
		fl.Flush()
	}
	if r.abortAfter > 0 && r.flushes >= r.abortAfter {
		slog.Info("llm fake: dropping connection mid-stream", "after_chunks", r.flushes)
		panic(http.ErrAbortHandler)
	}
}

// discard is a ResponseWriter that keeps nothing (the recorder above keeps the body).
type discard struct{ h http.Header }

func (d discard) Header() http.Header         { return d.h }
func (d discard) Write(b []byte) (int, error) { return len(b), nil }
func (d discard) WriteHeader(int)             {}
