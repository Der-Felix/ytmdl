package orchestrator_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"ytdm/backend/internal/apperr"
	"ytdm/backend/internal/matcher"
	"ytdm/backend/internal/music"
	"ytdm/backend/internal/orchestrator"
	"ytdm/backend/internal/provider"
	"ytdm/backend/internal/provider/soundcloud"
	"ytdm/backend/internal/ytdlp"
)

// Use the real SoundCloud resolver behind an offline yt-dlp fixture so the
// preview classification, candidate loop and cooldown decision are all exercised.
func TestSoundCloudPreviewRemainsCandidateScoped(t *testing.T) {
	for _, tc := range []struct {
		name, second string
		limit        int
		wantCode     apperr.Code
		wantCalls    string
	}{
		{"next_full_candidate", "full", 5, "", "search\npreview\nfull\n"},
		{"preview_exhaustion", "", 5, apperr.CodeTrackNotFound, "search\npreview\n"},
		{"candidate_limit_preserved", "full", 1, apperr.CodeTrackNotFound, "search\npreview\n"},
		{"weak_match_rejected", "unrelated", 5, apperr.CodeTrackNotFound, "search\npreview\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			binary := filepath.Join(dir, "yt-dlp")
			// The marker stays beside the tool, separate from any download directory.
			script := `#!/bin/sh
for arg do
 if [ "$arg" = '--cookies' ]; then exit 9; fi
done
case "$*" in
 *scsearch*)
 echo search >> "$0.calls"
 echo '{"id":"254407911","title":"Preview Song","artist":"Test Artist","duration":195,"webpage_url":"https://soundcloud.com/test/preview"}'
`
			if tc.second != "" {
				title := "Preview Song"
				if tc.second == "unrelated" {
					title = "Completely Different Recording"
				}
				script += `echo '{"id":"full","title":"` + title + `","artist":"Test Artist","duration":195,"webpage_url":"https://soundcloud.com/test/full"}'` + "\n"
			}
			script += `;;
 *test/preview*)
 echo preview >> "$0.calls"
 echo '{"id":"254407911","duration":30,"formats":[{"format_id":"hls_mp3_1_0_preview","acodec":"mp3","vcodec":"none"},{"format_id":"http_mp3_1_0_preview","acodec":"mp3","vcodec":"none"}]}'
 ;;
 *test/full*)
 echo full >> "$0.calls"
 echo '{"id":"full","duration":195,"formats":[{"format_id":"http_mp3_128","acodec":"mp3","vcodec":"none"}]}'
 ;;
 *) exit 8;;
esac
`
			if err := os.WriteFile(binary, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			sc, err := soundcloud.New(soundcloud.Config{Client: ytdlp.New(ytdlp.Options{Binary: binary})})
			if err != nil {
				t.Fatal(err)
			}
			_, pool, ytm, yt, _, cooldown := setupPreRoutingEnv(t, healthyAuditSession())
			reg := provider.NewRegistry()
			reg.RegisterMedia(sc)
			reg.RegisterMedia(ytm)
			reg.RegisterMedia(yt)
			orch := orchestrator.New(orchestrator.Options{Registry: reg, SessionPool: pool, Cooldown: cooldown, Matcher: matcher.New(matcher.Options{MinScore: 85, DurationToleranceMS: 15000})})
			result, err := orch.ResolveMedia(context.Background(), "soundcloud", music.Track{Title: "Preview Song", Artists: []string{"Test Artist"}, DurationMS: 195000}, tc.limit)
			if apperr.CodeOf(err) != tc.wantCode {
				t.Fatalf("error=%v, want %s", err, tc.wantCode)
			}
			if err == nil {
				if result.Source.ID != "full" || result.Source.SessionID != "" || result.AttemptedCount != 2 {
					t.Fatalf("unexpected resolution: %+v", result)
				}
			} else if apperr.ScopeOf(err) != apperr.ScopeCandidate || apperr.StopsCandidateFanout(err) {
				t.Fatalf("preview failure escaped candidate scope: %v", err)
			}
			calls, err := os.ReadFile(binary + ".calls")
			if err != nil {
				t.Fatal(err)
			}
			if string(calls) != tc.wantCalls {
				t.Fatalf("calls=%q, want %q", calls, tc.wantCalls)
			}
			cooldown.mu.Lock()
			triggered := len(cooldown.cooldowns)
			cooldown.mu.Unlock()
			if triggered != 0 {
				t.Fatal("preview triggered a family cooldown")
			}
			if pool.Sessions()[0].HealthStatus != healthyAuditSession().HealthStatus {
				t.Fatal("preview changed YouTube session health")
			}
			if tc.wantCode == "" && (ytm.SearchCalls() != 0 || yt.SearchCalls() != 0) {
				t.Fatal("successful candidate triggered provider fallback")
			}
			if tc.wantCode != "" && (ytm.SearchCalls() != 1 || yt.SearchCalls() != 1) {
				t.Fatal("candidate exhaustion changed the existing provider fallback")
			}
		})
	}
}
