package jobs_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/jobs"
)

func TestMediaCooldownManager_TriggerAndExpire(t *testing.T) {
	mgr := jobs.NewMediaCooldownManager()

	// Initially not in cooldown
	if _, cooling := mgr.Remaining("ytmusic"); cooling {
		t.Fatal("expected not cooling initially")
	}

	// Trigger 50ms cooldown
	mgr.Trigger("ytmusic", 50*time.Millisecond)

	rem, cooling := mgr.Remaining("ytmusic")
	if !cooling || rem <= 0 {
		t.Fatalf("expected cooling, got rem=%v, cooling=%v", rem, cooling)
	}

	// Case-insensitive key check
	if _, coolingUpper := mgr.Remaining("YTMUSIC"); !coolingUpper {
		t.Fatal("expected case-insensitive matching")
	}

	// Wait for expiration
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if err := mgr.Wait(ctx, "ytmusic"); err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	if _, cooling := mgr.Remaining("ytmusic"); cooling {
		t.Fatal("expected cooldown to have expired")
	}
}

func TestMediaCooldownManager_ConcurrentWorkersSync(t *testing.T) {
	mgr := jobs.NewMediaCooldownManager()
	mgr.Trigger("youtube", 40*time.Millisecond)

	var wg sync.WaitGroup
	workers := 5
	errs := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			if err := mgr.Wait(ctx, "youtube"); err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("worker Wait error: %v", err)
	}
}
