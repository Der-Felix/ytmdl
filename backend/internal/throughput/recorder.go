// Package throughput counts download pipeline events so that an operator can
// read the acquisitions, provider requests, rate limits, cooldown time and
// failure reasons of every hour separately.
//
// Counter names are fixed identifiers built from provider labels, operations
// and error codes. They never carry URLs, paths, cookies, titles or any other
// value taken from a request or a provider response.
package throughput

import (
	"sort"
	"sync"
	"time"
)

// Recorder collects counters and gauges between two drains. All methods are
// safe for concurrent use, and a nil *Recorder silently ignores every call so
// that components can be wired without it.
type Recorder struct {
	mu      sync.Mutex
	started time.Time
	counts  map[string]uint64
	gauges  map[string]gauge
	now     func() time.Time
}

type gauge struct {
	last int64
	max  int64
}

// Window is everything recorded between two drains.
type Window struct {
	Start  time.Time
	End    time.Time
	Counts map[string]uint64
	// GaugeLast and GaugeMax hold the final and the largest value a gauge
	// reported during the window.
	GaugeLast map[string]int64
	GaugeMax  map[string]int64
}

// New returns an empty recorder whose first window starts now.
func New() *Recorder {
	return newRecorder(time.Now)
}

func newRecorder(now func() time.Time) *Recorder {
	return &Recorder{
		started: now().UTC(),
		counts:  make(map[string]uint64),
		gauges:  make(map[string]gauge),
		now:     now,
	}
}

// Inc adds one to a counter.
func (r *Recorder) Inc(name string) { r.Add(name, 1) }

// Add adds n to a counter.
func (r *Recorder) Add(name string, n uint64) {
	if r == nil || name == "" || n == 0 {
		return
	}
	r.mu.Lock()
	r.counts[name] += n
	r.mu.Unlock()
}

// AddDuration adds a duration to a millisecond counter.
func (r *Recorder) AddDuration(name string, d time.Duration) {
	if d <= 0 {
		return
	}
	r.Add(name, uint64(d.Milliseconds()))
}

// Gauge reports the current value of a level such as the number of runnable
// items. The window keeps its last and its largest value.
func (r *Recorder) Gauge(name string, value int64) {
	if r == nil || name == "" {
		return
	}
	r.mu.Lock()
	g, seen := r.gauges[name]
	g.last = value
	if !seen || value > g.max {
		g.max = value
	}
	r.gauges[name] = g
	r.mu.Unlock()
}

// Drain returns the current window and starts the next one.
func (r *Recorder) Drain() Window {
	if r == nil {
		return Window{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	end := r.now().UTC()
	w := Window{
		Start:     r.started,
		End:       end,
		Counts:    r.counts,
		GaugeLast: make(map[string]int64, len(r.gauges)),
		GaugeMax:  make(map[string]int64, len(r.gauges)),
	}
	for name, g := range r.gauges {
		w.GaugeLast[name] = g.last
		w.GaugeMax[name] = g.max
	}
	r.counts = make(map[string]uint64)
	// Gauges describe a level rather than events: the next window starts from
	// the last known value instead of from zero.
	next := make(map[string]gauge, len(r.gauges))
	for name, g := range r.gauges {
		next[name] = gauge{last: g.last, max: g.last}
	}
	r.gauges = next
	r.started = end
	return w
}

// Names returns the counter names of a window in a stable order.
func (w Window) Names() []string {
	names := make([]string, 0, len(w.Counts))
	for name := range w.Counts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
