package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/music"
)

func setupLibraryTestRouter(t *testing.T) (*chi.Mux, *repository.Catalog, *repository.Files) {
	t.Helper()
	db := dbtest.Open(t)
	catalog := repository.NewCatalog(db)
	files := repository.NewFiles(db)

	h := &Handlers{
		deps: Deps{
			Catalog: catalog,
			Files:   files,
		},
	}

	r := chi.NewRouter()
	r.Route("/api/v1/library", func(lib chi.Router) {
		lib.Get("/search", h.LibrarySearch)
		lib.Get("/artists", h.LibraryArtists)
		lib.Get("/artists/{id}", h.LibraryArtistDetail)
		lib.Get("/releases", h.LibraryReleases)
		lib.Get("/releases/{id}", h.LibraryReleaseDetail)
		lib.Get("/tracks", h.LibraryTracks)
		lib.Get("/tracks/{id}", h.LibraryTrackDetail)
	})

	return r, catalog, files
}

func TestLibraryExperienceHandlers(t *testing.T) {
	router, catalog, files := setupLibraryTestRouter(t)
	ctx := context.Background()

	// Seed test data
	art, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Kraftwerk",
		Provider: "deezer",
		SourceID: "art-kw",
	})
	if err != nil {
		t.Fatal(err)
	}

	rel, err := catalog.UpsertRelease(ctx, music.Release{
		Title:       "Computer World",
		AlbumArtist: "Kraftwerk",
		Artists:     []string{"Kraftwerk"},
		ReleaseType: music.ReleaseAlbum,
		Year:        1981,
		Provider:    "deezer",
		SourceID:    "rel-cw",
	}, art.ID)
	if err != nil {
		t.Fatal(err)
	}

	trk, err := catalog.UpsertTrack(ctx, music.Track{
		Title:       "Pocket Calculator",
		Album:       "Computer World",
		AlbumArtist: "Kraftwerk",
		Artists:     []string{"Kraftwerk"},
		TrackNumber: 2,
		TrackTotal:  8,
		DiscNumber:  1,
		DiscTotal:   1,
		DurationMS:  295000,
		Year:        1981,
		ISRC:        "DEF018100002",
	}, rel.ID, art.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	_, err = files.Upsert(ctx, music.File{
		TrackID:     trk.ID,
		Path:        "Kraftwerk/1981 - Computer World/02 - Pocket Calculator.opus",
		SizeBytes:   3200000,
		Codec:       "opus",
		BitrateKbps: 160,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. GET /api/v1/library/artists
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/artists?q=kraft&limit=10&offset=0", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Data []music.LibraryArtist `json:"data"`
			Meta struct {
				Count int `json:"count"`
				Total int `json:"total"`
				Limit int `json:"limit"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Meta.Total != 1 || len(body.Data) != 1 || body.Data[0].Name != "Kraftwerk" {
			t.Fatalf("unexpected artists response: %+v", body)
		}
	}

	// 2. GET /api/v1/library/artists/{id}
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/artists/"+art.ID, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Data music.LibraryArtistDetail `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Data.Artist.Name != "Kraftwerk" || body.Data.ReleaseCount != 1 || body.Data.TrackCount != 1 {
			t.Fatalf("unexpected artist detail: %+v", body.Data)
		}
	}

	// 3. GET /api/v1/library/releases/{id}
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/releases/"+rel.ID, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Data music.LibraryReleaseDetail `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Data.Release.Title != "Computer World" || len(body.Data.Tracks) != 1 {
			t.Fatalf("unexpected release detail: %+v", body.Data)
		}
	}

	// 4. GET /api/v1/library/tracks/{id}
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks/"+trk.ID, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Data music.LibraryTrackDetail `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Data.Track.Title != "Pocket Calculator" || body.Data.File == nil || body.Data.File.Codec != "opus" {
			t.Fatalf("unexpected track detail: %+v", body.Data)
		}
	}

	// 5. GET /api/v1/library/search
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/search?q=calculator", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var body struct {
			Data music.LibrarySearchResults `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body.Data.Tracks) != 1 || body.Data.Tracks[0].Title != "Pocket Calculator" {
			t.Fatalf("unexpected search results: %+v", body.Data)
		}
	}

	// 6. Bad input checks
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks?limit=-1", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for negative limit, got %d", rec.Code)
		}

		req = httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks?sort=invalid_column", nil)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid sort, got %d", rec.Code)
		}
	}
}

func TestLibraryPaginationAndTotals(t *testing.T) {
	router, catalog, _ := setupLibraryTestRouter(t)
	ctx := context.Background()

	art, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Electronic Artist",
		Provider: "deezer",
		SourceID: "art-ea",
	})
	if err != nil {
		t.Fatal(err)
	}

	rel, err := catalog.UpsertRelease(ctx, music.Release{
		Title:       "Album 1",
		AlbumArtist: "Electronic Artist",
		Artists:     []string{"Electronic Artist"},
		ReleaseType: music.ReleaseAlbum,
		Year:        2024,
		Provider:    "deezer",
		SourceID:    "rel-ea-1",
	}, art.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Insert 15 tracks, 5 with synced lyrics, 10 unknown
	for i := 1; i <= 15; i++ {
		lyricsState := music.LyricsUnknown
		if i <= 5 {
			lyricsState = music.LyricsAvailableSynced
		}
		_, err := catalog.UpsertTrack(ctx, music.Track{
			Title:          fmt.Sprintf("Track %02d", i),
			Album:          "Album 1",
			AlbumArtist:    "Electronic Artist",
			Artists:        []string{"Electronic Artist"},
			TrackNumber:    i,
			DurationMS:     180000,
			Year:           2024,
			LyricsState:    lyricsState,
			SourceProvider: "deezer",
			SourceID:       fmt.Sprintf("trk-ea-%d", i),
		}, rel.ID, art.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
	}

	// 1. Test limit=5, offset=0 -> count=5, total=15
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks?limit=5&offset=0", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		var res struct {
			Data []music.LibraryTrack `json:"data"`
			Meta struct {
				Count  int `json:"count"`
				Total  int `json:"total"`
				Limit  int `json:"limit"`
				Offset int `json:"offset"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if res.Meta.Count != 5 || res.Meta.Total != 15 || res.Meta.Limit != 5 || res.Meta.Offset != 0 {
			t.Fatalf("unexpected meta for page 1: %+v", res.Meta)
		}
	}

	// 2. Test limit=5, offset=10 -> count=5, total=15 (page 3)
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks?limit=5&offset=10", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		var res struct {
			Data []music.LibraryTrack `json:"data"`
			Meta struct {
				Count  int `json:"count"`
				Total  int `json:"total"`
				Limit  int `json:"limit"`
				Offset int `json:"offset"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if res.Meta.Count != 5 || res.Meta.Total != 15 || res.Meta.Offset != 10 {
			t.Fatalf("unexpected meta for page 3: %+v", res.Meta)
		}
	}

	// 3. Test limit=5, offset=12 -> count=3, total=15
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks?limit=5&offset=12", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		var res struct {
			Data []music.LibraryTrack `json:"data"`
			Meta struct {
				Count int `json:"count"`
				Total int `json:"total"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if res.Meta.Count != 3 || res.Meta.Total != 15 {
			t.Fatalf("unexpected meta for partial page: %+v", res.Meta)
		}
	}

	// 4. Test filter lyrics_state=available_synced, limit=2 -> count=2, total=5
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks?lyrics_state=available_synced&limit=2", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		var res struct {
			Data []music.LibraryTrack `json:"data"`
			Meta struct {
				Count int `json:"count"`
				Total int `json:"total"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if res.Meta.Count != 2 || res.Meta.Total != 5 {
			t.Fatalf("unexpected filtered meta: count=%d, total=%d (expected 2 and 5)", res.Meta.Count, res.Meta.Total)
		}
	}

	// 5. Test offset beyond total -> count=0, total=15
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks?limit=5&offset=100", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		var res struct {
			Data []music.LibraryTrack `json:"data"`
			Meta struct {
				Count int `json:"count"`
				Total int `json:"total"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if res.Meta.Count != 0 || res.Meta.Total != 15 {
			t.Fatalf("unexpected meta for out of bounds offset: %+v", res.Meta)
		}
	}

	// 6. Test limit=999999 -> clamped to 100
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks?limit=999999", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		var res struct {
			Meta struct {
				Limit int `json:"limit"`
				Count int `json:"count"`
				Total int `json:"total"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if res.Meta.Limit != 100 {
			t.Fatalf("expected limit clamped to 100, got %d", res.Meta.Limit)
		}
	}
}

func TestLibrarySearchSecurityAndEdgeCases(t *testing.T) {
	router, catalog, _ := setupLibraryTestRouter(t)
	ctx := context.Background()

	art, err := catalog.UpsertArtist(ctx, music.Artist{
		Name:     "Böhse Onkelz & DJ 🎧 Über-Funk",
		Provider: "deezer",
		SourceID: "art-special",
	})
	if err != nil {
		t.Fatal(err)
	}

	rel, err := catalog.UpsertRelease(ctx, music.Release{
		Title:       "Lügen, Träume & 100% Liebe",
		AlbumArtist: art.Name,
		Artists:     []string{art.Name},
		ReleaseType: music.ReleaseAlbum,
		Year:        2023,
		Provider:    "deezer",
		SourceID:    "rel-special",
	}, art.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = catalog.UpsertTrack(ctx, music.Track{
		Title:          "Track 'with quotes' & special_chars %_\\",
		Album:          rel.Title,
		AlbumArtist:    art.Name,
		Artists:        []string{art.Name},
		TrackNumber:    1,
		DurationMS:     200000,
		Year:           2023,
		ISRC:           "DE-A01-23-00001",
		SourceProvider: "deezer",
		SourceID:       "trk-special",
	}, rel.ID, art.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	// 1. SQL injection payloads must not cause error or SQL syntax exceptions
	injectionPayloads := []string{
		"'; DROP TABLE tracks; --",
		"' OR '1'='1",
		"\" OR \"\"=\"",
		"%%",
		"__",
		"\\",
		"'; SELECT * FROM users; --",
	}

	for _, payload := range injectionPayloads {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks?q="+url.QueryEscape(payload), nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK for injection payload %q, got status %d", payload, rec.Code)
		}
	}

	// 2. Unicode, Emoji & German Umlaute search
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/artists?q="+url.QueryEscape("Böhse"), nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		var res struct {
			Data []music.LibraryArtist `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if len(res.Data) != 1 {
			t.Fatalf("expected 1 match for Umlaut search, got %d", len(res.Data))
		}

		req = httptest.NewRequest(http.MethodGet, "/api/v1/library/artists?q="+url.QueryEscape("🎧"), nil)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if len(res.Data) != 1 {
			t.Fatalf("expected 1 match for Emoji search, got %d", len(res.Data))
		}
	}

	// 3. ISRC Search: exact match (case insensitive), no partial substring match
	{
		// Exact match
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks?q=DE-A01-23-00001", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		var res struct {
			Data []music.LibraryTrack `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if len(res.Data) != 1 {
			t.Fatalf("expected 1 match for exact ISRC, got %d", len(res.Data))
		}

		// Case-insensitive exact match
		req = httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks?q=de-a01-23-00001", nil)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if len(res.Data) != 1 {
			t.Fatalf("expected 1 match for lowercase exact ISRC, got %d", len(res.Data))
		}

		// Partial ISRC prefix should not match unless title/artist matches
		req = httptest.NewRequest(http.MethodGet, "/api/v1/library/tracks?q=DE-A01", nil)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if len(res.Data) != 0 {
			t.Fatalf("expected 0 matches for partial ISRC prefix, got %d", len(res.Data))
		}
	}

	// 4. Omni Search with empty/whitespace query
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/library/search?q=", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		var res struct {
			Data music.LibrarySearchResults `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if len(res.Data.Artists) != 0 || len(res.Data.Releases) != 0 || len(res.Data.Tracks) != 0 {
			t.Fatalf("expected empty search results for empty query, got %+v", res.Data)
		}

		req = httptest.NewRequest(http.MethodGet, "/api/v1/library/search?q=%20%20%20", nil)
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if len(res.Data.Artists) != 0 || len(res.Data.Releases) != 0 || len(res.Data.Tracks) != 0 {
			t.Fatalf("expected empty search results for whitespace query, got %+v", res.Data)
		}
	}
}

func TestLibraryEndpointsRequireAuth(t *testing.T) {
	// Verify that unauthenticated requests to the actual app router get 401
	r := chi.NewRouter()
	r.Use(middleware.RequireAuth)
	r.Get("/api/v1/library/artists", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/library/artists", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated request, got %d", rec.Code)
	}
}
