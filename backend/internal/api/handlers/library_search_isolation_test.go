package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ytdm/backend/internal/api/handlers"
	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/music"
)

func TestLibrarySearchStrictLocalIsolation(t *testing.T) {
	db := dbtest.Open(t)
	catalog := repository.NewCatalog(db)
	ctx := context.Background()
	now := time.Now().UTC()

	// Seed one artist, release, track
	art, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Kraftwerk",
		Provider: "spotify",
		SourceID: "kw_iso_1",
	})
	if err != nil {
		t.Fatal(err)
	}

	rel, err := catalog.UpsertRelease(ctx, music.Release{
		Title:       "Computerwelt",
		AlbumArtist: "Kraftwerk",
		Artists:     []string{"Kraftwerk"},
		ReleaseType: music.ReleaseAlbum,
		Year:        1981,
		Provider:    "spotify",
		SourceID:    "rel_iso_1",
	}, art.ID)
	if err != nil {
		t.Fatal(err)
	}

	trk, err := catalog.UpsertTrack(ctx, music.Track{
		Title:       "Computerwelt",
		Album:       "Computerwelt",
		AlbumArtist: "Kraftwerk",
		Artists:     []string{"Kraftwerk"},
		Year:        1981,
	}, rel.ID, art.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Handlers constructed with nil Registry, nil Jobs, nil Discography, nil Resolver.
	// If any search or track filter triggers external discovery, acquisition, or jobs,
	// it will panic with nil dereference.
	h := handlers.NewForTest(handlers.Deps{
		Catalog:       catalog,
		Registry:      nil,
		Jobs:          nil,
		Discography:   nil,
		Resolver:      nil,
		Subscriptions: nil,
	})

	// 1. Test GET /library/search with query
	req := httptest.NewRequest(http.MethodGet, "/library/search?q=Computer", nil)
	w := httptest.NewRecorder()
	h.LibrarySearch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from LibrarySearch, got %d: %s", w.Code, w.Body.String())
	}

	var searchResp struct {
		Data struct {
			Artists  []music.LibraryArtist  `json:"artists"`
			Releases []music.LibraryRelease `json:"releases"`
			Tracks   []music.LibraryTrack   `json:"tracks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &searchResp); err != nil {
		t.Fatalf("failed to decode search response: %v", err)
	}
	if len(searchResp.Data.Releases) != 1 || searchResp.Data.Releases[0].ID != rel.ID {
		t.Errorf("expected release %s in search results, got %+v", rel.ID, searchResp.Data.Releases)
	}
	if len(searchResp.Data.Tracks) != 1 || searchResp.Data.Tracks[0].ID != trk.ID {
		t.Errorf("expected track %s in search results, got %+v", trk.ID, searchResp.Data.Tracks)
	}

	// 2. Test GET /library/tracks with query, year, sort=relevance
	req = httptest.NewRequest(http.MethodGet, "/library/tracks?q=Computer&year=1981&sort=relevance&order=asc", nil)
	w = httptest.NewRecorder()
	h.LibraryTracks(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from LibraryTracks, got %d: %s", w.Code, w.Body.String())
	}

	var tracksResp struct {
		Data []music.LibraryTrack `json:"data"`
		Meta struct {
			Count int `json:"count"`
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &tracksResp); err != nil {
		t.Fatalf("failed to decode tracks response: %v", err)
	}
	if tracksResp.Meta.Total != 1 || len(tracksResp.Data) != 1 || tracksResp.Data[0].ID != trk.ID {
		t.Errorf("expected 1 track in filtered results, got %+v", tracksResp)
	}

	// 3. Validation: query > 200 characters rejected with 400 Bad Request
	longQ := strings.Repeat("x", 201)
	req = httptest.NewRequest(http.MethodGet, "/library/search?q="+longQ, nil)
	w = httptest.NewRecorder()
	h.LibrarySearch(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for query > 200 chars in search, got %d", w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/library/tracks?q="+longQ, nil)
	w = httptest.NewRecorder()
	h.LibraryTracks(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for query > 200 chars in tracks, got %d", w.Code)
	}

	// 4. Favorite filter requires authentication
	req = httptest.NewRequest(http.MethodGet, "/library/tracks?favorite=true", nil)
	w = httptest.NewRecorder()
	h.LibraryTracks(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized when favorite=true without user context, got %d", w.Code)
	}

	// 5. Authenticated user with favorite
	testUser := &auth.User{
		ID:       "u_iso_test",
		Username: "isotest",
		Role:     "user",
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO users (id, username, password_hash, role, enabled, created_at, updated_at)
		VALUES ($1, $2, 'hash', 'user', true, $3, $3)
		ON CONFLICT (id) DO NOTHING
	`, testUser.ID, testUser.Username, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO favorite_tracks (user_id, track_id, created_at)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, testUser.ID, trk.ID, now)
	if err != nil {
		t.Fatal(err)
	}

	authCtx := middleware.ContextWithUser(context.Background(), testUser)
	req = httptest.NewRequest(http.MethodGet, "/library/tracks?favorite=true", nil).WithContext(authCtx)
	w = httptest.NewRecorder()
	h.LibraryTracks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for authenticated favorite filter, got %d: %s", w.Code, w.Body.String())
	}
	var favTracksResp struct {
		Data []music.LibraryTrack `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &favTracksResp); err != nil {
		t.Fatal(err)
	}
	if len(favTracksResp.Data) != 1 || favTracksResp.Data[0].ID != trk.ID {
		t.Errorf("expected 1 favorited track, got %+v", favTracksResp.Data)
	}
}
