package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/provider"
)

func TestMediaSessions_CRUD(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewMediaSessions(db)

	now := time.Now().UTC().Truncate(time.Microsecond)

	// 1. Create with auto-generated ID
	sess1 := mediasession.Session{
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Primary YouTube Cookie",
		CookieRef:      "managed://vault/yt-cookie-1",
		Enabled:        true,
		HealthStatus:   mediasession.HealthUnknown,
	}
	if err := repo.CreateSession(ctx, &sess1); err != nil {
		t.Fatalf("CreateSession sess1: %v", err)
	}
	if sess1.ID == "" {
		t.Fatal("expected non-empty assigned ID")
	}
	if sess1.HealthStatus != mediasession.HealthUnknown {
		t.Fatalf("expected HealthUnknown, got %s", sess1.HealthStatus)
	}

	// 2. Create with explicit ID
	explicitID := "11111111-2222-3333-4444-555555555555"
	sess2 := mediasession.Session{
		ID:             explicitID,
		ProviderFamily: provider.FamilyYouTube,
		Name:           "Secondary YouTube Cookie",
		CookieRef:      "managed://vault/yt-cookie-2",
		Enabled:        false,
		HealthStatus:   mediasession.HealthHealthy,
	}
	if err := repo.CreateSession(ctx, &sess2); err != nil {
		t.Fatalf("CreateSession sess2: %v", err)
	}
	if sess2.ID != explicitID {
		t.Fatalf("expected ID %s, got %s", explicitID, sess2.ID)
	}

	// 3. GetSession
	fetched, err := repo.GetSession(ctx, sess1.ID)
	if err != nil {
		t.Fatalf("GetSession sess1: %v", err)
	}
	if fetched.Name != "Primary YouTube Cookie" || fetched.CookieRef != "managed://vault/yt-cookie-1" {
		t.Fatalf("unexpected fetched content: %+v", fetched)
	}

	// 4. Get non-existent session -> CodeSessionNotFound
	_, err = repo.GetSession(ctx, "00000000-0000-0000-0000-000000000099")
	if apperr.CodeOf(err) != apperr.CodeSessionNotFound {
		t.Fatalf("expected CodeSessionNotFound, got %v", apperr.CodeOf(err))
	}

	// 5. ListSessions - All
	all, err := repo.ListSessions(ctx, mediasession.Filter{})
	if err != nil {
		t.Fatalf("ListSessions all: %v", err)
	}
	if len(all) < 2 {
		t.Fatalf("expected at least 2 sessions, got %d", len(all))
	}

	// ListSessions - Filter by Enabled = true
	enabledTrue := true
	enabledSessions, err := repo.ListSessions(ctx, mediasession.Filter{Enabled: &enabledTrue})
	if err != nil {
		t.Fatalf("ListSessions enabled=true: %v", err)
	}
	for _, s := range enabledSessions {
		if !s.Enabled {
			t.Fatalf("expected only enabled sessions, got %+v", s)
		}
	}

	// ListSessions - Filter by HealthStatus = Healthy
	healthyStatus := mediasession.HealthHealthy
	healthySessions, err := repo.ListSessions(ctx, mediasession.Filter{HealthStatus: &healthyStatus})
	if err != nil {
		t.Fatalf("ListSessions health=healthy: %v", err)
	}
	for _, s := range healthySessions {
		if s.HealthStatus != mediasession.HealthHealthy {
			t.Fatalf("expected only healthy sessions, got %+v", s)
		}
	}

	// 6. UpdateSessionMetadata
	updated, err := repo.UpdateSessionMetadata(ctx, sess1.ID, "Renamed Primary Cookie", false)
	if err != nil {
		t.Fatalf("UpdateSessionMetadata: %v", err)
	}
	if updated.Name != "Renamed Primary Cookie" || updated.Enabled != false {
		t.Fatalf("unexpected metadata update: %+v", updated)
	}

	// 7. UpdateHealth
	lastFail := now.Add(-5 * time.Minute)
	cooldown := now.Add(15 * time.Minute)
	healthUpdate := mediasession.HealthUpdate{
		HealthStatus:        mediasession.HealthCooldown,
		ConsecutiveFailures: 3,
		LastFailureAt:       &lastFail,
		LastFailureReason:   "HTTP 429: Too Many Requests",
		CooldownUntil:       &cooldown,
	}
	healthUpdated, err := repo.UpdateHealth(ctx, sess1.ID, healthUpdate)
	if err != nil {
		t.Fatalf("UpdateHealth: %v", err)
	}
	if healthUpdated.HealthStatus != mediasession.HealthCooldown {
		t.Fatalf("expected HealthCooldown, got %s", healthUpdated.HealthStatus)
	}
	if healthUpdated.ConsecutiveFailures != 3 {
		t.Fatalf("expected ConsecutiveFailures 3, got %d", healthUpdated.ConsecutiveFailures)
	}
	if healthUpdated.LastFailureReason != "HTTP 429: Too Many Requests" {
		t.Fatalf("expected failure reason, got %s", healthUpdated.LastFailureReason)
	}
	if healthUpdated.CooldownUntil == nil || !healthUpdated.CooldownUntil.Equal(cooldown) {
		t.Fatalf("expected cooldown timestamp %v, got %v", cooldown, healthUpdated.CooldownUntil)
	}

	// 8. DeleteSession
	if err := repo.DeleteSession(ctx, sess1.ID); err != nil {
		t.Fatalf("DeleteSession sess1: %v", err)
	}
	// Verify deleted
	_, err = repo.GetSession(ctx, sess1.ID)
	if apperr.CodeOf(err) != apperr.CodeSessionNotFound {
		t.Fatalf("expected CodeSessionNotFound after deletion, got %v", apperr.CodeOf(err))
	}
	// Deleting non-existent session returns CodeSessionNotFound
	err = repo.DeleteSession(ctx, sess1.ID)
	if apperr.CodeOf(err) != apperr.CodeSessionNotFound {
		t.Fatalf("expected CodeSessionNotFound for repeated delete, got %v", apperr.CodeOf(err))
	}
}

