package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The request is checked before the catalog is asked, so these need no database.
func TestLibraryArtistTopTracksRejectsInvalidRequests(t *testing.T) {
	h := &Handlers{}
	for name, tc := range map[string]struct {
		id, query string
	}{
		"missing id":       {"", ""},
		"zero limit":       {"art_1", "?limit=0"},
		"non-number limit": {"art_1", "?limit=ten"},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/library/artists/"+tc.id+"/top-tracks"+tc.query, nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tc.id)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			rec := httptest.NewRecorder()
			h.LibraryArtistTopTracks(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
		})
	}
}
