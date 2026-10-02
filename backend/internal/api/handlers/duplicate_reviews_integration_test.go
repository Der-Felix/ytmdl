package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
	"ytdm/backend/internal/library"
	"ytdm/backend/internal/storage"
)

func TestConfirmedDuplicateRemovalPreservesWinnerAndUnselectedMemberships(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	root := t.TempDir()
	cat := repository.NewCatalog(db)
	files := repository.NewFiles(db)
	_, err := db.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,role,created_at,updated_at) VALUES('reviewer','fixture','hash','admin',now(),now());
 INSERT INTO tracks(id,title,album_artist,identity_key,created_at,updated_at) SELECT 'version-'||n,'Same','Artist','identity-'||n,now(),now() FROM generate_series(1,3)n;
 INSERT INTO files(id,track_id,path,size_bytes,created_at,updated_at) SELECT 'file-'||id,id,id||'.opus',7,now(),now() FROM tracks;
 INSERT INTO favorite_tracks(user_id,track_id,created_at) SELECT 'reviewer',id,now() FROM tracks;
 INSERT INTO playlists(id,user_id,name,created_at,updated_at) VALUES('fixture-list','reviewer','Keep order',now(),now());
 INSERT INTO playlist_tracks(playlist_id,track_id,position,added_at) SELECT 'fixture-list',id,row_number() OVER(ORDER BY id),now() FROM tracks;`)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"version-1", "version-2", "version-3"} {
		if err = os.WriteFile(filepath.Join(root, id+".opus"), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	lib, err := storage.NewLibrary(root)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := library.NewService(library.ServiceOptions{Library: lib, Catalog: cat, Files: files, Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()
	h := NewForTest(Deps{Catalog: cat, Files: files, LibraryService: svc})
	groups, err := cat.DuplicateGroupsForUser(ctx, "reviewer", 0, false, "")
	if err != nil || len(groups) != 1 {
		t.Fatal(err)
	}
	review := repository.DuplicateReview{GroupKey: groups[0].Key, Fingerprint: groups[0].Fingerprint, Outcome: "preferred", PreferredTrackID: "version-2"}
	request := func(handler http.HandlerFunc, body any) *httptest.ResponseRecorder {
		raw, e := json.Marshal(body)
		if e != nil {
			t.Fatal(e)
		}
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(middleware.ContextWithUser(ctx, &auth.User{ID: "reviewer", Role: auth.RoleAdmin}))
		w := httptest.NewRecorder()
		handler(w, req)
		return w
	}
	if w := request(h.SaveDuplicateReview, review); w.Code != 204 {
		t.Fatal("review save", w.Code, w.Body.String())
	}
	body := map[string]any{"group_key": review.GroupKey, "fingerprint": review.Fingerprint, "outcome": "preferred", "preferred_track_id": "version-2", "remove_track_ids": []string{"version-1"}}
	if w := request(h.RemoveDuplicateVersions, body); w.Code != 400 {
		t.Fatal("consent missing", w.Code)
	}
	if _, err = os.Stat(filepath.Join(root, "version-1.opus")); err != nil {
		t.Fatal("unconfirmed file touched")
	}
	body["confirmed"] = true
	w := request(h.RemoveDuplicateVersions, body)
	if w.Code != 200 {
		t.Fatal("confirmed delete", w.Code, w.Body.String())
	}
	var result struct {
		Data struct {
			Deleted []string `json:"deleted_track_ids"`
			Failed  string   `json:"failed_track_id"`
		}
	}
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Data.Deleted) != 1 || result.Data.Deleted[0] != "version-1" || result.Data.Failed != "" {
		t.Fatal("result", err)
	}
	for _, id := range []string{"version-2", "version-3"} {
		if _, err = os.Stat(filepath.Join(root, id+".opus")); err != nil {
			t.Fatal("retained file changed", err)
		}
	}
	var n int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM favorite_tracks`).Scan(&n); err != nil || n != 2 {
		t.Fatal("favorites cleanup", n, err)
	}
	var pos int
	if err = db.QueryRowContext(ctx, `SELECT position FROM playlist_tracks WHERE track_id='version-2'`).Scan(&pos); err != nil || pos != 1 {
		t.Fatal("retained relative ordering changed", err)
	}
	if w = request(h.RemoveDuplicateVersions, body); w.Code != 409 {
		t.Fatal("stale replay accepted", w.Code)
	}
}
