package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
)

// UsersRepository is the interface for user persistence.
type UsersRepository interface {
	SetupFirstAdmin(ctx context.Context, u User) error
	Create(ctx context.Context, u User) error
	GetByID(ctx context.Context, id string) (*User, error)
	GetByUsername(ctx context.Context, username string) (*User, error)
	List(ctx context.Context, limit, offset int) ([]User, error)
	Count(ctx context.Context) (int, error)
	CountActiveAdmins(ctx context.Context) (int, error)
	UpdateProfile(ctx context.Context, id, displayName string) error
	UpdatePassword(ctx context.Context, id, passwordHash string) error
	UpdateStatus(ctx context.Context, id string, enabled bool, role Role) error
	Delete(ctx context.Context, id string) error
	UpdateLastLogin(ctx context.Context, id string, t time.Time) error
}

// SessionsRepository is the interface for session persistence.
type SessionsRepository interface {
	Create(ctx context.Context, s Session) error
	GetByTokenHash(ctx context.Context, tokenHash string) (*Session, error)
	GetByID(ctx context.Context, id string) (*Session, error)
	ListByUser(ctx context.Context, userID string) ([]Session, error)
	Touch(ctx context.Context, id string, lastSeenAt time.Time, ipAddress string) error
	Delete(ctx context.Context, id string) error
	DeleteByUser(ctx context.Context, userID string, exceptSessionID string) error
	DeleteExpired(ctx context.Context, now time.Time) error
}

// Service manages authentication, sessions and user accounts.
type Service struct {
	users    UsersRepository
	sessions SessionsRepository
	limiter  *Limiter
	logger   *slog.Logger

	sessionDuration  time.Duration
	inactivityPeriod time.Duration
}

// NewService creates a new authentication service.
func NewService(users UsersRepository, sessions SessionsRepository, limiter *Limiter, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if limiter == nil {
		limiter = NewLimiter(5, 5*time.Minute)
	}
	return &Service{
		users:            users,
		sessions:         sessions,
		limiter:          limiter,
		logger:           logger,
		sessionDuration:  30 * 24 * time.Hour,
		inactivityPeriod: 7 * 24 * time.Hour,
	}
}

// AuthStatus represents the current system authentication state.
type AuthStatus struct {
	SetupRequired bool         `json:"setup_required"`
	Authenticated bool         `json:"authenticated"`
	User          *UserSummary `json:"user,omitempty"`
}

// SetupRequest holds credentials for the initial administrator.
type SetupRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

// LoginRequest holds credentials for user login.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AuthResult contains the authenticated user summary and session token.
type AuthResult struct {
	User         UserSummary `json:"user"`
	SessionToken string      `json:"-"`
	ExpiresAt    time.Time   `json:"expires_at"`
}

// ChangePasswordRequest holds current and new passwords.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// UpdateProfileRequest holds updated user profile data.
type UpdateProfileRequest struct {
	DisplayName string `json:"display_name"`
}

// CreateUserRequest holds data to create a new user account.
type CreateUserRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	Role        Role   `json:"role"`
}

// UpdateUserStatusRequest holds administrative updates to a user.
type UpdateUserStatusRequest struct {
	DisplayName *string `json:"display_name"`
	Role        *Role   `json:"role"`
	Enabled     *bool   `json:"enabled"`
}

func newUUID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

// Status reports whether initial setup is needed and the caller's session state.
func (s *Service) Status(ctx context.Context, rawSessionToken string) (*AuthStatus, error) {
	count, err := s.users.Count(ctx)
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return &AuthStatus{
			SetupRequired: true,
			Authenticated: false,
		}, nil
	}

	if rawSessionToken == "" {
		return &AuthStatus{
			SetupRequired: false,
			Authenticated: false,
		}, nil
	}

	user, _, err := s.VerifySession(ctx, rawSessionToken)
	if err != nil {
		return &AuthStatus{
			SetupRequired: false,
			Authenticated: false,
		}, nil
	}

	summary := user.Summary()
	return &AuthStatus{
		SetupRequired: false,
		Authenticated: true,
		User:          &summary,
	}, nil
}

