package jobs

import (
	"context"
	"strings"
	"sync"
	"time"
)

const (
	defaultCooldown = 60 * time.Second
	maxCooldown     = 5 * time.Minute
)

// CanonicalCooldownKey maps provider names to their platform family key.
// Both "youtube" and "ytmusic" map to "youtube", ensuring a single source of
// truth for YouTube platform family cooldowns.
func CanonicalCooldownKey(prov string) string {
	cleaned := strings.ToLower(strings.TrimSpace(prov))
	switch cleaned {
	case "youtube", "ytmusic", "youtube-family":
		return "youtube"
	default:
		return cleaned
	}
}

// MediaCooldownManager coordinates shared rate-limit cooldowns across download workers.
type MediaCooldownManager struct {
	mu        sync.RWMutex
	cooldowns map[string]time.Time
}

// NewMediaCooldownManager creates a MediaCooldownManager.
func NewMediaCooldownManager() *MediaCooldownManager {
	return &MediaCooldownManager{
		cooldowns: make(map[string]time.Time),
	}
}

// Trigger sets a global rate limit cooldown for provider.
func (m *MediaCooldownManager) Trigger(provider string, duration time.Duration) time.Duration {
	key := CanonicalCooldownKey(provider)
	if key == "" {
		return 0
	}

	if duration <= 0 {
		duration = defaultCooldown
	} else if duration > maxCooldown {
		duration = maxCooldown
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	expiry := time.Now().Add(duration)
	if current, exists := m.cooldowns[key]; !exists || expiry.After(current) {
		m.cooldowns[key] = expiry
	}

	return duration
}

// Remaining returns how long provider remains in cooldown, and whether it is cooling down.
func (m *MediaCooldownManager) Remaining(provider string) (time.Duration, bool) {
	key := CanonicalCooldownKey(provider)
	if key == "" {
		return 0, false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	expiry, exists := m.cooldowns[key]
	if !exists {
		return 0, false
	}

	rem := time.Until(expiry)
	if rem <= 0 {
		return 0, false
	}

	return rem, true
}

// Wait blocks until the cooldown for provider expires or ctx is cancelled.
func (m *MediaCooldownManager) Wait(ctx context.Context, provider string) error {
	rem, cooling := m.Remaining(provider)
	if !cooling {
		return nil
	}

	timer := time.NewTimer(rem)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Clear removes active cooldowns (useful for tests and manual resets).
func (m *MediaCooldownManager) Clear(provider string) {
	key := CanonicalCooldownKey(provider)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cooldowns, key)
}
