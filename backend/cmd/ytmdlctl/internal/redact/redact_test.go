package redact_test

import (
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/redact"
)

func TestRedactSensitive(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain text without secrets",
			input:    "error: container not found",
			expected: "error: container not found",
		},
		{
			name:     "database url with password",
			input:    "failed to connect to postgres://ytmdl:supersecret123@db:5432/ytmdl?sslmode=disable",
			expected: "failed to connect to postgres://ytmdl:***REDACTED***@db:5432/ytmdl?sslmode=disable",
		},
		{
			name:     "database url without password",
			input:    "connected to postgres://localhost:5432/ytmdl",
			expected: "connected to postgres://localhost:5432/ytmdl",
		},
		{
			name:     "storage guard token in text",
			input:    "marker content was ytmdl-storage:9b7c8d9e-1234-5678-9abc-def012345678 in root",
			expected: "marker content was ytmdl-storage:***REDACTED*** in root",
		},
		{
			name:     "password env assignment",
			input:    "POSTGRES_PASSWORD=mysecretpassword in environment",
			expected: "POSTGRES_PASSWORD=***REDACTED*** in environment",
		},
		{
			name:     "token in log",
			input:    "token=ghp_ABC123XYZ456secret was passed",
			expected: "token=***REDACTED*** was passed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := redact.String(tc.input)
			if got != tc.expected {
				t.Errorf("redact.String(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestRedactCallerProvidedValues(t *testing.T) {
	dbPassword := "super_secret_db_pass_999"
	guardID := "e3b0c442-98fc-1c14-9afb-4c8996fb9242"
	apiToken := "ytmdl_live_token_abcdef123456"

	tests := []struct {
		name     string
		input    string
		secrets  []string
		expected string
	}{
		{
			name:     "caller provided db password",
			input:    "failed auth with user postgres and password super_secret_db_pass_999",
			secrets:  []string{dbPassword},
			expected: "failed auth with user postgres and password ***REDACTED***",
		},
		{
			name:     "caller provided database URL with password",
			input:    "failed to connect to postgres://ytmdl:super_secret_db_pass_999@db:5432/ytmdl?sslmode=disable",
			secrets:  []string{dbPassword},
			expected: "failed to connect to postgres://ytmdl:***REDACTED***@db:5432/ytmdl?sslmode=disable",
		},
		{
			name:     "caller provided storage guard ID",
			input:    "active guard verification id is e3b0c442-98fc-1c14-9afb-4c8996fb9242 on volume",
			secrets:  []string{guardID},
			expected: "active guard verification id is ***REDACTED*** on volume",
		},
		{
			name:     "caller provided generic API token value",
			input:    "request authorization header Bearer ytmdl_live_token_abcdef123456 failed",
			secrets:  []string{apiToken},
			expected: "request authorization header Bearer ***REDACTED*** failed",
		},
		{
			name:     "all four combined",
			input:    "db=super_secret_db_pass_999 guard=e3b0c442-98fc-1c14-9afb-4c8996fb9242 token=ytmdl_live_token_abcdef123456 url=postgres://u:p@host/db",
			secrets:  []string{dbPassword, guardID, apiToken},
			expected: "db=***REDACTED*** guard=***REDACTED*** token=***REDACTED*** url=postgres://u:***REDACTED***@host/db",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := redact.Values(tc.input, tc.secrets...)
			if got != tc.expected {
				t.Errorf("redact.Values(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}