// Setup creates the first administrator user account and an initial session.
func (s *Service) Setup(ctx context.Context, req SetupRequest, ip, userAgent string) (*AuthResult, error) {
	normUsername := NormalizeUsername(req.Username)
	if err := ValidateUsername(normUsername); err != nil {
		return nil, err
	}
	if err := ValidatePassword(req.Password); err != nil {
		return nil, err
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	admin := User{
		ID:           newUUID(),
		Username:     normUsername,
		DisplayName:  strings.TrimSpace(req.DisplayName),
		PasswordHash: hash,
		Role:         RoleAdmin,
		Enabled:      true,
	}
	if admin.DisplayName == "" {
		admin.DisplayName = "Administrator"
	}

	if err := s.users.SetupFirstAdmin(ctx, admin); err != nil {
		return nil, err
	}

	s.logger.Info("initial administrator account created",
		"user_id", admin.ID,
		"username", admin.Username,
	)

	// Create session for the new admin
	return s.createSession(ctx, admin, ip, userAgent)
}

// Login authenticates a user by username and password. Timing-leak protection
// ensures dummy verification is run even when the username does not exist.
func (s *Service) Login(ctx context.Context, req LoginRequest, ip, userAgent string) (*AuthResult, error) {
	normUsername := NormalizeUsername(req.Username)
	rateKey := ip + ":" + normUsername
	ipRateKey := ip

	if allowed, retryAfter := s.limiter.Allow(rateKey); !allowed {
		s.logger.Warn("login rate limited for user", "ip", ip, "username", normUsername, "retry_after", retryAfter)
		return nil, apperr.Newf(apperr.CodeRateLimited, "Zu viele fehlgeschlagene Login-Versuche. Bitte warte %d Sekunden.", int(retryAfter.Seconds()+1))
	}
	if allowed, retryAfter := s.limiter.Allow(ipRateKey); !allowed {
		s.logger.Warn("login rate limited for IP", "ip", ip, "retry_after", retryAfter)
		return nil, apperr.Newf(apperr.CodeRateLimited, "Zu viele fehlgeschlagene Login-Versuche von dieser IP. Bitte warte %d Sekunden.", int(retryAfter.Seconds()+1))
	}

	user, err := s.users.GetByUsername(ctx, normUsername)
	if err != nil {
		if apperr.CodeOf(err) == apperr.CodeUserNotFound {
			// Timing leak protection: run dummy verification
			DummyVerify(req.Password)
			s.limiter.RecordFailure(rateKey)
			s.limiter.RecordFailure(ipRateKey)
			s.logger.Warn("login failed: unknown user", "ip", ip, "username", normUsername)
			return nil, apperr.New(apperr.CodeInvalidCredentials, "Benutzername oder Passwort ist falsch.")
		}
		return nil, err
	}

	match, verifyErr := VerifyPassword(req.Password, user.PasswordHash)
	if verifyErr != nil || !match {
		s.limiter.RecordFailure(rateKey)
		s.limiter.RecordFailure(ipRateKey)
		s.logger.Warn("login failed: invalid password", "ip", ip, "username", normUsername, "user_id", user.ID)
		return nil, apperr.New(apperr.CodeInvalidCredentials, "Benutzername oder Passwort ist falsch.")
	}

	if !user.Enabled {
		s.logger.Warn("login rejected: user disabled", "ip", ip, "username", normUsername, "user_id", user.ID)
		return nil, apperr.New(apperr.CodeUnauthenticated, "Dieses Benutzerkonto ist deaktiviert.")
	}

	// Login successful: clear rate limits
	s.limiter.RecordSuccess(rateKey)
	s.limiter.RecordSuccess(ipRateKey)

	now := time.Now().UTC()
	_ = s.users.UpdateLastLogin(ctx, user.ID, now)
	user.LastLoginAt = &now

	s.logger.Info("user logged in successfully", "user_id", user.ID, "username", user.Username, "ip", ip)
	return s.createSession(ctx, *user, ip, userAgent)
}

func (s *Service) createSession(ctx context.Context, user User, ip, userAgent string) (*AuthResult, error) {
	rawToken, tokenHash, err := GenerateSessionToken()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	expiresAt := now.Add(s.sessionDuration)

	sess := Session{
		ID:         newUUID(),
		UserID:     user.ID,
		TokenHash:  tokenHash,
		UserAgent:  userAgent,
		IPAddress:  ip,
		CreatedAt:  now,
		ExpiresAt:  expiresAt,
		LastSeenAt: now,
	}

	if err := s.sessions.Create(ctx, sess); err != nil {
		return nil, err
	}

	return &AuthResult{
		User:         user.Summary(),
		SessionToken: rawToken,
		ExpiresAt:    expiresAt,
	}, nil
}

// Logout invalidates a session token immediately.
func (s *Service) Logout(ctx context.Context, rawSessionToken string) error {
	if rawSessionToken == "" {
		return nil
	}
	tokenHash := HashToken(rawSessionToken)
	sess, err := s.sessions.GetByTokenHash(ctx, tokenHash)
	if err != nil {
		if apperr.CodeOf(err) == apperr.CodeSessionNotFound {
			return nil
		}
		return err
	}
	return s.sessions.Delete(ctx, sess.ID)
}

// VerifySession validates a raw session token, checks expiration, inactivity and
// user status, and returns the User and Session.
func (s *Service) VerifySession(ctx context.Context, rawSessionToken string) (*User, *Session, error) {
	if rawSessionToken == "" {
		return nil, nil, apperr.New(apperr.CodeUnauthenticated, "Keine Sitzung vorhanden.")
	}

	tokenHash := HashToken(rawSessionToken)
	sess, err := s.sessions.GetByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, nil, apperr.New(apperr.CodeUnauthenticated, "Ungültige oder abgelaufene Sitzung.")
	}

	now := time.Now().UTC()
	if now.After(sess.ExpiresAt) {
		_ = s.sessions.Delete(ctx, sess.ID)
		return nil, nil, apperr.New(apperr.CodeUnauthenticated, "Sitzung ist abgelaufen.")
	}

	if now.Sub(sess.LastSeenAt) > s.inactivityPeriod {
		_ = s.sessions.Delete(ctx, sess.ID)
		return nil, nil, apperr.New(apperr.CodeUnauthenticated, "Sitzung wegen Inaktivität abgelaufen.")
	}

	user, err := s.users.GetByID(ctx, sess.UserID)
	if err != nil {
		_ = s.sessions.Delete(ctx, sess.ID)
		return nil, nil, apperr.New(apperr.CodeUnauthenticated, "Benutzerkonto existiert nicht mehr.")
	}

	if !user.Enabled {
		_ = s.sessions.Delete(ctx, sess.ID)
		return nil, nil, apperr.New(apperr.CodeUnauthenticated, "Benutzerkonto ist deaktiviert.")
	}

	// Throttle session touch to at most once every 15 minutes to reduce DB write load
	if now.Sub(sess.LastSeenAt) > 15*time.Minute {
		_ = s.sessions.Touch(ctx, sess.ID, now, "")
		sess.LastSeenAt = now
	}

	return user, sess, nil
}

