package auth_test

import (
	"context"
	"testing"
	"time"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
)

func newTestService(t *testing.T) *auth.Service {
	t.Helper()
	db := dbtest.Open(t)
	usersRepo := repository.NewUsers(db)
	sessionsRepo := repository.NewSessions(db)
	limiter := auth.NewLimiter(5, 5*time.Minute)
	t.Cleanup(limiter.Close)
	return auth.NewService(usersRepo, sessionsRepo, limiter, nil)
}

func TestAuthServiceSetupAndStatus(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)

	// Initial status should be setup_required = true
	status, err := svc.Status(ctx, "")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.SetupRequired || status.Authenticated {
		t.Fatalf("expected setup_required=true, authenticated=false, got %+v", status)
	}

	// Run First-Run Admin Setup
	setupRes, err := svc.Setup(ctx, auth.SetupRequest{
		Username:    "SysAdmin",
		DisplayName: "System Admin",
		Password:    "super_secret_password_123",
	}, "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if setupRes.User.Username != "sysadmin" || setupRes.User.Role != auth.RoleAdmin {
		t.Fatalf("unexpected setup result user: %+v", setupRes.User)
	}
	if setupRes.SessionToken == "" {
		t.Fatal("expected session token from setup")
	}

	// Status with active session token
	statusAfter, err := svc.Status(ctx, setupRes.SessionToken)
	if err != nil {
		t.Fatalf("status after setup: %v", err)
	}
	if statusAfter.SetupRequired || !statusAfter.Authenticated || statusAfter.User == nil {
		t.Fatalf("expected setup_required=false, authenticated=true, got %+v", statusAfter)
	}
	if statusAfter.User.Username != "sysadmin" {
		t.Fatalf("expected username sysadmin, got %s", statusAfter.User.Username)
	}
}

func TestAuthServiceLoginAndLogout(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)

	// Setup initial admin
	_, err := svc.Setup(ctx, auth.SetupRequest{
		Username:    "admin_user",
		DisplayName: "Admin User",
		Password:    "correct_password_123",
	}, "127.0.0.1", "TestAgent")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Login with correct password (testing case-insensitive username)
	loginRes, err := svc.Login(ctx, auth.LoginRequest{
		Username: "ADMIN_USER",
		Password: "correct_password_123",
	}, "127.0.0.1", "Browser 1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if loginRes.User.Username != "admin_user" || loginRes.SessionToken == "" {
		t.Fatalf("unexpected login result: %+v", loginRes)
	}

	// Verify session
	u, sess, err := svc.VerifySession(ctx, loginRes.SessionToken)
	if err != nil {
		t.Fatalf("verify session: %v", err)
	}
	if u.Username != "admin_user" || sess.UserAgent != "Browser 1" {
		t.Fatalf("unexpected session verify data: u=%+v, sess=%+v", u, sess)
	}

	// Login with wrong password
	_, err = svc.Login(ctx, auth.LoginRequest{
		Username: "admin_user",
		Password: "wrong_password_xyz",
	}, "127.0.0.1", "Browser 2")
	if err == nil || apperr.CodeOf(err) != apperr.CodeInvalidCredentials {
		t.Fatalf("expected CodeInvalidCredentials on wrong password, got %v", err)
	}

	// Login with non-existent user
	_, err = svc.Login(ctx, auth.LoginRequest{
		Username: "nonexistent_user",
		Password: "some_password_123",
	}, "127.0.0.1", "Browser 2")
	if err == nil || apperr.CodeOf(err) != apperr.CodeInvalidCredentials {
		t.Fatalf("expected CodeInvalidCredentials on unknown user, got %v", err)
	}

	// Logout
	if err := svc.Logout(ctx, loginRes.SessionToken); err != nil {
		t.Fatalf("logout: %v", err)
	}

	// Session must now be invalid
	_, _, err = svc.VerifySession(ctx, loginRes.SessionToken)
	if err == nil || apperr.CodeOf(err) != apperr.CodeUnauthenticated {
		t.Fatalf("expected CodeUnauthenticated after logout, got %v", err)
	}
}

