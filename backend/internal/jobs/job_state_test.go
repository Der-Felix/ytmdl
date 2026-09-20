package jobs_test

import (
	"testing"

	"ytdm/backend/internal/jobs"
)

func TestJobItemStatusTransitions(t *testing.T) {
	// Active state transitions
	validTransitions := []struct {
		from jobs.ItemStatus
		to   jobs.ItemStatus
	}{
		{jobs.ItemPending, jobs.ItemMatching},
		{jobs.ItemMatching, jobs.ItemDownloading},
		{jobs.ItemDownloading, jobs.ItemTagging},
		{jobs.ItemTagging, jobs.ItemFinalizing},
		{jobs.ItemFinalizing, jobs.ItemCompleted},
		{jobs.ItemDownloading, jobs.ItemRetryWait},
		{jobs.ItemRetryWait, jobs.ItemMatching},
		{jobs.ItemFinalizing, jobs.ItemWaitingStorage},
		{jobs.ItemWaitingStorage, jobs.ItemFinalizing},
		{jobs.ItemDownloading, jobs.ItemWaitingSpace},
		{jobs.ItemWaitingSpace, jobs.ItemDownloading},
		{jobs.ItemMatching, jobs.ItemFailed},
		{jobs.ItemDownloading, jobs.ItemCancelled},
	}

	for _, tt := range validTransitions {
		if !tt.from.CanTransitionTo(tt.to) {
			t.Errorf("expected transition %s -> %s to be valid", tt.from, tt.to)
		}
	}

	// Terminal states cannot transition
	terminal := []jobs.ItemStatus{jobs.ItemCompleted, jobs.ItemFailed, jobs.ItemSkipped, jobs.ItemCancelled}
	for _, term := range terminal {
		if term.CanTransitionTo(jobs.ItemPending) {
			t.Errorf("expected terminal status %s CANNOT transition to pending", term)
		}
		if term.CanTransitionTo(jobs.ItemMatching) {
			t.Errorf("expected terminal status %s CANNOT transition to matching", term)
		}
	}
}

