// Package redact sanitizes sensitive information from errors and log output.
package redact

import (
	"regexp"
)

var (
	// Matches postgres://user:pass@host:port/db
	postgresURLRegex = regexp.MustCompile(`(postgres(?:ql)?://[^:]+:)([^@]+)(@)`)
	// Matches ytmdl-storage:<token>
	storageGuardRegex = regexp.MustCompile(`(ytmdl-storage:)([^\s]+)`)
	// Matches PASSWORD=... or token=...
	envSecretRegex = regexp.MustCompile(`(?i)\b(POSTGRES_PASSWORD|PASSWORD|SECRET|TOKEN)=([^\s"']+)`)
)

// String replaces recognized conservative sensitive patterns with ***REDACTED***.
func String(s string) string {
	if s == "" {
		return ""
	}
	s = postgresURLRegex.ReplaceAllString(s, "${1}***REDACTED***${3}")
	s = storageGuardRegex.ReplaceAllString(s, "${1}***REDACTED***")
	s = envSecretRegex.ReplaceAllString(s, "${1}=***REDACTED***")
	return s
}

// Values replaces explicitly provided sensitive string values in s with ***REDACTED***,
// in addition to applying conservative credential patterns.
func Values(s string, sensitiveValues ...string) string {
	if s == "" {
		return ""
	}
	for _, secret := range sensitiveValues {
		if len(secret) >= 3 {
			s = regexp.MustCompile(regexp.QuoteMeta(secret)).ReplaceAllLiteralString(s, "***REDACTED***")
		}
	}
	return String(s)
}
