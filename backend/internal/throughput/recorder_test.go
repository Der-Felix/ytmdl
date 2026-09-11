package throughput

import (
	"sync"
	"testing"
	"time"
)

func TestRecorderDrainSeparatesWindows(t *testing.T) {
	clock := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	r := newRecorder(func() time.Time { return clock })

	r.Inc("items.completed")
	r.Add("items.completed", 2)
	r.AddDuration("cooldown.youtube.ms", 1500*time.Millisecond)
	r.Gauge("items.ready", 7)
	r.Gauge("items.ready", 3)

	clock = clock.Add(time.Hour)
	first := r.Drain()
	if first.Counts["items.completed"] != 3 || first.Counts["cooldown.youtube.ms"] != 1500 {
		t.Fatalf("unexpected counts: %v", first.Counts)
	}
	if first.GaugeLast["items.ready"] != 3 || first.GaugeMax["items.ready"] != 7 {
		t.Fatalf("unexpected gauges: last=%v max=%v", first.GaugeLast, first.GaugeMax)
	}
	if !first.Start.Before(first.End) || first.End.Sub(first.Start) != time.Hour {
		t.Fatalf("window bounds %v..%v", first.Start, first.End)
	}

	clock = clock.Add(time.Hour)
	second := r.Drain()
	if len(second.Counts) != 0 {
		t.Fatalf("counters leaked into the next window: %v", second.Counts)
	}
	// A level carries over: nothing was reported, so it is still 3.
	if second.GaugeLast["items.ready"] != 3 || second.GaugeMax["items.ready"] != 3 {
		t.Fatalf("gauge did not carry over: %v %v", second.GaugeLast, second.GaugeMax)
	}
	if !second.Start.Equal(first.End) {
		t.Fatal("windows are not contiguous")
	}
}

func TestNilRecorderIgnoresCalls(t *testing.T) {
	var r *Recorder
	r.Inc("x")
	r.AddDuration("y", time.Second)
	r.Gauge("z", 1)
	if w := r.Drain(); w.Counts != nil {
		t.Fatalf("nil recorder produced a window: %+v", w)
	}
}

func TestRecorderConcurrentUse(t *testing.T) {
	r := New()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				r.Inc("n")
				r.Gauge("g", int64(j))
			}
		}()
	}
	wg.Wait()
	if got := r.Drain().Counts["n"]; got != 16000 {
		t.Fatalf("count = %d, want 16000", got)
	}
}
