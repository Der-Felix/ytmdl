package mediasession

import (
	"bytes"
	"strconv"
	"strings"

	"ytdm/backend/internal/apperr"
)

const (
	// MaxCookieFileSize bounds uploaded cookie files to 1 MiB.
	MaxCookieFileSize = 1024 * 1024

	// MaxCookieLineLength bounds individual lines to 4096 bytes to avoid regex/parser DOS.
	MaxCookieLineLength = 4096
)

// ValidateNetscapeCookies inspects data to verify it conforms to the standard
// Netscape HTTP Cookie File format. It validates structure without leaking cookie
// values into error messages.
func ValidateNetscapeCookies(data []byte) error {
	if len(data) == 0 {
		return apperr.New(apperr.CodeInvalidRequest, "uploaded cookie file is empty")
	}
	if len(data) > MaxCookieFileSize {
		return apperr.Newf(apperr.CodeInvalidRequest, "cookie file exceeds maximum allowed size of %d bytes", MaxCookieFileSize)
	}
	if bytes.ContainsRune(data, 0) {
		return apperr.New(apperr.CodeInvalidRequest, "binary data detected in cookie file")
	}

	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	validRecords := 0

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if len(line) > MaxCookieLineLength {
			return apperr.Newf(apperr.CodeInvalidRequest, "line %d exceeds maximum line length of %d bytes", lineNum, MaxCookieLineLength)
		}

		// Comment lines
		if strings.HasPrefix(trimmed, "#") {
			// #HttpOnly_ is a standard convention for HttpOnly cookies in Netscape format
			if strings.HasPrefix(trimmed, "#HttpOnly_") {
				line = strings.TrimPrefix(trimmed, "#HttpOnly_")
			} else {
				continue
			}
		}

		// Non-comment line must be tab-delimited with 7 fields
		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			return apperr.Newf(apperr.CodeInvalidRequest, "malformed cookie record at line %d: expected 7 tab-separated fields, found %d", lineNum, len(fields))
		}

		domain := strings.TrimSpace(fields[0])
		if domain == "" {
			return apperr.Newf(apperr.CodeInvalidRequest, "malformed cookie record at line %d: empty domain", lineNum)
		}

		includeSub := strings.ToUpper(strings.TrimSpace(fields[1]))
		if includeSub != "TRUE" && includeSub != "FALSE" {
			return apperr.Newf(apperr.CodeInvalidRequest, "malformed cookie record at line %d: invalid include-subdomains flag", lineNum)
		}

		path := strings.TrimSpace(fields[2])
		if path == "" {
			return apperr.Newf(apperr.CodeInvalidRequest, "malformed cookie record at line %d: empty path", lineNum)
		}

		secure := strings.ToUpper(strings.TrimSpace(fields[3]))
		if secure != "TRUE" && secure != "FALSE" {
			return apperr.Newf(apperr.CodeInvalidRequest, "malformed cookie record at line %d: invalid secure flag", lineNum)
		}

		expiryStr := strings.TrimSpace(fields[4])
		if _, err := strconv.ParseInt(expiryStr, 10, 64); err != nil {
			return apperr.Newf(apperr.CodeInvalidRequest, "malformed cookie record at line %d: invalid expiry timestamp", lineNum)
		}

		name := strings.TrimSpace(fields[5])
		if name == "" {
			return apperr.Newf(apperr.CodeInvalidRequest, "malformed cookie record at line %d: empty cookie name", lineNum)
		}

		// Field 6 is value - legal values may contain symbols, base64, JSON, etc.
		validRecords++
	}

	if validRecords == 0 {
		return apperr.New(apperr.CodeInvalidRequest, "no valid cookie records found in file")
	}

	return nil
}
