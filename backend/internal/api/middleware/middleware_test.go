package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLoggerReportsResolvedClient pins the access log behind a reverse proxy:
// remote_ip is the client resolved through trusted proxies, and the direct
// peer is logged as peer_ip only when it differs.
func TestLoggerReportsResolvedClient(t *testing.T) {
	SetTrustedProxies([]string{"172.31.250.1", "fdd0:1::1"})
	t.Cleanup(func() { SetTrustedProxies(nil) })

	cases := []struct {
		name     string
		peer     string
		header   string
		wantIP   string
		wantPeer string
	}{
		{"direct client", "203.0.113.50:5000", "", "203.0.113.50", ""},
		{"untrusted peer cannot forge", "203.0.113.50:5000", "198.51.100.66", "203.0.113.50", ""},
		{"trusted gateway", "172.31.250.1:40000", "192.168.1.20", "192.168.1.20", "172.31.250.1"},
		{"trusted IPv6 gateway", "[fdd0:1::1]:40000", "192.168.1.20", "192.168.1.20", "fdd0:1::1"},
		{"trusted peer without header", "172.31.250.1:40000", "", "172.31.250.1", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))
			handler := Logger(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))

			req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
			req.RemoteAddr = tc.peer
			if tc.header != "" {
				req.Header.Set("X-Forwarded-For", tc.header)
			}
			handler.ServeHTTP(httptest.NewRecorder(), req)

			var line map[string]any
			if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
				t.Fatalf("decode log line %q: %v", buf.String(), err)
			}
			if got := line["remote_ip"]; got != tc.wantIP {
				t.Fatalf("remote_ip = %v, want %q", got, tc.wantIP)
			}
			got, present := line["peer_ip"]
			if tc.wantPeer == "" {
				if present {
					t.Fatalf("peer_ip = %v, want it absent", got)
				}
				return
			}
			if got != tc.wantPeer {
				t.Fatalf("peer_ip = %v, want %q", got, tc.wantPeer)
			}
		})
	}
}
