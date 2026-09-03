package jobs

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Event types published on the stream.
const (
	EventJobCreated         = "job.created"
	EventJobStatus          = "job.status"
	EventJobProgress        = "job.progress"
	EventJobCompleted       = "job.completed"
	EventJobCancelled       = "job.cancelled"
	EventJobFailed          = "job.failed"
	EventJobPriorityChanged = "job.priority_changed"
	EventJobPaused          = "job.paused"
	EventJobResumed         = "job.resumed"
	EventJobRetried         = "job.retried"
	EventItemStatus         = "job.item"
)

// Subscription sync event types. They travel on the same stream as the job
// events above: a client that already listens for download progress should not
// have to open a second connection to learn that a discography sync is
// running.
const (
	EventSubscriptionSyncStarted   = "subscription.sync.started"
	EventSubscriptionSyncProgress  = "subscription.sync.progress"
	EventSubscriptionSyncCompleted = "subscription.sync.completed"
	EventSubscriptionSyncFailed    = "subscription.sync.failed"
)

// Event is one message on the server sent event stream.
type Event struct {
	Type string    `json:"type"`
	Time time.Time `json:"time"`

	JobID    string   `json:"job_id,omitempty"`
	Status   Status   `json:"status,omitempty"`
	Label    string   `json:"label,omitempty"`
	Priority Priority `json:"priority,omitempty"`
	Paused   *bool    `json:"paused,omitempty"`

	// SubscriptionID is set on the subscription sync events and empty on every
	// job event. Status stays empty on a sync event: a sync does not move
	// through the job state machine.
	SubscriptionID string `json:"subscription_id,omitempty"`

	ItemID     string     `json:"item_id,omitempty"`
	ItemStatus ItemStatus `json:"item_status,omitempty"`
	Track      string     `json:"track,omitempty"`

	Current int `json:"current,omitempty"`
	Total   int `json:"total,omitempty"`

	DownloadPercent float64 `json:"download_percent,omitempty"`
	MatchScore      float64 `json:"match_score,omitempty"`

	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`

	Summary *Summary `json:"summary,omitempty"`
}

// subscriberBuffer is how many events a slow client may fall behind before
// events are dropped for it. Dropping protects the workers: a stalled HTTP
// client must never be able to block a download.
const subscriberBuffer = 64

// Broker fans events out to the connected event stream clients.
type Broker struct {
	mu          sync.RWMutex
	subscribers map[int64]chan Event
	nextID      atomic.Int64
	dropped     atomic.Int64
	logger      *slog.Logger
	closed      bool
}

// NewBroker builds an event broker.
func NewBroker(logger *slog.Logger) *Broker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Broker{subscribers: make(map[int64]chan Event), logger: logger}
}

// Subscribe registers a client. The returned function unsubscribes it and must
// be called when the client goes away; the channel is closed afterwards.
func (b *Broker) Subscribe() (<-chan Event, func()) {
	channel := make(chan Event, subscriberBuffer)
	id := b.nextID.Add(1)

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(channel)
		return channel, func() {}
	}
	b.subscribers[id] = channel
	b.mu.Unlock()

	var once sync.Once
	return channel, func() {
		once.Do(func() {
			b.mu.Lock()
			if ch, ok := b.subscribers[id]; ok {
				delete(b.subscribers, id)
				close(ch)
			}
			b.mu.Unlock()
		})
	}
}

// Publish sends an event to every subscriber. Subscribers that cannot keep up
// lose the event instead of slowing the sender down.
func (b *Broker) Publish(event Event) {
	if b == nil {
		return
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, channel := range b.subscribers {
		select {
		case channel <- event:
		default:
			b.dropped.Add(1)
		}
	}
}

// Subscribers returns the number of connected clients.
func (b *Broker) Subscribers() int {
	if b == nil {
		return 0
	}
	b.mu.RLock()

	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// Dropped returns how many events were dropped because a client was too slow.
func (b *Broker) Dropped() int64 { return b.dropped.Load() }

// Close disconnects every subscriber.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, channel := range b.subscribers {
		delete(b.subscribers, id)
		close(channel)
	}
}
