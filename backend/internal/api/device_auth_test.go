package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"ytdm/backend/internal/api"
	"ytdm/backend/internal/api/handlers"
	"ytdm/backend/internal/api/middleware"
	"ytdm/backend/internal/auth"
	"ytdm/backend/internal/database/dbtest"
	"ytdm/backend/internal/database/repository"
)

func TestDeviceRoutesRequireCSRFAndAuthenticatedConfirmation(t *testing.T) {
	db := dbtest.Open(t)
	limiter := auth.NewLimiter(5, time.Minute)
	t.Cleanup(limiter.Close)
	svc := auth.NewService(repository.NewUsers(db), repository.NewSessions(db), limiter, nil)
	result, err := svc.Setup(context.Background(), auth.SetupRequest{Username: "fixture_user", Password: "fixture_password_123"}, "127.0.0.1", "FixtureBrowser")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := api.NewRouter(api.RouterOptions{Auth: svc, Handlers: handlers.NewForTest(handlers.Deps{Auth: svc})})
	if err != nil {
		t.Fatal(err)
	}
	call := func(path, body string, csrf, authenticated bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth"+path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if csrf {
			req.AddCookie(&http.Cookie{Name: middleware.CSRFCookieName, Value: "fixture-csrf"})
			req.Header.Set(middleware.CSRFHeaderName, "fixture-csrf")
		}
		if authenticated {
			req.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: result.SessionToken})
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if got := call("/device", `{"device_name":"Apple TV"}`, false, false).Code; got != 403 {
		t.Fatalf("start without CSRF: %d", got)
	}
	start := call("/device", `{"device_name":"Apple TV"}`, true, false)
	if start.Code != 201 || start.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("start response contract")
	}
	var code struct {
		Data auth.DeviceStart `json:"data"`
	}
	if err := json.Unmarshal(start.Body.Bytes(), &code); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"user_code": code.Data.UserCode})
	for _, path := range []string{"/device/preview", "/device/confirm"} {
		if got := call(path, string(body), true, false).Code; got != 401 {
			t.Fatalf("anonymous %s: %d", path, got)
		}
		if got := call(path, string(body), false, true).Code; got != 403 {
			t.Fatalf("no CSRF %s: %d", path, got)
		}
	}
	if got := call("/device/preview", string(body), true, true).Code; got != 200 {
		t.Fatalf("preview %d", got)
	}
	if got := call("/device/confirm", string(body), true, true).Code; got != 204 {
		t.Fatalf("confirm %d", got)
	}
	pollBody, _ := json.Marshal(map[string]string{"device_code": code.Data.DeviceCode})
	if got := call("/device/poll", string(pollBody), false, false).Code; got != 403 {
		t.Fatalf("poll no CSRF: %d", got)
	}
	poll := call("/device/poll", string(pollBody), true, false)
	if poll.Code != 200 || poll.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("exchange response contract")
	}
	found := false
	for _, cookie := range poll.Result().Cookies() {
		if cookie.Name == middleware.SessionCookieName {
			found = cookie.HttpOnly && cookie.Value != result.SessionToken
		}
	}
	if !found {
		t.Fatal("independent HttpOnly session cookie missing")
	}
	if bytes.Contains(poll.Body.Bytes(), []byte("SessionToken")) || bytes.Contains(poll.Body.Bytes(), []byte(code.Data.DeviceCode)) {
		t.Fatal("secret included in exchange JSON")
	}
}
