package metrics

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// Status page words.
const (
	StatusOK       = "ok"
	StatusDegraded = "degraded"
)

// StatusBody is the JSON body of the status page. It holds check names and outcomes only;
// error text never reaches it, so a failing check cannot leak customer data.
type StatusBody struct {
	Status string            `json:"status"`
	Checks []StatusCheckBody `json:"checks"`
}

// StatusCheckBody is one smoke check on the status page.
type StatusCheckBody struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	TookMS int64  `json:"took_ms"`
}

// NewStatusBody turns a Status into the status page body.
func NewStatusBody(st Status) StatusBody {
	b := StatusBody{Status: StatusOK, Checks: make([]StatusCheckBody, 0, len(st.Checks))}
	if st.Degraded {
		b.Status = StatusDegraded
	}
	for _, c := range st.Checks {
		cs := StatusOK
		if !c.OK {
			cs = StatusDegraded
			b.Status = StatusDegraded
		}
		b.Checks = append(b.Checks, StatusCheckBody{Name: c.Name, Status: cs, TookMS: c.Took.Milliseconds()})
	}
	return b
}

// StatusHandler serves the status page: it runs every smoke check and reports each one.
// It answers 200 with status "degraded" when a check fails, so the page itself stays up.
func StatusHandler(c Checks) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		body := NewStatusBody(c.Run(r.Context()))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		if err := json.NewEncoder(w).Encode(body); err != nil {
			slog.WarnContext(r.Context(), "status page write failed", "err", err)
		}
	})
}