func TestAuthServiceChangePasswordAndRevokeOthers(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)

	// Setup initial admin
	setupRes, err := svc.Setup(ctx, auth.SetupRequest{
		Username:    "alice",
		DisplayName: "Alice",
		Password:    "initial_password_123",
	}, "127.0.0.1", "Session 1")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Login second session
	loginRes2, err := svc.Login(ctx, auth.LoginRequest{
		Username: "alice",
		Password: "initial_password_123",
	}, "127.0.0.1", "Session 2")
	if err != nil {
		t.Fatalf("login session 2: %v", err)
	}

	// Inspect current session 1
	_, sess1, err := svc.VerifySession(ctx, setupRes.SessionToken)
	if err != nil {
		t.Fatalf("verify sess1: %v", err)
	}

	// Change password from session 1
	err = svc.ChangePassword(ctx, setupRes.User.ID, sess1.ID, auth.ChangePasswordRequest{
		CurrentPassword: "initial_password_123",
		NewPassword:     "new_super_password_456",
	})
	if err != nil {
		t.Fatalf("change password: %v", err)
	}

	// Session 1 must still be valid
	_, _, err = svc.VerifySession(ctx, setupRes.SessionToken)
	if err != nil {
		t.Fatalf("session 1 should remain valid, got %v", err)
	}

	// Session 2 must be revoked
	_, _, err = svc.VerifySession(ctx, loginRes2.SessionToken)
	if err == nil || apperr.CodeOf(err) != apperr.CodeUnauthenticated {
		t.Fatalf("session 2 should be revoked, got %v", err)
	}

	// Login with old password must fail
	_, err = svc.Login(ctx, auth.LoginRequest{
		Username: "alice",
		Password: "initial_password_123",
	}, "127.0.0.1", "Session 3")
	if err == nil || apperr.CodeOf(err) != apperr.CodeInvalidCredentials {
		t.Fatalf("login with old password should fail, got %v", err)
	}

	// Login with new password must succeed
	loginResNew, err := svc.Login(ctx, auth.LoginRequest{
		Username: "alice",
		Password: "new_super_password_456",
	}, "127.0.0.1", "Session 3")
	if err != nil {
		t.Fatalf("login with new password: %v", err)
	}
	if loginResNew.User.Username != "alice" {
		t.Fatalf("unexpected user on new login: %+v", loginResNew.User)
	}
}

func TestAuthServiceUserCRUDAndDisabledRevocation(t *testing.T) {
	ctx := context.Background()
	svc := newTestService(t)

	// Setup initial admin
	_, err := svc.Setup(ctx, auth.SetupRequest{
		Username:    "admin",
		DisplayName: "Admin",
		Password:    "admin_pass_123",
	}, "127.0.0.1", "Admin Session")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Admin creates a normal user
	created, err := svc.CreateUser(ctx, auth.CreateUserRequest{
		Username:    "Bob_User",
		DisplayName: "Bob",
		Password:    "bob_secret_pw_123",
		Role:        auth.RoleUser,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if created.Username != "bob_user" || created.Role != auth.RoleUser || !created.Enabled {
		t.Fatalf("unexpected created user: %+v", created)
	}

	// Bob logs in
	bobLogin, err := svc.Login(ctx, auth.LoginRequest{
		Username: "bob_user",
		Password: "bob_secret_pw_123",
	}, "192.168.1.50", "Bob Mobile")
	if err != nil {
		t.Fatalf("bob login: %v", err)
	}

	// Verify Bob's session works
	_, _, err = svc.VerifySession(ctx, bobLogin.SessionToken)
	if err != nil {
		t.Fatalf("verify bob session: %v", err)
	}

	// Admin disables Bob
	disabledFlag := false
	_, err = svc.UpdateUser(ctx, created.ID, auth.UpdateUserStatusRequest{
		Enabled: &disabledFlag,
	})
	if err != nil {
		t.Fatalf("disable bob: %v", err)
	}

	// Bob's active session must now be rejected immediately
	_, _, err = svc.VerifySession(ctx, bobLogin.SessionToken)
	if err == nil || apperr.CodeOf(err) != apperr.CodeUnauthenticated {
		t.Fatalf("disabled user session should be rejected with CodeUnauthenticated, got %v", err)
	}

	// Bob login attempts must also be rejected
	_, err = svc.Login(ctx, auth.LoginRequest{
		Username: "bob_user",
		Password: "bob_secret_pw_123",
	}, "192.168.1.50", "Bob Mobile")
	if err == nil || apperr.CodeOf(err) != apperr.CodeUnauthenticated {
		t.Fatalf("disabled user login should be rejected with CodeUnauthenticated, got %v", err)
	}

	// Admin resets Bob's password and re-enables Bob
	enabledFlag := true
	_, err = svc.UpdateUser(ctx, created.ID, auth.UpdateUserStatusRequest{
		Enabled: &enabledFlag,
	})
	if err != nil {
		t.Fatalf("re-enable bob: %v", err)
	}

	if err := svc.ResetPassword(ctx, created.ID, "bob_brand_new_pw_999"); err != nil {
		t.Fatalf("reset password: %v", err)
	}

	// Bob can login with reset password
	bobLoginNew, err := svc.Login(ctx, auth.LoginRequest{
		Username: "bob_user",
		Password: "bob_brand_new_pw_999",
	}, "192.168.1.50", "Bob Desktop")
	if err != nil {
		t.Fatalf("bob new login: %v", err)
	}
	if bobLoginNew.User.Username != "bob_user" {
		t.Fatalf("unexpected user: %+v", bobLoginNew.User)
	}
}