// ChangePassword verifies the current password, updates to the new password,
// and revokes all other sessions of the user.
func (s *Service) ChangePassword(ctx context.Context, userID, currentSessionID string, req ChangePasswordRequest) error {
	if err := ValidatePassword(req.NewPassword); err != nil {
		return err
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}

	match, err := VerifyPassword(req.CurrentPassword, user.PasswordHash)
	if err != nil || !match {
		return apperr.New(apperr.CodeInvalidCredentials, "Das aktuelle Passwort ist nicht korrekt.")
	}

	newHash, err := HashPassword(req.NewPassword)
	if err != nil {
		return err
	}

	if err := s.users.UpdatePassword(ctx, userID, newHash); err != nil {
		return err
	}

	// Revoke all other sessions of this user
	if err := s.sessions.DeleteByUser(ctx, userID, currentSessionID); err != nil {
		s.logger.Warn("failed to revoke other sessions on password change", "user_id", userID, "error", err)
	}

	s.logger.Info("user changed password, revoked other sessions", "user_id", userID)
	return nil
}

// UpdateProfile updates the display name of the user.
func (s *Service) UpdateProfile(ctx context.Context, userID string, req UpdateProfileRequest) (*UserSummary, error) {
	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Der Anzeigename darf nicht leer sein.")
	}
	if len(name) > 64 {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Der Anzeigename darf maximal 64 Zeichen lang sein.")
	}

	if err := s.users.UpdateProfile(ctx, userID, name); err != nil {
		return nil, err
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	summary := user.Summary()
	return &summary, nil
}

// ListSessions returns all active sessions for a user with current session flag.
func (s *Service) ListSessions(ctx context.Context, userID, currentSessionID string) ([]SessionSummary, error) {
	sessions, err := s.sessions.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	summaries := make([]SessionSummary, len(sessions))
	for i, sess := range sessions {
		summaries[i] = SessionSummary{
			ID:         sess.ID,
			UserAgent:  sess.UserAgent,
			IPAddress:  sess.IPAddress,
			CreatedAt:  sess.CreatedAt,
			ExpiresAt:  sess.ExpiresAt,
			LastSeenAt: sess.LastSeenAt,
			IsCurrent:  sess.ID == currentSessionID,
		}
	}
	return summaries, nil
}

// RevokeSession deletes a specific session of a user.
func (s *Service) RevokeSession(ctx context.Context, userID, sessionID string) error {
	sess, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess.UserID != userID {
		return apperr.New(apperr.CodeForbidden, "Keine Berechtigung für diese Sitzung.")
	}
	return s.sessions.Delete(ctx, sessionID)
}

// RevokeOtherSessions revokes all sessions of a user except the current one.
func (s *Service) RevokeOtherSessions(ctx context.Context, userID, currentSessionID string) error {
	return s.sessions.DeleteByUser(ctx, userID, currentSessionID)
}

