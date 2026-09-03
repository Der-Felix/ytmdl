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
			got := jobs.DeriveParentStatus(tc.items)
			if got != tc.want {
				t.Errorf("DeriveParentStatus() = %v, want %v", got, tc.want)
			}
		})
	}
}
