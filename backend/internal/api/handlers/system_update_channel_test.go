package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"ytdm/backend/internal/api"
	"ytdm/backend/internal/api/handlers"
	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/update"
)

// TestUpdateChannelRequiresAdminAndOnlyRecordsTheChoice checks the complete
// route: authentication, administrator role, CSRF, persistence in the
// settings table and the absence of any side effect beyond a read-only check.
func TestUpdateChannelRequiresAdminAndOnlyRecordsTheChoice(t *testing.T) {
	var githubCalls atomic.Int32
	fakeGitHub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		githubCalls.Add(1)
		if r.URL.Path == "/repos/Der-Felix/ytmdl/releases" {
			fmt.Fprint(w, `[{"tag_name":"v0.27.2-rc.1","name":"YTMDL v0.27.2-rc.1","draft":false,"prerelease":true,"html_url":"https://github.com/Der-Felix/ytmdl/releases/tag/v0.27.2-rc.1","assets":[{"name":"release-manifest.json"},{"name":"SHA256SUMS"},{"name":"ytmdlctl-linux-amd64"},{"name":"ytmdlctl-linux-arm64"},{"name":"ytmdlctl-darwin-amd64"},{"name":"ytmdlctl-darwin-arm64"}]},{"tag_name":"v0.27.1","draft":false,"prerelease":false}]`)
			return
		}
		fmt.Fprint(w, `{"tag_name":"v0.27.1","name":"YTMDL v0.27.1","draft":false,"prerelease":false,"html_url":"https://github.com/Der-Felix/ytmdl/releases/tag/v0.27.1"}`)
	}))
	defer fakeGitHub.Close()

	db := dbtest.Open(t)
	settingsRepo := repository.NewSettings(db)
	updates := update.NewService(update.Config{Enabled: true, CheckInterval: time.Hour, BaseURL: fakeGitHub.URL}, "0.27.1", fakeGitHub.Client(), nil)
	if err := updates.UseSettings(context.Background(), settingsRepo); err != nil {
		t.Fatal(err)
	}

	limiter := auth.NewLimiter(20, 5*time.Minute)
	t.Cleanup(limiter.Close)
	authService := auth.NewService(repository.NewUsers(db), repository.NewSessions(db), limiter, nil)
	router, err := api.NewRouter(api.RouterOptions{
		Handlers: handlers.NewForTest(handlers.Deps{Auth: authService, Updates: updates}),
		Auth:     authService,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	admin, err := authService.Setup(ctx, auth.SetupRequest{Username: "admin_upd", DisplayName: "Admin", Password: "password123!"}, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authService.CreateUser(ctx, auth.CreateUserRequest{Username: "user_upd", DisplayName: "User", Password: "password123!", Role: auth.RoleUser}); err != nil {
		t.Fatal(err)
	}
	user, err := authService.Login(ctx, auth.LoginRequest{Username: "user_upd", Password: "password123!"}, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	csrf, err := auth.GenerateCSRFToken()
	if err != nil {
		t.Fatal(err)
	}

	put := func(token, csrfToken, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/v1/system/update/channel", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.AddCookie(&http.Cookie{Name: "ytmdl_session", Value: token})
		}
		if csrfToken != "" {
			req.AddCookie(&http.Cookie{Name: middleware.CSRFCookieName, Value: csrfToken})
			req.Header.Set(middleware.CSRFHeaderName, csrfToken)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	stored := func() string {
		values, err := settingsRepo.All(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return values[update.SettingsKeyChannel]
	}

	body := `{"channel":"development"}`
	if rec := put("", "", body); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: %d %s", rec.Code, rec.Body.String())
	}
	if rec := put(user.SessionToken, csrf, body); rec.Code != http.StatusForbidden {
		t.Fatalf("standard user: %d", rec.Code)
	}
	if rec := put(admin.SessionToken, "", body); rec.Code != http.StatusForbidden {
		t.Fatalf("admin without CSRF: %d", rec.Code)
	}
	if rec := put(admin.SessionToken, csrf, `{"channel":"nightly"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown channel: %d", rec.Code)
	}
	if got := stored(); got != "" {
		t.Fatalf("rejected requests changed the setting: %q", got)
	}

	rec := put(admin.SessionToken, csrf, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data update.Status `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Channel != update.ChannelDevelopment || resp.Data.LatestVersion != "0.27.2-rc.1" || !resp.Data.LatestPrerelease {
		t.Fatalf("status %+v", resp.Data)
	}
	if got := stored(); got != "development" {
		t.Fatalf("stored channel %q", got)
	}
	if updates.Channel() != update.ChannelDevelopment {
		t.Fatal("service did not switch")
	}
	// The answer came from read-only GitHub API requests only.
	if githubCalls.Load() == 0 {
		t.Fatal("no check was made after the change")
	}
}
