package handlers_test

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
	"ytdm/backend/internal/jobs"
	"ytdm/backend/internal/music"
)

type jobAuthTestEnv struct {
	router     http.Handler
	adminToken string
	userToken  string
	csrfToken  string
	jobID      string
	jobID2     string
	itemID     string
	jobsRepo   *repository.Jobs
}

func setupJobAuthTestEnv(t *testing.T) *jobAuthTestEnv {
	t.Helper()
	db := dbtest.Open(t)
	usersRepo := repository.NewUsers(db)
	sessionsRepo := repository.NewSessions(db)
	limiter := auth.NewLimiter(20, 5*time.Minute)
	t.Cleanup(limiter.Close)
	authService := auth.NewService(usersRepo, sessionsRepo, limiter, nil)
	jobsRepo := repository.NewJobs(db)
	mgr := jobs.NewManagerForTest(jobsRepo, nil)

	h := handlers.NewForTest(handlers.Deps{
		Auth: authService,
		Jobs: mgr,
	})

	router, err := api.NewRouter(api.RouterOptions{
		Handlers: h,
		Auth:     authService,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	ctx := context.Background()

	// 1. Create Admin
	setupRes, err := authService.Setup(ctx, auth.SetupRequest{
		Username:    "admin_user",
		DisplayName: "Admin",
		Password:    "password123!",
	}, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("setup admin: %v", err)
	}

	// 2. Create Standard User
	_, err = authService.CreateUser(ctx, auth.CreateUserRequest{
		Username:    "standard_user",
		DisplayName: "Standard User",
		Password:    "password123!",
		Role:        auth.RoleUser,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	loginRes, err := authService.Login(ctx, auth.LoginRequest{
		Username: "standard_user",
		Password: "password123!",
	}, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("login standard user: %v", err)
	}

	// 3. Create a test job and item
	testJob := &jobs.Job{
		Type:             jobs.TypeTrack,
		Status:           jobs.StatusFailed,
		Priority:         jobs.PriorityNormal,
		Label:            "Auth Test Job",
		MetadataProvider: "spotify",
		MediaProvider:    "youtube",
		TargetID:         "target_1",
		Options:          jobs.DefaultOptions(),
		Total:            1,
		Failed:           1,
	}
	if err := jobsRepo.Create(ctx, testJob); err != nil {
		t.Fatalf("create job: %v", err)
	}
	testItemID := music.NewID()
	testItem2ID := music.NewID()
	items := []jobs.Item{
		{
			ID:          testItemID,
			JobID:       testJob.ID,
			Position:    1,
			Status:      jobs.ItemFailed,
			Label:       "Test Item 1",
			Attempts:    3,
			MaxAttempts: 3,
			ErrorCode:   "DOWNLOAD_FAILED",
		},
	}
	if err := jobsRepo.AddItems(ctx, testJob.ID, items); err != nil {
		t.Fatalf("add items: %v", err)
	}

	// Job 2 for single item retry
	testJob2 := &jobs.Job{
		Type:             jobs.TypeTrack,
		Status:           jobs.StatusFailed,
		Priority:         jobs.PriorityNormal,
		Label:            "Auth Test Job 2",
		MetadataProvider: "spotify",
		MediaProvider:    "youtube",
		TargetID:         "target_2",
		Options:          jobs.DefaultOptions(),
		Total:            1,
		Failed:           1,
	}
	if err := jobsRepo.Create(ctx, testJob2); err != nil {
		t.Fatalf("create job 2: %v", err)
	}
	items2 := []jobs.Item{

		{
			ID:          testItem2ID,
			JobID:       testJob2.ID,
			Position:    1,
			Status:      jobs.ItemFailed,
			Label:       "Test Item 2",
			Attempts:    3,
			MaxAttempts: 3,
			ErrorCode:   "DOWNLOAD_FAILED",
		},
	}
	if err := jobsRepo.AddItems(ctx, testJob2.ID, items2); err != nil {
		t.Fatalf("add items 2: %v", err)
	}

	csrfToken, err := auth.GenerateCSRFToken()
	if err != nil {
		t.Fatalf("generate CSRF: %v", err)
	}

	return &jobAuthTestEnv{
		router:     router,
		adminToken: setupRes.SessionToken,
		userToken:  loginRes.SessionToken,
		csrfToken:  csrfToken,
		jobID:      testJob.ID,
		jobID2:     testJob2.ID,
		itemID:     testItem2ID,
		jobsRepo:   jobsRepo,
	}
}

func TestJobControlAuthMatrix(t *testing.T) {
	env := setupJobAuthTestEnv(t)

	type testCase struct {
		name      string
		method    string
		path      string
		body      []byte
		adminOnly bool
	}

	cases := []testCase{
		{
			name:   "PATCH Priority",
			method: http.MethodPatch,
			path:   "/api/v1/jobs/" + env.jobID,
			body:   []byte(`{"priority":"high"}`),
		},
		{
			name:   "POST Pause",
			method: http.MethodPost,
			path:   "/api/v1/jobs/" + env.jobID + "/pause",
			body:   nil,
		},
		{
			name:   "POST Resume",
			method: http.MethodPost,
			path:   "/api/v1/jobs/" + env.jobID + "/resume",
			body:   nil,
		},
		{
			name:   "POST Retry Failed",
			method: http.MethodPost,
			path:   "/api/v1/jobs/" + env.jobID + "/retry-failed",
			body:   nil,
		},
		{
			name:   "POST Retry Item",
			method: http.MethodPost,
			path:   "/api/v1/jobs/" + env.jobID2 + "/items/" + env.itemID + "/retry",
			body:   nil,
		},

		{
			name:   "DELETE Cancel",
			method: http.MethodDelete,
			path:   "/api/v1/jobs/" + env.jobID,
			body:   nil,
		},
		{
			name:      "DELETE History (Admin only)",
			method:    http.MethodDelete,
			path:      "/api/v1/jobs/history?older_than_days=30",
			body:      nil,
			adminOnly: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 1. Unauthenticated -> 401
			reqUnauth := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(tc.body))
			if len(tc.body) > 0 {
				reqUnauth.Header.Set("Content-Type", "application/json")
			}
			recUnauth := httptest.NewRecorder()
			env.router.ServeHTTP(recUnauth, reqUnauth)
			if recUnauth.Code != http.StatusUnauthorized {
				t.Errorf("[%s] unauthenticated: got %d, want 401", tc.name, recUnauth.Code)
			}

			// 2. Authenticated with User session but NO CSRF token -> 403
			reqNoCSRF := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(tc.body))
			reqNoCSRF.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: env.userToken})
			if len(tc.body) > 0 {
				reqNoCSRF.Header.Set("Content-Type", "application/json")
			}
			recNoCSRF := httptest.NewRecorder()
			env.router.ServeHTTP(recNoCSRF, reqNoCSRF)
			if recNoCSRF.Code != http.StatusForbidden {
				t.Errorf("[%s] authenticated without CSRF: got %d, want 403", tc.name, recNoCSRF.Code)
			}

			// 3. Authenticated User with valid CSRF cookie and header
			reqUserValid := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(tc.body))
			reqUserValid.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: env.userToken})
			reqUserValid.AddCookie(&http.Cookie{Name: middleware.CSRFCookieName, Value: env.csrfToken})
			reqUserValid.Header.Set(middleware.CSRFHeaderName, env.csrfToken)
			if len(tc.body) > 0 {
				reqUserValid.Header.Set("Content-Type", "application/json")
			}
			recUserValid := httptest.NewRecorder()
			env.router.ServeHTTP(recUserValid, reqUserValid)

			if tc.adminOnly {
				// Standard user must be forbidden (403) for admin-only routes
				if recUserValid.Code != http.StatusForbidden {
					t.Errorf("[%s] standard user on admin route: got %d, want 403", tc.name, recUserValid.Code)
				}

				// Admin with valid CSRF must succeed (200)
				reqAdminValid := httptest.NewRequest(tc.method, tc.path, bytes.NewReader(tc.body))
				reqAdminValid.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: env.adminToken})
				reqAdminValid.AddCookie(&http.Cookie{Name: middleware.CSRFCookieName, Value: env.csrfToken})
				reqAdminValid.Header.Set(middleware.CSRFHeaderName, env.csrfToken)
				if len(tc.body) > 0 {
					reqAdminValid.Header.Set("Content-Type", "application/json")
				}
				recAdminValid := httptest.NewRecorder()
				env.router.ServeHTTP(recAdminValid, reqAdminValid)
				if recAdminValid.Code != http.StatusOK {
					t.Errorf("[%s] admin with CSRF: got %d, want 200 (%s)", tc.name, recAdminValid.Code, recAdminValid.Body.String())
				}
			} else {
				// Standard user must succeed (200)
				if recUserValid.Code != http.StatusOK {
					t.Errorf("[%s] standard user with CSRF: got %d, want 200 (%s)", tc.name, recUserValid.Code, recUserValid.Body.String())
				}
			}
		})
	}
}

