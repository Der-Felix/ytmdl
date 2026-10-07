package auth

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"ytdm/backend/internal/apperr"
)

func TestDeviceGrantExpiryAndAdmission(t *testing.T) {
	svc := &Service{}
	first, err := svc.StartDevice("Apple TV", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.DeviceCode) != 64 || len(first.UserCode) != 9 || first.Interval != 5 || first.ExpiresIn != 300 {
		t.Fatal("invalid code contract")
	}
	preview, err := svc.PreviewDevice(strings.ToLower(first.UserCode), "u")
	if err != nil || preview.DeviceName != "Apple TV" {
		t.Fatal("preview missing")
	}
	svc.devices.grants[HashToken(first.DeviceCode)].expires = time.Now().Add(-time.Second)
	if _, err := svc.PreviewDevice(first.UserCode, "u"); err == nil {
		t.Fatal("expired code accepted")
	}
	for i := 1; i < 10; i++ {
		if _, err := svc.StartDevice("Apple TV", "fixture"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.StartDevice("Apple TV", "fixture"); apperr.CodeOf(err) != apperr.CodeRateLimited {
		t.Fatal("start rate limit missing")
	}
}

func TestDeviceConfirmationCannotBeReassigned(t *testing.T) {
	svc := &Service{}
	start, err := svc.StartDevice("Apple TV", "ip")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmDevice(start.UserCode, nil, nil); apperr.CodeOf(err) != apperr.CodeUnauthenticated {
		t.Fatal("anonymous confirmation accepted")
	}
	user := &User{ID: "u", Enabled: true}
	session := &Session{ID: "s", UserID: "u"}
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if svc.ConfirmDevice(start.UserCode, user, session) == nil {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("confirmed %d times", winners)
	}
	if _, err := svc.PreviewDevice(start.UserCode, "other"); err == nil {
		t.Fatal("already-approved code disclosed")
	}
}

func TestDeviceSecretSeparatedAndPollingBackoff(t *testing.T) {
	svc := &Service{}
	start, err := svc.StartDevice("Apple TV", "ip")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.PollDevice(t.Context(), start.UserCode, "ip"); err == nil {
		t.Fatal("short code can exchange a session")
	}
	_, state, err := svc.PollDevice(t.Context(), start.DeviceCode, "ip")
	if err != nil || state != "authorization_pending" {
		t.Fatal("pending state missing")
	}
	_, state, err = svc.PollDevice(t.Context(), start.DeviceCode, "ip")
	if err != nil || state != "slow_down" {
		t.Fatal("poll backoff missing")
	}
	for i := 0; i < 10; i++ {
		_, _ = svc.PreviewDevice("WRONG-CODE", "user")
	}
	if _, err := svc.PreviewDevice(start.UserCode, "user"); apperr.CodeOf(err) != apperr.CodeRateLimited {
		t.Fatal("guess limit missing")
	}
}

func TestDeviceStartRefusedAtCapacityKeepsCallerQuota(t *testing.T) {
	svc := &Service{}
	svc.devices.grants = make(map[string]*deviceGrant)
	for i := 0; i < maxDeviceGrants; i++ {
		svc.devices.grants[fmt.Sprint(i)] = &deviceGrant{expires: time.Now().Add(time.Minute)}
	}
	for i := 0; i < 15; i++ {
		if _, err := svc.StartDevice("Apple TV", "ip"); apperr.CodeOf(err) != apperr.CodeRateLimited {
			t.Fatal("start accepted while the grant table is full")
		}
	}
	svc.devices.grants = make(map[string]*deviceGrant)
	for i := 0; i < 10; i++ {
		if _, err := svc.StartDevice("Apple TV", "ip"); err != nil {
			t.Fatalf("refused starts consumed the caller's quota: %v", err)
		}
	}
}

func TestDeviceAdmissionKeyCapEvictsInsteadOfRefusingNewKeys(t *testing.T) {
	records := make(map[string][]time.Time)
	now := time.Now()
	for i := 0; i < 1024; i++ {
		records[fmt.Sprint(i)] = []time.Time{now}
	}
	if !deviceAdmit(records, "new-client", now, 10) {
		t.Fatal("a full key table locked out a new client")
	}
	if len(records) != 1024 || len(records["new-client"]) != 1 {
		t.Fatalf("key table not bounded: %d keys", len(records))
	}
}
