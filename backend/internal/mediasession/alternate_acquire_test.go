package mediasession

import (
	"context"
	"testing"
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