func TestJobsProgressAPI(t *testing.T) {
	env := setupJobAuthTestEnv(t)
	ctx := context.Background()

	// Create an artist batch job where parent row in DB has stale completed = 0
	parentJob := &jobs.Job{
		Type:             jobs.TypeArtist,
		Status:           jobs.StatusDownloading,
		Priority:         jobs.PriorityNormal,
		Label:            "Green Day Live Test",
		MetadataProvider: "ytmusic",
		MediaProvider:    "ytmusic",
		TargetID:         "target_greenday",
		Options:          jobs.DefaultOptions(),
		Total:            514,
		Completed:        0,
		Failed:           0,
		Skipped:          0,
	}
	if err := env.jobsRepo.Create(ctx, parentJob); err != nil {
		t.Fatalf("create parentJob: %v", err)
	}

	// Add 514 items: 166 completed, 4 matching, 344 pending
	items := make([]jobs.Item, 0, 514)
	for i := 0; i < 514; i++ {
		status := jobs.ItemPending
		if i < 166 {
			status = jobs.ItemCompleted
		} else if i < 170 {
			status = jobs.ItemMatching
		}
		items = append(items, jobs.Item{
			Position: i,
			Status:   status,
			Track:    music.Track{Title: "Song " + music.NewID()},
			Label:    "Song " + music.NewID(),
		})
	}
	if err := env.jobsRepo.AddItems(ctx, parentJob.ID, items); err != nil {
		t.Fatalf("add items: %v", err)
	}

	// 1. Test GET /api/v1/jobs (List Endpoint)
	reqList := httptest.NewRequest(http.MethodGet, "/api/v1/jobs?type=artist", nil)
	reqList.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: env.userToken})
	recList := httptest.NewRecorder()
	env.router.ServeHTTP(recList, reqList)

	if recList.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/jobs: got %d, want 200 (%s)", recList.Code, recList.Body.String())
	}

	var listResp struct {
		Data []struct {
			ID        string `json:"id"`
			Total     int    `json:"total"`
			Completed int    `json:"completed"`
			Failed    int    `json:"failed"`
			Skipped   int    `json:"skipped"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recList.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listResp.Data) != 1 {
		t.Fatalf("list returned %d items, want 1", len(listResp.Data))
	}
	jobInList := listResp.Data[0]
	if jobInList.ID != parentJob.ID {
		t.Fatalf("job ID = %s, want %s", jobInList.ID, parentJob.ID)
	}
	if jobInList.Total != 514 || jobInList.Completed != 166 {
		t.Errorf("List endpoint returned %d/%d, want 166/514", jobInList.Completed, jobInList.Total)
	}

	// 2. Test GET /api/v1/jobs/{id} (Detail Endpoint)
	reqDetail := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+parentJob.ID, nil)
	reqDetail.AddCookie(&http.Cookie{Name: middleware.SessionCookieName, Value: env.userToken})
	recDetail := httptest.NewRecorder()
	env.router.ServeHTTP(recDetail, reqDetail)

	if recDetail.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/jobs/{id}: got %d, want 200 (%s)", recDetail.Code, recDetail.Body.String())
	}

	var detailResp struct {
		Data struct {
			Job struct {
				ID        string `json:"id"`
				Total     int    `json:"total"`
				Completed int    `json:"completed"`
				Failed    int    `json:"failed"`
				Skipped   int    `json:"skipped"`
			} `json:"job"`
			Summary struct {
				Total     int `json:"total"`
				Completed int `json:"completed"`
				Failed    int `json:"failed"`
				Skipped   int `json:"skipped"`
			} `json:"summary"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recDetail.Body.Bytes(), &detailResp); err != nil {
		t.Fatalf("decode detail response: %v", err)
	}
	if detailResp.Data.Job.Total != 514 || detailResp.Data.Job.Completed != 166 {
		t.Errorf("Detail job returned %d/%d, want 166/514",
			detailResp.Data.Job.Completed, detailResp.Data.Job.Total)
	}
	if detailResp.Data.Summary.Total != 514 || detailResp.Data.Summary.Completed != 166 {
		t.Errorf("Detail summary returned %d/%d, want 166/514",
			detailResp.Data.Summary.Completed, detailResp.Data.Summary.Total)
	}

	// 3. Consistency check between list and detail
	if jobInList.Completed != detailResp.Data.Job.Completed || jobInList.Total != detailResp.Data.Job.Total {
		t.Errorf("List (%d/%d) and Detail (%d/%d) inconsistent",
			jobInList.Completed, jobInList.Total,
			detailResp.Data.Job.Completed, detailResp.Data.Job.Total)
	}
}
