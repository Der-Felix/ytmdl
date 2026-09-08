package ytdlp

import (
	"context"
	"sync/atomic"
	"testing"
)

type recordingGate struct{ calls atomic.Int32 }

func (g *recordingGate) Acquire(context.Context) (func(), error) {
	g.calls.Add(1)
	return func() {}, nil
}

func TestEveryYTDLPProcessStartUsesBoundExecutionGate(t *testing.T) {
	gate := &recordingGate{}
	client := New(Options{Binary: "/usr/bin/false"}).WithExecutionGate(gate)
	_, _ = client.Query(context.Background(), "ytsearch1:test")
	_, _ = client.Download(context.Background(), DownloadRequest{
		URL: "https://youtube.com/watch?v=dQw4w9WgXcQ", Dir: t.TempDir(),
	}, nil)
	if got := gate.calls.Load(); got != 2 {
		t.Fatalf("gate acquisitions = %d, want one for query and one for download", got)
	}
}
