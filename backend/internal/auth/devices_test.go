package auth_test

import (
	"context"
	"sync"
	"testing"
	"ytdm/backend/internal/auth"
)

func TestDeviceGrantConcurrentExchangeIssuesOnlyOneSession(t *testing.T) {
	svc := newTestService(t)
	ctx := t.Context()
	approver, err := svc.Setup(ctx, auth.SetupRequest{Username: "concurrent_device", Password: "fixture_password_123"}, "127.0.0.1", "Browser")
	if err != nil {
		t.Fatal(err)
	}
	u, session, err := svc.VerifySession(ctx, approver.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	code, err := svc.StartDevice("Apple TV", "127.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmDevice(code.UserCode, u, session); err != nil {
		t.Fatal(err)
	}
	results := make(chan *auth.AuthResult, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			result, state, err := svc.PollDevice(ctx, code.DeviceCode, "127.0.0.2")
			if err == nil && state == "authorized" && result != nil {
				results <- result
			}
		})
	}
	workers.Wait()
	close(results)
	if len(results) != 1 {
		t.Fatalf("session exchanges: %d, want 1", len(results))
	}
	for result := range results {
		if _, _, err := svc.VerifySession(ctx, result.SessionToken); err != nil {
			t.Fatal("issued session is unusable")
		}
	}
}

func TestDeviceSessionIsIndependentAndRevocable(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	approver, err := svc.Setup(ctx, auth.SetupRequest{Username: "device_user", Password: "fixture_password_123"}, "127.0.0.1", "Browser")
	if err != nil {
		t.Fatal(err)
	}
	u, s, err := svc.VerifySession(ctx, approver.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	code, err := svc.StartDevice("Apple TV", "127.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmDevice(code.UserCode, u, s); err != nil {
		t.Fatal(err)
	}
	result, state, err := svc.PollDevice(ctx, code.DeviceCode, "127.0.0.2")
	if err != nil || state != "authorized" || result == nil {
		t.Fatal("device exchange failed")
	}
	if result.SessionToken == approver.SessionToken {
		t.Fatal("approver session reused")
	}
	_, deviceSession, err := svc.VerifySession(ctx, result.SessionToken)
	if err != nil || deviceSession.ID == s.ID || deviceSession.UserAgent != "YTMDL Apple · Apple TV" {
		t.Fatal("invalid independent device session")
	}
	if _, _, err := svc.PollDevice(ctx, code.DeviceCode, "127.0.0.2"); err == nil {
		t.Fatal("device code replay accepted")
	}
	if err := svc.RevokeSession(ctx, u.ID, deviceSession.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.VerifySession(ctx, result.SessionToken); err == nil {
		t.Fatal("revoked device session accepted")
	}
	if _, _, err := svc.VerifySession(ctx, approver.SessionToken); err != nil {
		t.Fatal("browser session was revoked")
	}
}

func TestDeviceGrantDiesWithApproverSession(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	approver, err := svc.Setup(ctx, auth.SetupRequest{Username: "device_user", Password: "fixture_password_123"}, "127.0.0.1", "Browser")
	if err != nil {
		t.Fatal(err)
	}
	u, s, err := svc.VerifySession(ctx, approver.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	code, err := svc.StartDevice("Apple TV", "127.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmDevice(code.UserCode, u, s); err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(ctx, approver.SessionToken); err != nil {
		t.Fatal(err)
	}
	if result, _, err := svc.PollDevice(ctx, code.DeviceCode, "127.0.0.2"); err == nil || result != nil {
		t.Fatal("revoked approver authorized device")
	}
}
