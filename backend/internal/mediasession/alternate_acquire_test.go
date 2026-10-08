package mediasession

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAlternateAcquireExcludesCheckedAndBusySessions(t *testing.T) {
	pool := executionTestPool()
	first, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer first.ReleaseNeutral()
	excluded := map[string]struct{}{first.SessionID(): {}}
	next, err := pool.TryAcquireExcluding(context.Background(), excluded)
	if err != nil || next == nil || next.SessionID() == first.SessionID() {
		t.Fatalf("alternate acquire: %v", err)
	}
	unavailable, err := pool.TryAcquireExcluding(context.Background(), excluded)
	if err != nil || unavailable != nil {
		t.Fatal("busy alternate was acquired")
	}
	next.ReleaseNeutral()
	excluded[next.SessionID()] = struct{}{}
	unavailable, err = pool.TryAcquireExcluding(context.Background(), excluded)
	if err != nil || unavailable != nil {
		t.Fatal("excluded session reused")
	}
}

func TestAlternateAcquireWaitsForCapacityAndCancels(t *testing.T) {
	pool := executionTestPool()
	first, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer first.ReleaseNeutral()
	excluded := map[string]struct{}{first.SessionID(): {}}
	busy, err := pool.AcquireExcluding(context.Background(), excluded)
	if err != nil || busy == nil {
		t.Fatal("missing alternate")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if lease, err := pool.AcquireExcluding(ctx, excluded); !errors.Is(err, context.DeadlineExceeded) || lease != nil {
		t.Fatal("busy alternate did not respect cancellation")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	type result struct {
		lease *Lease
		err   error
	}
	done := make(chan result, 1)
	go func() { lease, err := pool.AcquireExcluding(ctx2, excluded); done <- result{lease, err} }()
	busy.ReleaseNeutral()
	got := <-done
	if got.err != nil || got.lease == nil || got.lease.SessionID() != busy.SessionID() {
		t.Fatalf("did not wait for correct capacity: %v", got.err)
	}
	got.lease.ReleaseNeutral()
	excluded[busy.SessionID()] = struct{}{}
	if lease, err := pool.AcquireExcluding(context.Background(), excluded); err != nil || lease != nil {
		t.Fatal("exhausted alternatives should return neutrally")
	}
}
