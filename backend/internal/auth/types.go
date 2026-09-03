// Package auth provides local user authentication, password hashing, session
// management, and role-based authorization for YTMDL.
package auth

import (
	"strings"
	"time"

	"ytdm/backend/internal/apperr"
)

// Role defines the authorization level of a user.
type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

// IsValid returns true if the role is a recognized role.
func (r Role) IsValid() bool {
	return r == RoleAdmin || r == RoleUser
}

// User represents a local user account.
type User struct {
	ID           string     `json:"id"`
	Username     string     `json:"username"`
	DisplayName  string     `json:"display_name"`
	PasswordHash string     `json:"-"`
	Role         Role       `json:"role"`
	Enabled      bool       `json:"enabled"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastLoginAt  *time.Time `json:"last_login_at"`
}

// UserSummary is the public representation of a user without sensitive data.
type UserSummary struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	Role        Role       `json:"role"`
	Enabled     bool       `json:"enabled"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastLoginAt *time.Time `json:"last_login_at"`
}

// Summary converts User to UserSummary.
func (u User) Summary() UserSummary {
	return UserSummary{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Role:        u.Role,
		Enabled:     u.Enabled,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
		LastLoginAt: u.LastLoginAt,
	}
}

// Session represents an active login session.
type Session struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	TokenHash  string    `json:"-"`
	UserAgent  string    `json:"user_agent"`
	IPAddress  string    `json:"ip_address"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// SessionSummary represents a session for profile management.
type SessionSummary struct {
	ID         string    `json:"id"`
	UserAgent  string    `json:"user_agent"`
	IPAddress  string    `json:"ip_address"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	IsCurrent  bool      `json:"is_current"`
}

// NormalizeUsername trims whitespace and converts to lowercase.
func NormalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

// ValidateUsername checks username format.
func ValidateUsername(username string) error {
	norm := NormalizeUsername(username)
	if len(norm) < 3 || len(norm) > 32 {
		return apperr.New(apperr.CodeInvalidRequest, "Der Benutzername muss zwischen 3 und 32 Zeichen lang sein.")
	}
	for _, ch := range norm {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' || ch == '.') {
			return apperr.New(apperr.CodeInvalidRequest, "Der Benutzername darf nur Buchstaben, Ziffern, Unterstriche, Bindestriche und Punkte enthalten.")
		}
	}
	return nil
}

// ValidatePassword checks password requirements.
func ValidatePassword(password string) error {
	if len(password) < 8 {
		return apperr.New(apperr.CodeInvalidRequest, "Das Passwort muss mindestens 8 Zeichen lang sein.")
	}
	if len(password) > 128 {
		return apperr.New(apperr.CodeInvalidRequest, "Das Passwort darf maximal 128 Zeichen lang sein.")
	}
	return nil
}
