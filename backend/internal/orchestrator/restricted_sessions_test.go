package orchestrator_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/mediasession"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/provider/youtube"
	"ytdm/backend/internal/ytdlp"
)

func TestRestrictedSourceTriesExistingEntitledSession(t *testing.T) {
	for _, tc := range []struct {
		name, message          string
		limit, good, wantCalls int
		want                   apperr.Code
	}{
		{"existing age-verified session", "Sign in to confirm your age", 5, 4, 4, ""},
		{"existing premium session", "This video is only available to Music Premium members", 5, 4, 4, ""},
		{"bounded attempts", "Sign in to confirm your age", 2, 4, 3, apperr.CodeTrackNotFound},
		{"all restricted", "Sign in to confirm your age", 5, 0, 6, apperr.CodeTrackNotFound},
		{"bot never rotates", "Sign in to confirm you are not a bot", 5, 4, 1, apperr.CodeSessionBotChallenge},
		{"throttling never rotates", "HTTP Error 429: Too Many Requests", 5, 4, 1, apperr.CodeProviderRateLimited},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessions := make([]mediasession.Session, 5)
			for i := range sessions {
				s := healthyAuditSession()
				s.ID = fmt.Sprintf("session-%d", i+1)
				s.CookieRef = mediasession.CookieRefPrefix + s.ID
				sessions[i] = s
			}
			_, pool, _, _, cooldown := setupTestEnvironment(t, sessions...)
			dir := t.TempDir()
			binary := filepath.Join(dir, "yt-dlp")
			script := fmt.Sprintf(`#!/bin/sh
printf 'call\n' >> "$0.calls"
case "$*" in
 *ytsearch*) exit 0;;
 *session-%d.cookies.txt*) echo '{"id":"dQw4w9WgXcQ","title":"Dancing Queen","artist":"ABBA","duration":231,"formats":[{"format_id":"251","acodec":"opus","vcodec":"none"}]}' ;;
 *) echo 'ERROR: [youtube] dQw4w9WgXcQ: %s' >&2;exit 1;;
esac
`, tc.good, tc.message)
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			p, err := youtube.New(youtube.Config{Name: "ytmusic", Client: ytdlp.New(ytdlp.Options{Binary: binary}), EnrichLimit: 1})
			if err != nil {
				t.Fatal(err)
			}
			orch := newOrchestrator(pool, cooldown, p)
			track := music.Track{Title: "Dancing Queen", Artists: []string{"ABBA"}, DurationMS: 231000, SourceProvider: "ytmusic", SourceID: "dQw4w9WgXcQ"}
			res, err := orch.ResolveMedia(context.Background(), "ytmusic", track, tc.limit)
			if apperr.CodeOf(err) != tc.want {
				t.Fatalf("result code %s, want %s", apperr.CodeOf(err), tc.want)
			}
			if tc.want == "" {
				if res.SessionID != fmt.Sprintf("session-%d", tc.good) {
					t.Fatal("wrong download affinity")
				}
				orch.RecordDownloadOutcome(context.Background(), res.SessionID, nil)
			}
			calls, err := os.ReadFile(binary + ".calls")
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(string(calls), "call"); got != tc.wantCalls {
				t.Fatalf("process count=%d,want=%d", got, tc.wantCalls)
			}
			assertNoLeaseHeld(t, pool)
			if tc.want == apperr.CodeTrackNotFound || tc.want == "" {
				assertNoFamilyPause(t, pool, cooldown)
				for _, s := range pool.Sessions() {
					if s.HealthStatus != mediasession.HealthHealthy {
						t.Fatal("restriction charged session health")
					}
				}
			}
		})
	}
}
