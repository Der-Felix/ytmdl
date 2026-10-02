package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestArtistGenresAuthenticationAndFiltering(t *testing.T) {
	srv, admin, user, _ := setupPlaylistsE2ETest(t)
	response, err := http.Get(srv.URL + "/api/v1/library/genres")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal("unauthenticated genres exposed")
	}
	path := "/api/v1/library/artists/art_e2e/genres"
	body := map[string]any{"genres": []string{"Electronic"}}
	res, _, err := admin.do(http.MethodPut, path, body, false)
	if err != nil || res.StatusCode != http.StatusForbidden {
		t.Fatal("CSRF-less write accepted")
	}
	res, _, err = user.do(http.MethodPut, path, body, true)
	if err != nil || res.StatusCode != http.StatusForbidden {
		t.Fatal("non-admin genre write accepted")
	}
	res, _, err = admin.do(http.MethodPut, path, body, true)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("admin write: %v %v", res, err)
	}
	res, _, err = admin.do(http.MethodPut, path, map[string]any{}, true)
	if err != nil || res.StatusCode != http.StatusBadRequest {
		t.Fatal("missing genres silently cleared metadata")
	}
	res, raw, err := user.do(http.MethodGet, "/api/v1/library/tracks?genre=Electronic&limit=1&offset=1", nil, false)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("filter: %v %v", res, err)
	}
	var envelope struct {
		Data []any `json:"data"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil || len(envelope.Data) != 1 || envelope.Meta.Total != 3 {
		t.Fatalf("paginated shared filter: %v %v", envelope, err)
	}
	res, _, err = user.do(http.MethodGet, "/api/v1/library/tracks/trk_e2e_1/artwork", nil, false)
	if err != nil || res.StatusCode != http.StatusNotFound {
		t.Fatal("missing local artwork not classified as missing")
	}
}