func TestMediaSessions_ValidationAndConstraints(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	repo := NewMediaSessions(db)

	// Application-level validation
	t.Run("rejects empty name", func(t *testing.T) {
		s := mediasession.Session{
			ProviderFamily: provider.FamilyYouTube,
			Name:           "",
			CookieRef:      "managed://vault/ref",
		}
		err := repo.CreateSession(ctx, &s)
		if apperr.CodeOf(err) != apperr.CodeInvalidRequest {
			t.Fatalf("expected CodeInvalidRequest for empty name, got %v", apperr.CodeOf(err))
		}
	})

	t.Run("allows empty cookie_ref for metadata-only session", func(t *testing.T) {
		s := mediasession.Session{
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Metadata Only Session",
			CookieRef:      "",
		}
		err := repo.CreateSession(ctx, &s)
		if err != nil {
			t.Fatalf("expected nil error for metadata-only session, got %v", err)
		}
		if s.ID == "" {
			t.Fatalf("expected non-empty ID")
		}
	})

	t.Run("rejects invalid health status", func(t *testing.T) {
		s := mediasession.Session{
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Session Name",
			CookieRef:      "managed://vault/ref",
			HealthStatus:   "disabled", // Invalid: enabled is separate from health
		}
		err := repo.CreateSession(ctx, &s)
		if apperr.CodeOf(err) != apperr.CodeInvalidRequest {
			t.Fatalf("expected CodeInvalidRequest for health=disabled, got %v", apperr.CodeOf(err))
		}
	})

	// Database-level CHECK constraint violations
	t.Run("db rejects invalid health status via raw SQL", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO media_sessions (provider_family, name, cookie_ref, health_status)
			VALUES ('youtube', 'Bad Health', 'managed://ref', 'disabled')
		`)
		if err == nil {
			t.Fatal("expected CHECK constraint violation for health_status='disabled', got nil")
		}
		if !strings.Contains(err.Error(), "media_sessions_health_check") {
			t.Fatalf("expected check constraint error, got: %v", err)
		}
	})

	t.Run("db rejects negative consecutive failures", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO media_sessions (provider_family, name, cookie_ref, consecutive_failures)
			VALUES ('youtube', 'Negative Failures', 'managed://ref', -1)
		`)
		if err == nil {
			t.Fatal("expected CHECK constraint violation for negative consecutive_failures, got nil")
		}
		if !strings.Contains(err.Error(), "media_sessions_failures_check") {
			t.Fatalf("expected check constraint error, got: %v", err)
		}
	})

	t.Run("updates cookie_ref", func(t *testing.T) {
		s := mediasession.Session{
			ProviderFamily: provider.FamilyYouTube,
			Name:           "Cookie Update Test",
			CookieRef:      "",
		}
		if err := repo.CreateSession(ctx, &s); err != nil {
			t.Fatalf("create: %v", err)
		}
		updated, err := repo.UpdateCookieRef(ctx, s.ID, "managed://cookies/sess_updated")
		if err != nil {
			t.Fatalf("UpdateCookieRef: %v", err)
		}
		if updated.CookieRef != "managed://cookies/sess_updated" {
			t.Errorf("cookie_ref = %s, want managed://cookies/sess_updated", updated.CookieRef)
		}
	})
}
