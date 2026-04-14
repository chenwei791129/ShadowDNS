package metrics

import (
	"strconv"
	"time"

	"github.com/miekg/dns"
)

// RcodeName returns the human-readable Prometheus label for a DNS rcode.
func RcodeName(rcode int) string {
	if name, ok := dns.RcodeToString[rcode]; ok {
		return name
	}
	return strconv.Itoa(rcode)
}

// ResponseWriter wraps a dns.ResponseWriter and records response metrics
// (rcode counter and request duration histogram) when WriteMsg is called.
// It delegates all other methods to the underlying writer unchanged.
type ResponseWriter struct {
	dns.ResponseWriter
	metrics *Metrics
	view    string
	start   time.Time
}

// NewResponseWriter wraps inner with metrics instrumentation. The view label
// and start time are captured at query entry and carried through to WriteMsg.
func NewResponseWriter(inner dns.ResponseWriter, m *Metrics, view string, start time.Time) *ResponseWriter {
	return &ResponseWriter{
		ResponseWriter: inner,
		metrics:        m,
		view:           view,
		start:          start,
	}
}

// SetView updates the view label used by subsequent WriteMsg calls.
// This allows the caller to create the wrapper early (with a default view)
// and refine it once the actual view is resolved.
func (w *ResponseWriter) SetView(view string) {
	w.view = view
}

// WriteMsg records response metrics and then delegates to the underlying writer.
func (w *ResponseWriter) WriteMsg(msg *dns.Msg) error {
	w.metrics.RecordResponse(RcodeName(msg.Rcode), w.view)
	w.metrics.ObserveDuration(w.view, time.Since(w.start))
	return w.ResponseWriter.WriteMsg(msg)
}