func TestDeriveParentStatus(t *testing.T) {
	cases := []struct {
		name  string
		items []jobs.Item
		want  jobs.Status
	}{
		{
			name:  "empty items",
			items: []jobs.Item{},
			want:  jobs.StatusQueued,
		},
		{
			name: "one downloading one retry_wait",
			items: []jobs.Item{
				{Status: jobs.ItemDownloading},
				{Status: jobs.ItemRetryWait},
			},
			want: jobs.StatusDownloading,
		},
		{
			name: "all items waiting for storage",
			items: []jobs.Item{
				{Status: jobs.ItemWaitingStorage},
				{Status: jobs.ItemWaitingStorage},
			},
			want: jobs.StatusWaitingStorage,
		},
		{
			name: "all items waiting for space",
			items: []jobs.Item{
				{Status: jobs.ItemWaitingSpace},
				{Status: jobs.ItemWaitingSpace},
			},
			want: jobs.StatusWaitingSpace,
		},
		{
			name: "mixed completed and waiting for storage",
			items: []jobs.Item{
				{Status: jobs.ItemCompleted},
				{Status: jobs.ItemWaitingStorage},
			},
			want: jobs.StatusWaitingStorage,
		},
		{
			name: "mixed completed and failed",
			items: []jobs.Item{
				{Status: jobs.ItemCompleted},
				{Status: jobs.ItemFailed},
			},
			want: jobs.StatusCompleted,
		},
		{
			name: "all failed",
			items: []jobs.Item{
				{Status: jobs.ItemFailed},
				{Status: jobs.ItemFailed},
			},
			want: jobs.StatusFailed,
		},
		{
			name: "all completed",
			items: []jobs.Item{
				{Status: jobs.ItemCompleted},
				{Status: jobs.ItemCompleted},
			},
			want: jobs.StatusCompleted,
		},
		{
			name: "all cancelled",
			items: []jobs.Item{
				{Status: jobs.ItemCancelled},
				{Status: jobs.ItemCancelled},
			},
			want: jobs.StatusCancelled,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, _ := jobs.DeriveParentStatusDetails(tc.items)
			if got != tc.want {
				t.Errorf("DeriveParentStatusDetails() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDeriveParentStatusDetails(t *testing.T) {
	cases := []struct {
		name         string
		items        []jobs.Item
		wantStatus   jobs.Status
		wantCode     string
		wantMsg      string
		wantMsgEmpty bool
	}{
		{
			name: "all retry_wait session_unavailable",
			items: []jobs.Item{
				{Status: jobs.ItemRetryWait, ErrorCode: "SESSION_UNAVAILABLE", ErrorMessage: "wait for session"},
				{Status: jobs.ItemRetryWait, ErrorCode: "SESSION_UNAVAILABLE", ErrorMessage: "wait for session"},
			},
			wantStatus:   jobs.StatusRetryWait,
			wantCode:     "SESSION_UNAVAILABLE",
			wantMsgEmpty: false,
		},
		{
			name: "retry_wait generic rate_limited",
			items: []jobs.Item{
				{Status: jobs.ItemRetryWait, ErrorCode: "RATE_LIMITED", ErrorMessage: "too many requests"},
			},
			wantStatus:   jobs.StatusRetryWait,
			wantCode:     "",
			wantMsgEmpty: true,
		},
		{
			name: "mixed completed and session_unavailable",
			items: []jobs.Item{
				{Status: jobs.ItemCompleted},
				{Status: jobs.ItemRetryWait, ErrorCode: "SESSION_UNAVAILABLE", ErrorMessage: "wait for session"},
			},
			wantStatus:   jobs.StatusRetryWait,
			wantCode:     "SESSION_UNAVAILABLE",
			wantMsgEmpty: false,
		},
		{
			name: "active downloading takes precedence over retry_wait",
			items: []jobs.Item{
				{Status: jobs.ItemDownloading},
				{Status: jobs.ItemRetryWait, ErrorCode: "SESSION_UNAVAILABLE", ErrorMessage: "wait for session"},
			},
			wantStatus:   jobs.StatusDownloading,
			wantCode:     "",
			wantMsgEmpty: true,
		},
		{
			name:         "empty items",
			items:        []jobs.Item{},
			wantStatus:   jobs.StatusQueued,
			wantCode:     "",
			wantMsgEmpty: true,
		},
		// TEST A: single failed item
		{
			name: "single failed item propagates error",
			items: []jobs.Item{
				{Status: jobs.ItemFailed, ErrorCode: "TRACK_NOT_FOUND", ErrorMessage: "Video unavailable"},
			},
			wantStatus:   jobs.StatusFailed,
			wantCode:     "TRACK_NOT_FOUND",
			wantMsg:      "Video unavailable",
			wantMsgEmpty: false,
		},
		// TEST B: mixed multi-track job (skipped + failed)
		{
			name: "mixed skipped and failed propagates first failed error",
			items: []jobs.Item{
				{Status: jobs.ItemSkipped},
				{Status: jobs.ItemFailed, ErrorCode: "PATH_CONFLICT", ErrorMessage: "File already exists"},
			},
			wantStatus:   jobs.StatusFailed,
			wantCode:     "PATH_CONFLICT",
			wantMsg:      "File already exists",
			wantMsgEmpty: false,
		},
		// TEST C: multiple failed items (deterministic selection of first failed item)
		{
			name: "multiple failed items selects first failed item deterministically",
			items: []jobs.Item{
				{Status: jobs.ItemFailed, ErrorCode: "FIRST_ERROR", ErrorMessage: "First error message"},
				{Status: jobs.ItemFailed, ErrorCode: "SECOND_ERROR", ErrorMessage: "Second error message"},
			},
			wantStatus:   jobs.StatusFailed,
			wantCode:     "FIRST_ERROR",
			wantMsg:      "First error message",
			wantMsgEmpty: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStatus, gotCode, gotMsg := jobs.DeriveParentStatusDetails(tc.items)
			if gotStatus != tc.wantStatus {
				t.Errorf("status = %v, want %v", gotStatus, tc.wantStatus)
			}
			if gotCode != tc.wantCode {
				t.Errorf("errorCode = %v, want %v", gotCode, tc.wantCode)
			}
			if tc.wantMsg != "" && gotMsg != tc.wantMsg {
				t.Errorf("errorMessage = %q, want %q", gotMsg, tc.wantMsg)
			}
			if tc.wantMsgEmpty && gotMsg != "" {
				t.Errorf("expected empty error message, got %q", gotMsg)
			}
			if !tc.wantMsgEmpty && tc.wantMsg == "" && gotMsg == "" {
				t.Errorf("expected non-empty error message")
			}
		})
	}
}

func TestDeriveParentStatusDetails_RetryClearing(t *testing.T) {
	// Start with a failed parent carrying an error
	items := []jobs.Item{
		{Status: jobs.ItemFailed, ErrorCode: "TRACK_NOT_FOUND", ErrorMessage: "Video unavailable"},
	}
	status, code, msg := jobs.DeriveParentStatusDetails(items)
	if status != jobs.StatusFailed || code != "TRACK_NOT_FOUND" || msg != "Video unavailable" {
		t.Fatalf("expected StatusFailed with error details, got status=%v code=%v msg=%v", status, code, msg)
	}

	// Retry item: resets to ItemPending with cleared error
	items[0].Status = jobs.ItemPending
	items[0].ErrorCode = ""
	items[0].ErrorMessage = ""

	// Parent status must now be Queued and error details cleared
	status, code, msg = jobs.DeriveParentStatusDetails(items)
	if status != jobs.StatusQueued || code != "" || msg != "" {
		t.Fatalf("expected StatusQueued with empty error details, got status=%v code=%v msg=%v", status, code, msg)
	}

	// Processing begins: item becomes ItemDownloading
	items[0].Status = jobs.ItemDownloading
	status, code, msg = jobs.DeriveParentStatusDetails(items)
	if status != jobs.StatusDownloading || code != "" || msg != "" {
		t.Fatalf("expected StatusDownloading with empty error details, got status=%v code=%v msg=%v", status, code, msg)
	}

	// Later completes successfully: item becomes ItemCompleted
	items[0].Status = jobs.ItemCompleted
	status, code, msg = jobs.DeriveParentStatusDetails(items)
	if status != jobs.StatusCompleted || code != "" || msg != "" {
		t.Fatalf("expected StatusCompleted with empty error details, got status=%v code=%v msg=%v", status, code, msg)
	}
}