// ListUsers returns a paginated list of all users and total count (Admin only).
func (s *Service) ListUsers(ctx context.Context, limit, offset int) ([]UserSummary, int, error) {
	users, err := s.users.List(ctx, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.users.Count(ctx)
	if err != nil {
		return nil, 0, err
	}

	summaries := make([]UserSummary, len(users))
	for i, u := range users {
		summaries[i] = u.Summary()
	}
	return summaries, total, nil
}

// CreateUser creates a new user account (Admin only).
func (s *Service) CreateUser(ctx context.Context, req CreateUserRequest) (*UserSummary, error) {
	normUsername := NormalizeUsername(req.Username)
	if err := ValidateUsername(normUsername); err != nil {
		return nil, err
	}
	if err := ValidatePassword(req.Password); err != nil {
		return nil, err
	}
	if !req.Role.IsValid() {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Ungültige Benutzerrolle.")
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = normUsername
	}

	user := User{
		ID:           newUUID(),
		Username:     normUsername,
		DisplayName:  displayName,
		PasswordHash: hash,
		Role:         req.Role,
		Enabled:      true,
	}

	if err := s.users.Create(ctx, user); err != nil {
		return nil, err
	}

	s.logger.Info("administrator created new user",
		"created_user_id", user.ID,
		"username", user.Username,
		"role", user.Role,
	)

	summary := user.Summary()
	return &summary, nil
}

// GetUser returns a specific user summary (Admin only).
func (s *Service) GetUser(ctx context.Context, id string) (*UserSummary, error) {
	user, err := s.users.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	summary := user.Summary()
	return &summary, nil
}

// UpdateUser updates role, status or display name of a user (Admin only).
func (s *Service) UpdateUser(ctx context.Context, id string, req UpdateUserStatusRequest) (*UserSummary, error) {
	user, err := s.users.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	newRole := user.Role
	if req.Role != nil {
		if !req.Role.IsValid() {
			return nil, apperr.New(apperr.CodeInvalidRequest, "Ungültige Benutzerrolle.")
		}
		newRole = *req.Role
	}

	newEnabled := user.Enabled
	if req.Enabled != nil {
		newEnabled = *req.Enabled
	}

	if req.Role != nil || req.Enabled != nil {
		if err := s.users.UpdateStatus(ctx, id, newEnabled, newRole); err != nil {
			return nil, err
		}
	}

	if req.DisplayName != nil {
		name := strings.TrimSpace(*req.DisplayName)
		if name == "" {
			return nil, apperr.New(apperr.CodeInvalidRequest, "Der Anzeigename darf nicht leer sein.")
		}
		if len(name) > 64 {
			return nil, apperr.New(apperr.CodeInvalidRequest, "Der Anzeigename darf maximal 64 Zeichen lang sein.")
		}
		if err := s.users.UpdateProfile(ctx, id, name); err != nil {
			return nil, err
		}
	}

	updated, err := s.users.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	s.logger.Info("administrator updated user status",
		"updated_user_id", id,
		"role", updated.Role,
		"enabled", updated.Enabled,
	)

	summary := updated.Summary()
	return &summary, nil
}

// ResetPassword resets a user's password and revokes all active sessions for that user (Admin only).
func (s *Service) ResetPassword(ctx context.Context, id, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}

	// Verify user exists
	if _, err := s.users.GetByID(ctx, id); err != nil {
		return err
	}

	newHash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	if err := s.users.UpdatePassword(ctx, id, newHash); err != nil {
		return err
	}

	// Revoke all active sessions for this user immediately
	if err := s.sessions.DeleteByUser(ctx, id, ""); err != nil {
		s.logger.Warn("failed to revoke sessions on admin password reset", "user_id", id, "error", err)
	}

	s.logger.Info("administrator reset user password and revoked all sessions", "user_id", id)
	return nil
}

// DeleteUser deletes a user account (Admin only). Last active admin invariant
// is enforced at the repository transaction level.
func (s *Service) DeleteUser(ctx context.Context, id string) error {
	if err := s.users.Delete(ctx, id); err != nil {
		return err
	}
	s.logger.Info("administrator deleted user", "deleted_user_id", id)
	return nil
}

// CleanupExpired purges expired sessions from the database.
func (s *Service) CleanupExpired(ctx context.Context) error {
	now := time.Now().UTC()
	return s.sessions.DeleteExpired(ctx, now)
}

// StartCleanupLoop starts a periodic background worker that purges expired sessions.
func (s *Service) StartCleanupLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	go func() {
		// Run initial cleanup on startup
		if err := s.CleanupExpired(ctx); err != nil && !errors.Is(err, context.Canceled) {
			s.logger.Warn("initial session cleanup failed", "error", err)
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.CleanupExpired(ctx); err != nil && !errors.Is(err, context.Canceled) {
					s.logger.Warn("periodic session cleanup failed", "error", err)
				}
			}
		}
	}()
}
