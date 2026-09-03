package repository

import (
	"context"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/auth"
)

func TestSessionsCreateGetAndCascade(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	usersRepo := NewUsers(db)
	sessionsRepo := NewSessions(db)

	u := auth.User{
		ID:           "u-sess-1",
		Username:     "session_user",
		DisplayName:  "Session User",
		PasswordHash: "hash",
		Role:         auth.RoleUser,
		Enabled:      true,
	}
	if err := usersRepo.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	now := time.Now().UTC()
	sess := auth.Session{
		ID:         "sess-1",
		UserID:     u.ID,
		TokenHash:  "sha256_hash_1",
		UserAgent:  "Mozilla/5.0 (Test)",
		IPAddress:  "127.0.0.1",
		CreatedAt:  now,
		ExpiresAt:  now.Add(30 * 24 * time.Hour),
		LastSeenAt: now,
	}
	if err := sessionsRepo.Create(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// Lookup by token hash
	byHash, err := sessionsRepo.GetByTokenHash(ctx, "sha256_hash_1")
	if err != nil {
		t.Fatalf("get by token hash: %v", err)
	}
	if byHash.ID != sess.ID || byHash.UserID != u.ID || byHash.UserAgent != sess.UserAgent {
		t.Fatalf("unexpected session data: %+v", byHash)
	}

	// Lookup by ID
	byID, err := sessionsRepo.GetByID(ctx, "sess-1")
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if byID.TokenHash != sess.TokenHash {
		t.Fatalf("expected token hash %q, got %q", sess.TokenHash, byID.TokenHash)
	}

	// Non-existent token hash
	_, err = sessionsRepo.GetByTokenHash(ctx, "nonexistent_hash")
	if err == nil || apperr.CodeOf(err) != apperr.CodeSessionNotFound {
		t.Fatalf("expected CodeSessionNotFound, got %v", err)
	}

	// Deleting the user must cascade to sessions
	if err := usersRepo.Delete(ctx, u.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	_, err = sessionsRepo.GetByID(ctx, "sess-1")
	if err == nil || apperr.CodeOf(err) != apperr.CodeSessionNotFound {
		t.Fatalf("expected session to be deleted by cascade, got %v", err)
	}
}

func TestSessionsListTouchAndRevokeOthers(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	usersRepo := NewUsers(db)
	sessionsRepo := NewSessions(db)

	u := auth.User{
		ID:           "u-sess-2",
		Username:     "session_user_2",
		DisplayName:  "Session User 2",
		PasswordHash: "hash",
		Role:         auth.RoleUser,
		Enabled:      true,
	}
	if err := usersRepo.Create(ctx, u); err != nil {
		t.Fatalf("create user: %v", err)
	}

	now := time.Now().UTC()
	sess1 := auth.Session{
		ID:         "sess-device-1",
		UserID:     u.ID,
		TokenHash:  "hash_dev_1",
		UserAgent:  "Firefox Desktop",
		IPAddress:  "192.168.1.10",
		CreatedAt:  now.Add(-2 * time.Hour),
		ExpiresAt:  now.Add(30 * 24 * time.Hour),
		LastSeenAt: now.Add(-2 * time.Hour),
	}
	sess2 := auth.Session{
		ID:         "sess-device-2",
		UserID:     u.ID,
		TokenHash:  "hash_dev_2",
		UserAgent:  "Mobile Safari",
		IPAddress:  "10.0.0.5",
		CreatedAt:  now.Add(-1 * time.Hour),
		ExpiresAt:  now.Add(30 * 24 * time.Hour),
		LastSeenAt: now.Add(-1 * time.Hour),
	}
	if err := sessionsRepo.Create(ctx, sess1); err != nil {
		t.Fatalf("create sess1: %v", err)
	}
	if err := sessionsRepo.Create(ctx, sess2); err != nil {
		t.Fatalf("create sess2: %v", err)
	}

	// Touch sess1
	later := now.Add(10 * time.Minute)
	if err := sessionsRepo.Touch(ctx, sess1.ID, later, "192.168.1.20"); err != nil {
		t.Fatalf("touch session: %v", err)
	}

	// List sessions - sess1 should now be first because it was touched more recently
	list, err := sessionsRepo.ListByUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(list))
	}
	if list[0].ID != sess1.ID {
		t.Fatalf("expected sess1 to be first, got %s", list[0].ID)
	}
	if list[0].IPAddress != "192.168.1.20" {
		t.Fatalf("expected touched IP 192.168.1.20, got %s", list[0].IPAddress)
	}

	// Revoke other sessions except sess1
	if err := sessionsRepo.DeleteByUser(ctx, u.ID, sess1.ID); err != nil {
		t.Fatalf("revoke others: %v", err)
	}

	remaining, err := sessionsRepo.ListByUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("list after revoke others: %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != sess1.ID {
		t.Fatalf("expected only sess1 to remain, got %+v", remaining)
	}

	// Delete expired sessions test
	expired := auth.Session{
		ID:         "sess-expired",
		UserID:     u.ID,
		TokenHash:  "hash_expired",
		UserAgent:  "Old Browser",
		CreatedAt:  now.Add(-40 * 24 * time.Hour),
		ExpiresAt:  now.Add(-10 * 24 * time.Hour),
		LastSeenAt: now.Add(-10 * 24 * time.Hour),
	}
	if err := sessionsRepo.Create(ctx, expired); err != nil {
		t.Fatalf("create expired session: %v", err)
	}
	if err := sessionsRepo.DeleteExpired(ctx, now); err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	_, err = sessionsRepo.GetByID(ctx, "sess-expired")
	if err == nil || apperr.CodeOf(err) != apperr.CodeSessionNotFound {
		t.Fatalf("expected expired session to be deleted, got %v", err)
	}
}
