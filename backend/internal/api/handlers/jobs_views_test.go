package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ytdm/backend/internal/api/handlers"
)

func TestJobListRejectsUnknownViewBeforeDatabaseAccess(t *testing.T) {
	h := handlers.NewForTest(handlers.Deps{})
	w := httptest.NewRecorder()
	h.ListJobs(w, httptest.NewRequest(http.MethodGet, "/api/v1/jobs?view=not-a-view", nil))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "INVALID_REQUEST") {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
	}
}
