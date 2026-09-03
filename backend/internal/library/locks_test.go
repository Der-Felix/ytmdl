package library

import (
	"sync"
	"testing"
	"time"
)

func TestKeyedMutexExclusionAndCleanup(t *testing.T) {
	km := NewKeyedMutex()

	unlock1 := km.Lock("track-1")
	if km.Len() != 1 {
		t.Fatalf("expected 1 lock, got %d", km.Len())
	}

	var secondAcquired bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		unlock2 := km.Lock("track-1")
		secondAcquired = true
		unlock2()
	}()

	time.Sleep(20 * time.Millisecond)
	if secondAcquired {
		t.Fatal("second goroutine should be blocked while lock is held")
	}

	unlock1()
	wg.Wait()

	if km.Len() != 0 {
		t.Fatalf("expected 0 locks after release, got %d", km.Len())
	}
}

func TestKeyedMutexDifferentKeysParallel(t *testing.T) {
	km := NewKeyedMutex()

	unlock1 := km.Lock("track-1")
	unlock2 := km.Lock("track-2")

	if km.Len() != 2 {
		t.Fatalf("expected 2 locks, got %d", km.Len())
	}

	unlock1()
	if km.Len() != 1 {
		t.Fatalf("expected 1 lock, got %d", km.Len())
	}

	unlock2()
	if km.Len() != 0 {
		t.Fatalf("expected 0 locks, got %d", km.Len())
	}
}

func TestKeyedMutexTryLock(t *testing.T) {
	km := NewKeyedMutex()

	unlock1, ok := km.TryLock("track-1")
	if !ok {
		t.Fatal("expected TryLock to succeed")
	}

	_, ok2 := km.TryLock("track-1")
	if ok2 {
		t.Fatal("expected second TryLock on same key to fail")
	}

	unlock1()

	unlock3, ok3 := km.TryLock("track-1")
	if !ok3 {
		t.Fatal("expected TryLock to succeed after unlock")
	}
	unlock3()

	if km.Len() != 0 {
		t.Fatalf("expected 0 locks, got %d", km.Len())
	}
}
