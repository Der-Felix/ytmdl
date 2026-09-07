package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"ytdm/backend/internal/api"
	"ytdm/backend/internal/api/handlers"
	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/playlist"
)

type testClient struct {
	client    *http.Client
	baseURL   string
	csrfToken string
}

func (c *testClient) do(method, path string, body any, withCSRF bool) (*http.Response, []byte, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if withCSRF && c.csrfToken != "" {
		req.Header.Set("X-CSRF-Token", c.csrfToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	for _, cookie := range resp.Cookies() {
		if cookie.Name == "ytmdl_csrf" {
			c.csrfToken = cookie.Value
		}
	}

	return resp, respBody, nil
}

func setupPlaylistsE2ETest(t *testing.T) (*httptest.Server, *testClient, *testClient, []string) {
	t.Helper()
	db := dbtest.Open(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Seed tracks
	_, err := db.ExecContext(ctx, `
		INSERT INTO artists (id, name, sort_key, provider, source_id, created_at, updated_at)
		VALUES ('art_e2e', 'Daft Punk', 'daft punk', 'spotify', 'art_dp', $1, $1)
		ON CONFLICT (id) DO NOTHING
	`, now)
	if err != nil {
		t.Fatalf("insert artist: %v", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO releases (id, artist_id, title, provider, source_id, year, created_at, updated_at)
		VALUES ('rel_e2e', 'art_e2e', 'Discovery', 'spotify', 'rel_dp', 2001, $1, $1)
		ON CONFLICT (id) DO NOTHING
	`, now)
	if err != nil {
		t.Fatalf("insert release: %v", err)
	}

	trackIDs := []string{"trk_e2e_1", "trk_e2e_2", "trk_e2e_3"}
	trackTitles := []string{"One More Time", "Aerodynamic", "Digital Love"}
	for i, tid := range trackIDs {
		_, err = db.ExecContext(ctx, `
			INSERT INTO tracks (id, release_id, artist_id, title, identity_key, duration_ms, created_at, updated_at)
			VALUES ($1, 'rel_e2e', 'art_e2e', $2, $3, 300000, $4, $4)
			ON CONFLICT (id) DO NOTHING
		`, tid, trackTitles[i], "key_"+tid, now)
		if err != nil {
			t.Fatalf("insert track %s: %v", tid, err)
		}
		_, err = db.ExecContext(ctx, `
			INSERT INTO files (id, track_id, path, size_bytes, codec, bitrate_kbps, created_at, updated_at)
			VALUES ($1, $2, $3, 1000000, 'flac', 900, $4, $4)
			ON CONFLICT (id) DO NOTHING
		`, "fil_"+tid, tid, "Daft Punk/Discovery/"+tid+".flac", now)
		if err != nil {
			t.Fatalf("insert file %s: %v", tid, err)
		}
	}

	// Setup backend services
	usersRepo := repository.NewUsers(db)
	sessionsRepo := repository.NewSessions(db)
	limiter := auth.NewLimiter(100, time.Minute)
	t.Cleanup(limiter.Close)
	authService := auth.NewService(usersRepo, sessionsRepo, limiter, nil)

	jobsRepo := repository.NewJobs(db)
	jobMgr := jobs.NewManagerForTest(jobsRepo, nil)

	playlistsRepo := repository.NewPlaylists(db)
	playlistSvc, err := playlist.New(playlist.Options{
		Store: playlistsRepo,
	})
	if err != nil {
		t.Fatalf("new playlist service: %v", err)
	}

	handlerSet := handlers.NewForTest(handlers.Deps{
		Jobs:      jobMgr,
		Auth:      authService,
		Database:  db,
		Playlists: playlistSvc,
	})

	router, err := api.NewRouter(api.RouterOptions{
		Handlers: handlerSet,
		Auth:     authService,
	})
	if err != nil {
		t.Fatalf("new router: %v", err)
	}

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	// Create Client A (Admin/User 1)
	jarA, _ := cookiejar.New(nil)
	clientA := &testClient{
		client:  &http.Client{Jar: jarA},
		baseURL: srv.URL,
	}

	// Setup initial user (User A)
	res, _, err := clientA.do(http.MethodGet, "/api/v1/auth/status", nil, false)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("status failed: %v", err)
	}
	res, body, err := clientA.do(http.MethodPost, "/api/v1/auth/setup", map[string]string{
		"username":     "user_a",
		"display_name": "User Alpha",
		"password":     "Password123!",
	}, true)
	if err != nil || (res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated) {
		t.Fatalf("setup user_a failed: code=%d, body=%s, err=%v", res.StatusCode, string(body), err)
	}

	// Create Client B (User 2)
	_, err = db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, role, enabled, created_at, updated_at)
		VALUES ('u_b', 'user_b', 'hash_b', 'user', true, $1, $1)
		ON CONFLICT (id) DO NOTHING
	`, now)
	if err != nil {
		t.Fatalf("insert user_b: %v", err)
	}

	// Create session for user B directly
	rawToken, tokenHash, err := auth.GenerateSessionToken()
	if err != nil {
		t.Fatalf("generate session token: %v", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, user_agent, ip_address, created_at, expires_at, last_seen_at)
		VALUES ('sess_b', 'u_b', $1, 'test', '127.0.0.1', $2, $3, $2)
	`, tokenHash, now, now.Add(24*time.Hour))
	if err != nil {
		t.Fatalf("insert session_b: %v", err)
	}

	jarB, _ := cookiejar.New(nil)
	clientB := &testClient{
		client:  &http.Client{Jar: jarB},
		baseURL: srv.URL,
	}
	// Warm up CSRF cookie for client B
	res, _, err = clientB.do(http.MethodGet, "/api/v1/auth/status", nil, false)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("clientB status failed: %v", err)
	}
	// Add session cookie to client B
	u, _ := url.Parse(srv.URL)
	clientB.client.Jar.SetCookies(u, []*http.Cookie{
		{
			Name:  "ytmdl_session",
			Value: rawToken,
			Path:  "/",
		},
	})

	return srv, clientA, clientB, trackIDs
}

func TestPlaylistsAndFavorites_E2E(t *testing.T) {
	_, clientA, clientB, tracks := setupPlaylistsE2ETest(t)

	// 1. Unauthenticated request rejected (401)
	unauthClient := &testClient{
		client:  &http.Client{},
		baseURL: clientA.baseURL,
	}
	res, _, err := unauthClient.do(http.MethodGet, "/api/v1/playlists", nil, false)
	if err != nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated GET /playlists, got: %d", res.StatusCode)
	}

	// 2. CSRF rejection: User A POST without CSRF token fails with 403
	res, _, err = clientA.do(http.MethodPost, "/api/v1/playlists", map[string]string{
		"name": "No CSRF",
	}, false)
	if err != nil || res.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden without CSRF token, got: %d", res.StatusCode)
	}

	// 3. User A creates playlist with valid CSRF token -> 201 Created
	res, body, err := clientA.do(http.MethodPost, "/api/v1/playlists", map[string]string{
		"name":        "French Touch Klassiker",
		"description": "Die besten Tracks aus Paris",
	}, true)
	if err != nil || res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created for playlist create, got: %d, body: %s", res.StatusCode, string(body))
	}

	var plCreated struct {
		Data repository.Playlist `json:"data"`
	}
	if err := json.Unmarshal(body, &plCreated); err != nil || plCreated.Data.ID == "" {
		t.Fatalf("failed to decode created playlist: %v, body: %s", err, string(body))
	}
	playlistID := plCreated.Data.ID

	// 4. User A lists playlists -> sees 1 playlist
	res, body, err = clientA.do(http.MethodGet, "/api/v1/playlists", nil, false)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("list playlists u1: %d", res.StatusCode)
	}
	var plListA struct {
		Data []repository.Playlist `json:"data"`
	}
	if err := json.Unmarshal(body, &plListA); err != nil || len(plListA.Data) != 1 {
		t.Fatalf("expected 1 playlist for user A, got: %v", plListA.Data)
	}

	// 5. User B lists playlists -> sees 0 playlists (Multi-User Isolation)
	res, body, err = clientB.do(http.MethodGet, "/api/v1/playlists", nil, false)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("list playlists u2: %d", res.StatusCode)
	}
	var plListB struct {
		Data []repository.Playlist `json:"data"`
	}
	if err := json.Unmarshal(body, &plListB); err != nil || len(plListB.Data) != 0 {
		t.Fatalf("expected 0 playlists for user B, got: %v", plListB.Data)
	}

	// 6. User B attempts GET /playlists/{id} of User A -> 404 Not Found (Concealment)
	res, _, err = clientB.do(http.MethodGet, "/api/v1/playlists/"+playlistID, nil, false)
	if err != nil || res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for User B GET User A playlist, got: %d", res.StatusCode)
	}

	// 7. User B attempts PATCH /playlists/{id} of User A -> 404 Not Found
	res, _, err = clientB.do(http.MethodPatch, "/api/v1/playlists/"+playlistID, map[string]string{
		"name": "Hacked",
	}, true)
	if err != nil || res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for User B PATCH User A playlist, got: %d", res.StatusCode)
	}

	// 8. User B attempts DELETE /playlists/{id} of User A -> 404 Not Found
	res, _, err = clientB.do(http.MethodDelete, "/api/v1/playlists/"+playlistID, nil, true)
	if err != nil || res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for User B DELETE User A playlist, got: %d", res.StatusCode)
	}

	// 9. User B attempts POST /playlists/{id}/tracks of User A -> 404 Not Found
	res, _, err = clientB.do(http.MethodPost, "/api/v1/playlists/"+playlistID+"/tracks", map[string]string{
		"track_id": tracks[0],
	}, true)
	if err != nil || res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for User B add track to User A playlist, got: %d", res.StatusCode)
	}

	// 10. User A adds tracks 0, 1, 2 to playlist
	for _, tid := range tracks {
		res, body, err = clientA.do(http.MethodPost, "/api/v1/playlists/"+playlistID+"/tracks", map[string]string{
			"track_id": tid,
		}, true)
		if err != nil || res.StatusCode != http.StatusOK {
			t.Fatalf("user A add track %s failed: %d, body: %s", tid, res.StatusCode, string(body))
		}
	}

	// 11. User A attempts duplicate add of track 1 -> 409 Conflict
	res, body, err = clientA.do(http.MethodPost, "/api/v1/playlists/"+playlistID+"/tracks", map[string]string{
		"track_id": tracks[1],
	}, true)
	if err != nil || res.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for duplicate add, got: %d, body: %s", res.StatusCode, string(body))
	}

	// 12. User A attempts add of non-existent track -> 404 Not Found
	res, _, err = clientA.do(http.MethodPost, "/api/v1/playlists/"+playlistID+"/tracks", map[string]string{
		"track_id": "trk_bogus",
	}, true)
	if err != nil || res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found for non-existent track, got: %d", res.StatusCode)
	}

	// 13. User A gets playlist detail -> verify 3 tracks ordered 1, 2, 3
	res, body, err = clientA.do(http.MethodGet, "/api/v1/playlists/"+playlistID, nil, false)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("get playlist detail failed: %d", res.StatusCode)
	}
	var detailA struct {
		Data repository.PlaylistDetail `json:"data"`
	}
	if err := json.Unmarshal(body, &detailA); err != nil || len(detailA.Data.Tracks) != 3 {
		t.Fatalf("expected 3 tracks in detail, got: %v", detailA.Data)
	}
	if detailA.Data.Tracks[0].Position != 1 || detailA.Data.Tracks[1].Position != 2 || detailA.Data.Tracks[2].Position != 3 {
		t.Fatalf("unexpected track positions: %+v", detailA.Data.Tracks)
	}

	// 14. User A reorders tracks: [tracks[2], tracks[0], tracks[1]]
	newOrder := []string{tracks[2], tracks[0], tracks[1]}
	res, body, err = clientA.do(http.MethodPut, "/api/v1/playlists/"+playlistID+"/tracks/reorder", map[string]any{
		"track_ids": newOrder,
	}, true)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("reorder failed: %d, body: %s", res.StatusCode, string(body))
	}
	var reorderDetail struct {
		Data repository.PlaylistDetail `json:"data"`
	}
	if err := json.Unmarshal(body, &reorderDetail); err != nil || len(reorderDetail.Data.Tracks) != 3 {
		t.Fatalf("decode reorder response failed: %v", err)
	}
	if reorderDetail.Data.Tracks[0].ID != tracks[2] || reorderDetail.Data.Tracks[1].ID != tracks[0] || reorderDetail.Data.Tracks[2].ID != tracks[1] {
		t.Fatalf("reordered tracks order incorrect: %+v", reorderDetail.Data.Tracks)
	}

	// 15. User A invalid reorder (missing track) -> 400 Bad Request, zero mutation
	res, _, err = clientA.do(http.MethodPut, "/api/v1/playlists/"+playlistID+"/tracks/reorder", map[string]any{
		"track_ids": []string{tracks[2], tracks[0]},
	}, true)
	if err != nil || res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for incomplete reorder, got: %d", res.StatusCode)
	}

	// 16. User A removes track 0 (which is at position 2) -> 200 OK, remaining tracks normalized to 1, 2
	res, body, err = clientA.do(http.MethodDelete, "/api/v1/playlists/"+playlistID+"/tracks/"+tracks[0], nil, true)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("remove track failed: %d, body: %s", res.StatusCode, string(body))
	}
	var afterRemove struct {
		Data repository.PlaylistDetail `json:"data"`
	}
	if err := json.Unmarshal(body, &afterRemove); err != nil || len(afterRemove.Data.Tracks) != 2 {
		t.Fatalf("expected 2 tracks after remove, got: %v", afterRemove.Data)
	}
	if afterRemove.Data.Tracks[0].Position != 1 || afterRemove.Data.Tracks[1].Position != 2 {
		t.Fatalf("positions not normalized after remove: %+v", afterRemove.Data.Tracks)
	}

	// 17. Favorites:
	// User A favorites track 1
	res, _, err = clientA.do(http.MethodPut, "/api/v1/favorites/"+tracks[1], nil, true)
	if err != nil || res.StatusCode != http.StatusNoContent {
		t.Fatalf("favorite track failed: %d", res.StatusCode)
	}

	// Idempotent favorite
	res, _, err = clientA.do(http.MethodPut, "/api/v1/favorites/"+tracks[1], nil, true)
	if err != nil || res.StatusCode != http.StatusNoContent {
		t.Fatalf("idempotent favorite failed: %d", res.StatusCode)
	}

	// User A checks isFavorite -> true
	res, body, err = clientA.do(http.MethodGet, "/api/v1/favorites/"+tracks[1], nil, false)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("isFavorite check failed: %d", res.StatusCode)
	}
	var isFavA struct {
		Data struct {
			Favorited bool `json:"favorited"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &isFavA); err != nil || !isFavA.Data.Favorited {
		t.Fatalf("expected favorited=true for User A, got: %v", isFavA)
	}

	// User B checks isFavorite for same track -> false (User isolation)
	res, body, err = clientB.do(http.MethodGet, "/api/v1/favorites/"+tracks[1], nil, false)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("User B isFavorite check failed: %d", res.StatusCode)
	}
	var isFavB struct {
		Data struct {
			Favorited bool `json:"favorited"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &isFavB); err != nil || isFavB.Data.Favorited {
		t.Fatalf("expected favorited=false for User B, got: %v", isFavB)
	}

	// User A lists favorite IDs
	res, body, err = clientA.do(http.MethodGet, "/api/v1/favorites/ids", nil, false)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("get favorite ids failed: %d", res.StatusCode)
	}
	var favIDsA struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(body, &favIDsA); err != nil || len(favIDsA.Data) != 1 || favIDsA.Data[0] != tracks[1] {
		t.Fatalf("unexpected favorite IDs for User A: %v", favIDsA.Data)
	}

	// User B lists favorite IDs -> empty
	res, body, err = clientB.do(http.MethodGet, "/api/v1/favorites/ids", nil, false)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("User B get favorite ids failed: %d", res.StatusCode)
	}
	var favIDsB struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(body, &favIDsB); err != nil || len(favIDsB.Data) != 0 {
		t.Fatalf("expected 0 favorite IDs for User B, got: %v", favIDsB.Data)
	}

	// User A unfavorites track 1
	res, _, err = clientA.do(http.MethodDelete, "/api/v1/favorites/"+tracks[1], nil, true)
	if err != nil || res.StatusCode != http.StatusNoContent {
		t.Fatalf("unfavorite failed: %d", res.StatusCode)
	}
	res, body, err = clientA.do(http.MethodGet, "/api/v1/favorites/"+tracks[1], nil, false)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("isFavorite check after unfavorite failed: %d", res.StatusCode)
	}
	_ = json.Unmarshal(body, &isFavA)
	if isFavA.Data.Favorited {
		t.Fatalf("expected favorited=false after unfavorite")
	}

	// 18. User A deletes playlist -> 204 No Content
	res, _, err = clientA.do(http.MethodDelete, "/api/v1/playlists/"+playlistID, nil, true)
	if err != nil || res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete playlist failed: %d", res.StatusCode)
	}

	// Confirm playlist is gone (404)
	res, _, err = clientA.do(http.MethodGet, "/api/v1/playlists/"+playlistID, nil, false)
	if err != nil || res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after playlist delete, got: %d", res.StatusCode)
	}
}
