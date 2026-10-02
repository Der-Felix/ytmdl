package handlers_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"testing"
)

func TestLibraryToolsAuthOwnershipAndAtomicWrites(t *testing.T) {
	_, admin, user, ids := setupPlaylistsE2ETest(t)
	patch := map[string]any{"track_ids": []string{ids[0], ids[1]}, "patch": map[string]any{"album": "Manual album", "year": 2026}}
	var raw []byte
	res, _, err := admin.do(http.MethodPatch, "/api/v1/library/tracks/metadata", patch, false)
	if err != nil || res.StatusCode != http.StatusForbidden {
		t.Fatal("CSRF missing write accepted")
	}
	res, _, err = user.do(http.MethodPatch, "/api/v1/library/tracks/metadata", patch, true)
	if err != nil || res.StatusCode != http.StatusForbidden {
		t.Fatal("user metadata write accepted")
	}
	res, _, err = admin.do(http.MethodPatch, "/api/v1/library/tracks/metadata", patch, true)
	if err != nil || res.StatusCode != http.StatusNoContent {
		t.Fatal("admin patch failed", err, res)
	}
	res, raw, err = user.do(http.MethodGet, "/api/v1/library/tracks/"+ids[0], nil, false)
	if err != nil || res.StatusCode != 200 {
		t.Fatal("read failed")
	}
	var track struct {
		Data struct {
			Track struct {
				Album string
				Year  int
			}
		}
	}
	if err = json.Unmarshal(raw, &track); err != nil || track.Data.Track.Album != "Manual album" || track.Data.Track.Year != 2026 {
		t.Fatal("override missing", err, string(raw))
	}
	event := map[string]any{"track_id": ids[0], "event_id": "event-e2e-000000000001"}
	for i := 0; i < 2; i++ {
		res, _, err = user.do(http.MethodPost, "/api/v1/history", event, true)
		if err != nil || res.StatusCode != 204 {
			t.Fatal("history write failed")
		}
	}
	res, raw, err = user.do(http.MethodGet, "/api/v1/history", nil, false)
	var history struct {
		Data []struct {
			PlayCount int `json:"play_count"`
		}
	}
	if err != nil || res.StatusCode != 200 || json.Unmarshal(raw, &history) != nil || len(history.Data) != 1 || history.Data[0].PlayCount != 1 {
		t.Fatal("history not idempotent")
	}
	_, raw, _ = admin.do(http.MethodGet, "/api/v1/history", nil, false)
	if json.Unmarshal(raw, &history) != nil || len(history.Data) != 0 {
		t.Fatal("private history exposed")
	}
	rules := map[string]any{"name": "Dynamic", "smart_rules": map[string]any{"sort": "title", "limit": 50, "favorites": true}}
	res, raw, err = user.do(http.MethodPost, "/api/v1/playlists", rules, true)
	var pl struct{ Data struct{ ID string } }
	if err != nil || res.StatusCode != 201 || json.Unmarshal(raw, &pl) != nil {
		t.Fatal("smart creation failed")
	}
	path := "/api/v1/playlists/" + pl.Data.ID
	res, _, _ = user.do(http.MethodPut, path+"/rules", map[string]any{}, true)
	if res.StatusCode != 400 {
		t.Fatal("missing rules cleared playlist")
	}
	res, _, _ = admin.do(http.MethodPut, path+"/rules", map[string]any{"smart_rules": nil}, true)
	if res.StatusCode != 404 {
		t.Fatal("foreign rules changed")
	}
	res, _, _ = user.do(http.MethodPost, path+"/tracks/bulk", map[string]any{"track_ids": ids}, true)
	if res.StatusCode != 400 {
		t.Fatal("smart membership changed")
	}
	res, _, _ = user.do(http.MethodPut, "/api/v1/library/artists/art_e2e/artwork", map[string]any{}, true)
	if res.StatusCode != 403 {
		t.Fatal("non-admin image upload accepted")
	}
	res, _, _ = admin.do(http.MethodPut, "/api/v1/library/artists/art_e2e/artwork", map[string]any{}, true)
	if res.StatusCode != 400 {
		t.Fatal("invalid image accepted")
	}
	res, _, _ = user.do(http.MethodDelete, "/api/v1/history", nil, false)
	if res.StatusCode != 403 {
		t.Fatal("history CSRF missing accepted")
	}
}

func TestCustomArtworkUploadDownloadAndDelete(t *testing.T) {
	_, admin, _, _ := setupPlaylistsE2ETest(t)
	var raw bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 12, 12))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	if err := png.Encode(&raw, img); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("image", "fixture.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(raw.Bytes())
	_ = writer.Close()
	path := "/api/v1/library/releases/rel_e2e/artwork"
	req, err := http.NewRequest(http.MethodPut, admin.baseURL+path, &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", admin.csrfToken)
	res, err := admin.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 204 {
		t.Fatal("valid upload failed", res.StatusCode)
	}
	res, data, err := admin.do(http.MethodGet, path, nil, false)
	if err != nil || res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" || res.Header.Get("ETag") == "" {
		t.Fatal("custom artwork not served", err)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width != 12 {
		t.Fatal("stored image invalid")
	}
	res, _, err = admin.do(http.MethodHead, "/api/v1/library/tracks/trk_e2e_1/artwork", nil, false)
	if err != nil || res.StatusCode != 200 {
		t.Fatal("track did not inherit custom release cover")
	}
	res, _, err = admin.do(http.MethodDelete, path, nil, true)
	if err != nil || res.StatusCode != 204 {
		t.Fatal("custom removal failed")
	}
	res, _, _ = admin.do(http.MethodGet, path, nil, false)
	if res.StatusCode != 404 {
		t.Fatal("removed image still served")
	}
}
