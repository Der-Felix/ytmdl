package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"ytdm/backend/internal/auth"
)

func TestAdminUsersHandlers(t *testing.T) {
	h, authService := newTestAuthHandlers(t)

	// Setup initial admin
	setupRes, err := authService.Setup(context.Background(), auth.SetupRequest{
		Username:    "main_admin",
		DisplayName: "Main Admin",
		Password:    "password_123",
	}, "127.0.0.1", "Session")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// 1. POST /users - Create normal user
	createBody := `{"username":"Dave_Dev","display_name":"Dave","password":"dave_password_123","role":"user"}`
	recCreate := httptest.NewRecorder()
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewBufferString(createBody))
	reqCreate.Header.Set("Content-Type", "application/json")
	h.CreateUser(recCreate, reqCreate)

	if recCreate.Code != http.StatusCreated {
		t.Fatalf("create user code = %d, want 201; body = %s", recCreate.Code, recCreate.Body.String())
	}

	var createdRes struct {
		Data auth.UserSummary `json:"data"`
	}
	if err := json.NewDecoder(recCreate.Body).Decode(&createdRes); err != nil {
		t.Fatalf("decode: %v", err)
	}
	daveID := createdRes.Data.ID

	// 2. GET /users - List users
	recList := httptest.NewRecorder()
	reqList := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	h.ListUsers(recList, reqList)

	if recList.Code != http.StatusOK {
		t.Fatalf("list users code = %d, want 200", recList.Code)
	}

	var listRes struct {
		Data []auth.UserSummary `json:"data"`
	}
	if err := json.NewDecoder(recList.Body).Decode(&listRes); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listRes.Data) != 2 {
		t.Fatalf("expected 2 users in list, got %d", len(listRes.Data))
	}

	// 3. GET /users/{id} - Get Dave
	rCtx := chi.NewRouteContext()
	rCtx.URLParams.Add("id", daveID)
	reqGet := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+daveID, nil).WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rCtx))
	recGet := httptest.NewRecorder()
	h.GetUser(recGet, reqGet)

	if recGet.Code != http.StatusOK {
		t.Fatalf("get user code = %d, want 200", recGet.Code)
	}

	// 4. PATCH /users/{id} - Disable Dave
	reqUpdate := httptest.NewRequest(http.MethodPatch, "/api/v1/users/"+daveID, bytes.NewBufferString(`{"enabled":false}`)).WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rCtx))
	reqUpdate.Header.Set("Content-Type", "application/json")
	recUpdate := httptest.NewRecorder()
	h.UpdateUser(recUpdate, reqUpdate)

	if recUpdate.Code != http.StatusOK {
		t.Fatalf("update user code = %d, want 200", recUpdate.Code)
	}

	// 5. POST /users/{id}/reset-password
	reqReset := httptest.NewRequest(http.MethodPost, "/api/v1/users/"+daveID+"/reset-password", bytes.NewBufferString(`{"password":"dave_new_super_pw_123"}`)).WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rCtx))
	reqReset.Header.Set("Content-Type", "application/json")
	recReset := httptest.NewRecorder()
	h.ResetPassword(recReset, reqReset)

	if recReset.Code != http.StatusNoContent {
		t.Fatalf("reset password code = %d, want 204", recReset.Code)
	}

	// 6. DELETE /users/{id} - Delete Dave
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+daveID, nil).WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rCtx))
	recDel := httptest.NewRecorder()
	h.DeleteUser(recDel, reqDel)

	if recDel.Code != http.StatusNoContent {
		t.Fatalf("delete user code = %d, want 204", recDel.Code)
	}

	// 7. Attempt to delete last admin (main_admin) -> must fail with 409 Conflict
	rCtxAdmin := chi.NewRouteContext()
	rCtxAdmin.URLParams.Add("id", setupRes.User.ID)
	reqDelAdmin := httptest.NewRequest(http.MethodDelete, "/api/v1/users/"+setupRes.User.ID, nil).WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, rCtxAdmin))
	recDelAdmin := httptest.NewRecorder()
	h.DeleteUser(recDelAdmin, reqDelAdmin)

	if recDelAdmin.Code != http.StatusConflict {
		t.Fatalf("delete last admin code = %d, want 409 Conflict", recDelAdmin.Code)
	}
}
