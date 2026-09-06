package mediasession_test

import (
	"bytes"
	"strings"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/mediasession"
)

const secretSentinel = "SUPER_SECRET_COOKIE_TOKEN_ABC123XYZ"

func TestValidateNetscapeCookies_ValidFormats(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{
			name: "standard netscape with header",
			data: "# Netscape HTTP Cookie File\n" +
				"# https://curl.se/docs/http-cookies.html\n" +
				".youtube.com\tTRUE\t/\tTRUE\t1750000000\tSID\t" + secretSentinel + "\n" +
				".youtube.com\tTRUE\t/\tTRUE\t1750000000\tHSID\tsome_value\n",
		},
		{
			name: "httponly cookie record",
			data: "# Netscape HTTP Cookie File\n" +
				"#HttpOnly_.youtube.com\tTRUE\t/\tTRUE\t1750000000\tSSID\t" + secretSentinel + "\n",
		},
		{
			name: "complex cookie value with symbols and punctuation",
			data: ".google.com\tTRUE\t/\tTRUE\t0\tPREF\tf1=50000000&tz=Europe/Berlin&lang=de-DE\n" +
				".youtube.com\tTRUE\t/\tFALSE\t1750000000\tDATA\teyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0\n",
		},
		{
			name: "windows line endings",
			data: "# Netscape HTTP Cookie File\r\n.youtube.com\tTRUE\t/\tTRUE\t1750000000\tSID\tval\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mediasession.ValidateNetscapeCookies([]byte(tt.data))
			if err != nil {
				t.Fatalf("expected valid cookie format, got error: %v", err)
			}
		})
	}
}

func TestValidateNetscapeCookies_InvalidFormats_AndNoSecretLeakage(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		wantErrCode apperr.Code
	}{
		{
			name:        "empty data",
			data:        []byte(""),
			wantErrCode: apperr.CodeInvalidRequest,
		},
		{
			name:        "binary data with NUL byte",
			data:        []byte(".youtube.com\tTRUE\t/\tTRUE\t1750000000\tSID\tval\x00extra"),
			wantErrCode: apperr.CodeInvalidRequest,
		},
		{
			name:        "only comments and blank lines",
			data:        []byte("# Netscape HTTP Cookie File\n# Just a comment\n\n"),
			wantErrCode: apperr.CodeInvalidRequest,
		},
		{
			name:        "fewer than 7 fields with secret sentinel",
			data:        []byte(".youtube.com\tTRUE\t/\tTRUE\t" + secretSentinel + "\n"),
			wantErrCode: apperr.CodeInvalidRequest,
		},
		{
			name:        "invalid boolean flag",
			data:        []byte(".youtube.com\tYES\t/\tTRUE\t1750000000\tSID\t" + secretSentinel + "\n"),
			wantErrCode: apperr.CodeInvalidRequest,
		},
		{
			name:        "invalid expiry number",
			data:        []byte(".youtube.com\tTRUE\t/\tTRUE\tNOT_A_NUMBER\tSID\t" + secretSentinel + "\n"),
			wantErrCode: apperr.CodeInvalidRequest,
		},
		{
			name:        "oversized file",
			data:        bytes.Repeat([]byte("a"), mediasession.MaxCookieFileSize+10),
			wantErrCode: apperr.CodeInvalidRequest,
		},
		{
			name:        "excessively long line",
			data:        []byte(".youtube.com\tTRUE\t/\tTRUE\t1750000000\tSID\t" + strings.Repeat("x", mediasession.MaxCookieLineLength+10) + "\n"),
			wantErrCode: apperr.CodeInvalidRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := mediasession.ValidateNetscapeCookies(tt.data)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if apperr.CodeOf(err) != tt.wantErrCode {
				t.Fatalf("expected error code %s, got %s", tt.wantErrCode, apperr.CodeOf(err))
			}

			// Critical requirement: secret sentinel must NEVER appear in error messages!
			errMsg := err.Error()
			if strings.Contains(errMsg, secretSentinel) {
				t.Fatalf("CRITICAL: error message leaked secret sentinel: %s", errMsg)
			}
		})
	}
}
