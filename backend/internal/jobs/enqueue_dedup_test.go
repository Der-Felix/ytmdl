package jobs

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider"
)

type atomicEnqueueStore struct {
	Store
	mu   sync.Mutex
	jobs []Job
}

func (s *atomicEnqueueStore) HasNonTerminalJob(_ context.Context, jobType Type, targetID string) (bool, error) {
	// Widen the old check/create race so the regression fails reliably if the
	// Manager admission lock is removed.
	time.Sleep(5 * time.Millisecond)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, job := range s.jobs {
		if job.Type == jobType && job.TargetID == targetID && !job.Status.Terminal() {
			return true, nil
		}
	}
	return false, nil
}

func (s *atomicEnqueueStore) Create(_ context.Context, job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *job
	copy.ID = "created"
	job.ID = copy.ID
	s.jobs = append(s.jobs, copy)
	return nil
}

type enqueueMetadata struct{}

func (enqueueMetadata) Name() string { return "ytmusic" }
func (enqueueMetadata) SearchArtists(context.Context, string) ([]music.Artist, error) {
	return nil, nil
}
func (enqueueMetadata) GetArtist(context.Context, string) (*music.Artist, error) { return nil, nil }
func (enqueueMetadata) GetDiscography(context.Context, string) ([]music.Release, error) {
	return nil, nil
}
func (enqueueMetadata) GetRelease(context.Context, string) (*music.Release, error) { return nil, nil }
func (enqueueMetadata) GetReleaseTracks(context.Context, string) ([]music.Track, error) {
	return nil, nil
}

func TestConcurrentReleaseEnqueueCreatesOneNonTerminalJob(t *testing.T) {
	store := &atomicEnqueueStore{}
	registry := provider.NewRegistry()
	registry.RegisterMetadata(enqueueMetadata{})
	m := &Manager{
		store: store, registry: registry, broker: NewBroker(nil),
		logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
		resolveQueue: make(chan string, 64), wake: make(chan struct{}, 1),
	}
	m.accepting.Store(true)

	const callers = 32
	var wg sync.WaitGroup
	results := make(chan bool, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			created, err := m.EnqueueReleaseWithPriority(context.Background(), "ytmusic", "same-release", "label", PriorityNormal)
			if err != nil {
				t.Errorf("enqueue: %v", err)
			}
			results <- created
		}()
	}
	wg.Wait()
	close(results)
	created := 0
	for result := range results {
		if result {
			created++
		}
	}
	if created != 1 || len(store.jobs) != 1 {
		t.Fatalf("created responses/jobs = %d/%d, want 1/1", created, len(store.jobs))
	}
	if !store.jobs[0].Options.SkipExisting {
		t.Fatal("subscription enqueue lost skip-existing semantics")
	}
}
