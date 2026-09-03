package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/auth"
)

func TestProfileHandlers(t *testing.T) {
	h, authService := newTestAuthHandlers(t)

	// Setup initial user
	setupRes, err := authService.Setup(context.Background(), auth.SetupRequest{
		Username:    "charlie",
		DisplayName: "Charlie Brown",
		Password:    "password_123",
	}, "127.0.0.1", "Session 1")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	user, sess, err := authService.VerifySession(context.Background(), setupRes.SessionToken)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	ctx := middleware.ContextWithUser(context.Background(), user)
	ctx = middleware.ContextWithSession(ctx, sess)

	// GET /profile
	recGet := httptest.NewRecorder()
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/profile", nil).WithContext(ctx)
	h.GetProfile(recGet, reqGet)

	if recGet.Code != http.StatusOK {
		t.Fatalf("get profile code = %d, want 200", recGet.Code)
	}

	// PATCH /profile
	recPatch := httptest.NewRecorder()
	reqPatch := httptest.NewRequest(http.MethodPatch, "/api/v1/profile", bytes.NewBufferString(`{"display_name":"Charles Brown"}`)).WithContext(ctx)
	reqPatch.Header.Set("Content-Type", "application/json")
	h.UpdateProfile(recPatch, reqPatch)

	if recPatch.Code != http.StatusOK {
		t.Fatalf("update profile code = %d, want 200", recPatch.Code)
	}

	// POST /profile/password
	recPass := httptest.NewRecorder()
	reqPass := httptest.NewRequest(http.MethodPost, "/api/v1/profile/password", bytes.NewBufferString(`{"current_password":"password_123","new_password":"new_password_456"}`)).WithContext(ctx)
	reqPass.Header.Set("Content-Type", "application/json")
	h.ChangePassword(recPass, reqPass)

	if recPass.Code != http.StatusNoContent {
		t.Fatalf("change password code = %d, want 204", recPass.Code)
	}

	// GET /profile/sessions
	recSessions := httptest.NewRecorder()
	reqSessions := httptest.NewRequest(http.MethodGet, "/api/v1/profile/sessions", nil).WithContext(ctx)
	h.ListSessions(recSessions, reqSessions)

	if recSessions.Code != http.StatusOK {
		t.Fatalf("list sessions code = %d, want 200", recSessions.Code)
	}

	var sessList struct {
		Data []auth.SessionSummary `json:"data"`
	}
	if err := json.NewDecoder(recSessions.Body).Decode(&sessList); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(sessList.Data) != 1 || !sessList.Data[0].IsCurrent {
		t.Fatalf("expected 1 current session, got %+v", sessList.Data)
	}

	// DELETE /profile/sessions/{id}
	rCtx := chi.NewRouteContext()
	rCtx.URLParams.Add("id", sess.ID)
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/v1/profile/sessions/"+sess.ID, nil).WithContext(context.WithValue(ctx, chi.RouteCtxKey, rCtx))
	recDel := httptest.NewRecorder()
	h.RevokeSession(recDel, reqDel)

	if recDel.Code != http.StatusNoContent {
		t.Fatalf("revoke session code = %d, want 204", recDel.Code)
	}
}
