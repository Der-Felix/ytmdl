package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// flushRecorder records the header set at the moment the response was
// committed. Flushing commits it, so anything the handler sets afterwards
// never reaches the client — which is precisely what these tests guard.
type flushRecorder struct {
	*httptest.ResponseRecorder
	committed http.Header
}

func (f *flushRecorder) Flush() {
	if f.committed == nil {
		f.committed = f.Header().Clone()
	}
	f.ResponseRecorder.Flush()
}

// TestEventsCommitsStreamHeaders pins the header block a browser's EventSource
// depends on. It rejects a stream that does not arrive as text/event-stream, so
// a header set after the first flush is the same as no header at all.
func TestEventsCommitsStreamHeaders(t *testing.T) {
	committed := commitEventStreamHeaders(t)

	want := map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"X-Accel-Buffering": "no",
	}
	for name, expected := range want {
		if got := committed.Get(name); got != expected {
			t.Errorf("committed header %s = %q, want %q", name, got, expected)
		}
	}
}

// commitEventStreamHeaders runs the events handler far enough to commit the
// response and returns the header it committed. The handler only reaches for
// the job manager after that point, so it is left unset and the resulting panic
// is recovered: it marks the end of the part under test.
func commitEventStreamHeaders(t *testing.T) http.Header {
	t.Helper()

	recorder := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	func() {
		defer func() { _ = recover() }()
		(&Handlers{}).Events(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/events", nil))
	}()

	if recorder.committed == nil {
		t.Fatal("the handler never flushed, so no header reached the client")
	}
	return recorder.committed
}
