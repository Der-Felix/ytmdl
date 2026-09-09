package jobs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEventJobStatus_ErrorSerializationAndClearing(t *testing.T) {
	// 1. EventJobStatus can serialize SESSION_UNAVAILABLE
	errCode := "SESSION_UNAVAILABLE"
	errMsg := "Provider vorübergehend nicht verfügbar"
	evtSet := Event{
		Type:         EventJobStatus,
		JobID:        "job-1",
		Status:       StatusRetryWait,
		ErrorCode:    Ptr(errCode),
		ErrorMessage: Ptr(errMsg),
	}
	data, err := json.Marshal(evtSet)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	raw := string(data)
	if !strings.Contains(raw, `"error_code":"SESSION_UNAVAILABLE"`) {
		t.Errorf("expected JSON to contain error_code SESSION_UNAVAILABLE, got %s", raw)
	}
	if !strings.Contains(raw, `"error_message":"Provider vorübergehend nicht verfügbar"`) {
		t.Errorf("expected JSON to contain error_message, got %s", raw)
	}

	// 2 & 3. Later EventJobStatus explicitly clears error_code and error_message
	empty := ""
	evtClear := Event{
		Type:         EventJobStatus,
		JobID:        "job-1",
		Status:       StatusMatching,
		ErrorCode:    Ptr(empty),
		ErrorMessage: Ptr(empty),
	}
	dataClear, err := json.Marshal(evtClear)
	if err != nil {
		t.Fatalf("marshal clear failed: %v", err)
	}
	rawClear := string(dataClear)
	if !strings.Contains(rawClear, `"error_code":""`) {
		t.Errorf("expected JSON to explicitly contain empty error_code, got %s", rawClear)
	}
	if !strings.Contains(rawClear, `"error_message":""`) {
		t.Errorf("expected JSON to explicitly contain empty error_message, got %s", rawClear)
	}

	// 4. New error replaces old error
	newErrCode := "RATE_LIMITED"
	newErrMsg := "Too many requests"
	evtReplace := Event{
		Type:         EventJobStatus,
		JobID:        "job-1",
		Status:       StatusRetryWait,
		ErrorCode:    Ptr(newErrCode),
		ErrorMessage: Ptr(newErrMsg),
	}
	dataReplace, err := json.Marshal(evtReplace)
	if err != nil {
		t.Fatalf("marshal replace failed: %v", err)
	}
	rawReplace := string(dataReplace)
	if !strings.Contains(rawReplace, `"error_code":"RATE_LIMITED"`) {
		t.Errorf("expected JSON to contain error_code RATE_LIMITED, got %s", rawReplace)
	}
	if !strings.Contains(rawReplace, `"error_message":"Too many requests"`) {
		t.Errorf("expected JSON to contain error_message, got %s", rawReplace)
	}

	// Unrelated events must omit error_code and error_message
	evtProgress := Event{
		Type:   EventJobProgress,
		JobID:  "job-1",
		Status: StatusDownloading,
	}
	dataProgress, err := json.Marshal(evtProgress)
	if err != nil {
		t.Fatalf("marshal progress failed: %v", err)
	}
	rawProgress := string(dataProgress)
	if strings.Contains(rawProgress, "error_code") {
		t.Errorf("expected progress event to omit error_code, got %s", rawProgress)
	}
	if strings.Contains(rawProgress, "error_message") {
		t.Errorf("expected progress event to omit error_message, got %s", rawProgress)
	}
}

// memoryStore is a mock Store for testing Manager status events without requiring a real database
type mockStatusStore struct {
	Store
	lastStatus  Status
	lastErrCode string
	lastErrMsg  string
}

func (m *mockStatusStore) SetStatus(ctx context.Context, id string, status Status, errorCode, errorMessage string) error {
	m.lastStatus = status
	m.lastErrCode = errorCode
	m.lastErrMsg = errorMessage
	return nil
}

func TestManager_StatusTransition_SSEErrorClearing(t *testing.T) {
	store := &mockStatusStore{}
	broker := NewBroker(nil)
	mgr := NewManagerForTest(store, broker)

	events, unsubscribe := broker.Subscribe()
	defer unsubscribe()

	ctx := context.Background()
	job := &Job{
		ID:     "job-123",
		Status: StatusQueued,
		Label:  "Test Album",
	}

	// Move to retry_wait with SESSION_UNAVAILABLE
	mgr.setStatusWithReason(ctx, job, StatusRetryWait, "SESSION_UNAVAILABLE", "Provider vorübergehend nicht verfügbar")

	var setEvt Event
	select {
	case setEvt = <-events:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for retry_wait event")
	}
	if setEvt.ErrorCode == nil || *setEvt.ErrorCode != "SESSION_UNAVAILABLE" {
		t.Fatalf("expected error_code SESSION_UNAVAILABLE, got %v", setEvt.ErrorCode)
	}
	if setEvt.ErrorMessage == nil || *setEvt.ErrorMessage != "Provider vorübergehend nicht verfügbar" {
		t.Fatalf("expected error_message, got %v", setEvt.ErrorMessage)
	}

	// Now recover to matching - should clear error
	mgr.setStatus(ctx, job, StatusMatching)

	var clearEvt Event
	select {
	case clearEvt = <-events:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for matching recovery event")
	}
	if clearEvt.ErrorCode == nil || *clearEvt.ErrorCode != "" {
		t.Fatalf("expected error_code explicitly cleared to empty string, got %v", clearEvt.ErrorCode)
	}
	if clearEvt.ErrorMessage == nil || *clearEvt.ErrorMessage != "" {
		t.Fatalf("expected error_message explicitly cleared to empty string, got %v", clearEvt.ErrorMessage)
	}

	data, err := json.Marshal(clearEvt)
	if err != nil {
		t.Fatalf("marshal clear event failed: %v", err)
	}
	if !strings.Contains(string(data), `"error_code":""`) {
		t.Fatalf("expected serialized clear event to contain error_code:\"\", got %s", string(data))
	}
	if !strings.Contains(string(data), `"error_message":""`) {
		t.Fatalf("expected serialized clear event to contain error_message:\"\", got %s", string(data))
	}
}
