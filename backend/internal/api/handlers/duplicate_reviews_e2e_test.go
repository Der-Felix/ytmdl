package handlers_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestDuplicateReviewAuthenticationCSRFAndExplicitConsent(t *testing.T) {
	srv, admin, user, _ := setupPlaylistsE2ETest(t)
	res, err := http.Get(srv.URL + "/api/v1/library/duplicates?review=open")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatal("anonymous catalog exposed")
	}
	review := map[string]any{"group_key": strings.Repeat("a", 64), "fingerprint": strings.Repeat("b", 64), "outcome": "preferred", "preferred_track_id": "trk_e2e_1"}
	for _, path := range []string{"/api/v1/library/duplicates/review", "/api/v1/library/duplicates/remove"} {
		res, _, err = admin.do(http.MethodPost, path, review, false)
		if err != nil || res.StatusCode != http.StatusForbidden {
			t.Fatal("CSRF-less write accepted", path)
		}
	}
	res, _, err = user.do(http.MethodPost, "/api/v1/library/duplicates/remove", review, true)
	if err != nil || res.StatusCode != http.StatusForbidden {
		t.Fatal("user deletion accepted")
	}
	res, _, err = admin.do(http.MethodPost, "/api/v1/library/duplicates/remove", review, true)
	if err != nil || res.StatusCode != http.StatusBadRequest {
		t.Fatal("unconfirmed deletion accepted")
	}
	res, _, err = user.do(http.MethodPost, "/api/v1/library/duplicates/review", review, true)
	if err != nil || res.StatusCode != http.StatusConflict {
		t.Fatal("nonexistent/stale review accepted")
	}
	res, _, err = user.do(http.MethodGet, "/api/v1/library/duplicates?after=invalid", nil, false)
	if err != nil || res.StatusCode != http.StatusBadRequest {
		t.Fatal("invalid cursor accepted")
	}
	res, _, err = admin.do(http.MethodDelete, "/api/v1/library/duplicates/review/"+strings.Repeat("a", 64), nil, false)
	if err != nil || res.StatusCode != http.StatusForbidden {
		t.Fatal("CSRF-less reset accepted")
	}
}
