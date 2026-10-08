package auth

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"strings"
	"sync"
	"time"

	"ytdm/backend/internal/apperr"
)

const deviceTTL = 5 * time.Minute
const deviceInterval = 5 * time.Second
const maxDeviceGrants = 128

// Pending device grants deliberately live only in this process. Restarting the
// server invalidates pending codes; established sessions use normal persistence.
// Neither raw device secrets nor short codes are retained or logged.
type deviceGrants struct {
	mu            sync.Mutex
	grants        map[string]*deviceGrant
	starts        map[string][]time.Time
	confirmations map[string][]time.Time
}

type deviceGrant struct {
	codeHash, name, userID, sessionID string
	expires, nextPoll                 time.Time
}

type DeviceStart struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	ExpiresIn  int    `json:"expires_in"`
	Interval   int    `json:"interval"`
}

type DevicePreview struct {
	DeviceName string    `json:"device_name"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func normalizeDeviceCode(code string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
}

func (d *deviceGrants) prune(now time.Time) {
	if d.grants == nil {
		d.grants = make(map[string]*deviceGrant)
	}
	if d.starts == nil {
		d.starts = make(map[string][]time.Time)
	}
	if d.confirmations == nil {
		d.confirmations = make(map[string][]time.Time)
	}
	for key, grant := range d.grants {
		if !now.Before(grant.expires) {
			delete(d.grants, key)
		}
	}
	for _, records := range []map[string][]time.Time{d.starts, d.confirmations} {
		for key, attempts := range records {
			valid := attempts[:0]
			for _, attempt := range attempts {
				if now.Sub(attempt) < deviceTTL {
					valid = append(valid, attempt)
				}
			}
			if len(valid) == 0 {
				delete(records, key)
			} else {
				records[key] = valid
			}
		}
	}
}

// start/confirmation admission is atomic and bounded, including distinct-IP
// floods. Unknown codes get the same answer as expired or already-used codes.
// The key set is bounded by dropping one record, not by refusing new keys: keys
// derive from a forwarded client address, so refusing them would let a flood of
// forged addresses lock every genuine client out for the whole expiry window.
func deviceAdmit(records map[string][]time.Time, key string, now time.Time, maximum int) bool {
	if len(records[key]) >= maximum {
		return false
	}
	if records[key] == nil && len(records) >= 1024 {
		for evicted := range records {
			delete(records, evicted)
			break
		}
	}
	records[key] = append(records[key], now)
	return true
}

func deviceInvalid() error {
	return apperr.New(apperr.CodeInvalidRequest, "Der Gerätecode ist ungültig, abgelaufen oder bereits verwendet. Bitte einen neuen Code anfordern.")
}
func deviceLimited() error {
	return apperr.New(apperr.CodeRateLimited, "Bitte kurz warten und erneut versuchen.")
}

func (s *Service) StartDevice(name, ip string) (*DeviceStart, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 || strings.ContainsAny(name, "\r\n\t") {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Bitte einen Gerätenamen mit höchstens 64 Zeichen angeben.")
	}
	d := &s.devices
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now().UTC()
	d.prune(now)
	// Check capacity first so a refused start does not use up the caller's quota.
	if len(d.grants) >= maxDeviceGrants || !deviceAdmit(d.starts, ip, now, 10) {
		return nil, deviceLimited()
	}
	raw, hash, err := GenerateSessionToken()
	if err != nil {
		return nil, err
	}
	for attempts := 0; attempts < 8; attempts++ {
		var bytes [5]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return nil, apperr.Wrap(apperr.CodeInternal, "Gerätecode konnte nicht erstellt werden.", err)
		}
		code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(bytes[:])
		codeHash := HashToken(code)
		collision := false
		for _, grant := range d.grants {
			if grant.codeHash == codeHash {
				collision = true
			}
		}
		if collision {
			continue
		}
		d.grants[hash] = &deviceGrant{codeHash: codeHash, name: name, expires: now.Add(deviceTTL)}
		return &DeviceStart{DeviceCode: raw, UserCode: code[:4] + "-" + code[4:], ExpiresIn: int(deviceTTL.Seconds()), Interval: int(deviceInterval.Seconds())}, nil
	}
	return nil, apperr.New(apperr.CodeInternal, "Gerätecode konnte nicht erstellt werden.")
}

// PreviewDevice doesn't approve anything. The user sees the requesting device
// before explicitly granting it a normal account session.
func (s *Service) PreviewDevice(code, userID string) (*DevicePreview, error) {
	d := &s.devices
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now().UTC()
	d.prune(now)
	if !deviceAdmit(d.confirmations, userID, now, 10) {
		return nil, deviceLimited()
	}
	hash := HashToken(normalizeDeviceCode(code))
	for _, g := range d.grants {
		if g.codeHash == hash && g.userID == "" {
			return &DevicePreview{DeviceName: g.name, ExpiresAt: g.expires}, nil
		}
	}
	return nil, deviceInvalid()
}

func (s *Service) ConfirmDevice(code string, user *User, session *Session) error {
	if user == nil || session == nil || !user.Enabled || session.UserID != user.ID {
		return apperr.New(apperr.CodeUnauthenticated, "Bitte zuerst anmelden.")
	}
	d := &s.devices
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now().UTC()
	d.prune(now)
	if !deviceAdmit(d.confirmations, user.ID, now, 10) {
		return deviceLimited()
	}
	hash := HashToken(normalizeDeviceCode(code))
	for _, g := range d.grants {
		if g.codeHash == hash && g.userID == "" {
			g.userID = user.ID
			g.sessionID = session.ID
			return nil
		}
	}
	return deviceInvalid()
}

// PollDevice consumes a grant exactly once: an approved grant is removed under
// the lock before any database work, so concurrent polls cannot both exchange it
// and the lock is not held across database round trips. The granting session must
// still exist and be valid at exchange.
func (s *Service) PollDevice(ctx context.Context, raw, ip string) (*AuthResult, string, error) {
	if len(raw) != 64 {
		return nil, "", deviceInvalid()
	}
	d := &s.devices
	d.mu.Lock()
	now := time.Now().UTC()
	d.prune(now)
	key := HashToken(raw)
	g := d.grants[key]
	if g == nil {
		d.mu.Unlock()
		return nil, "", deviceInvalid()
	}
	if now.Before(g.nextPoll) {
		g.nextPoll = now.Add(2 * deviceInterval)
		d.mu.Unlock()
		return nil, "slow_down", nil
	}
	g.nextPoll = now.Add(deviceInterval)
	if g.userID == "" {
		d.mu.Unlock()
		return nil, "authorization_pending", nil
	}
	delete(d.grants, key)
	userID, sessionID, name := g.userID, g.sessionID, g.name
	d.mu.Unlock()

	session, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil || session == nil || session.UserID != userID || !now.Before(session.ExpiresAt) || now.Sub(session.LastSeenAt) > s.inactivityPeriod {
		return nil, "", deviceInvalid()
	}
	user, err := s.users.GetByID(ctx, userID)
	if err != nil || user == nil || !user.Enabled {
		return nil, "", deviceInvalid()
	}
	result, err := s.createSession(ctx, *user, ip, "YTMDL Apple · "+name)
	if err != nil {
		// Nothing was issued: put the grant back so the device can retry.
		d.mu.Lock()
		if time.Now().Before(g.expires) {
			d.grants[key] = g
		}
		d.mu.Unlock()
		return nil, "", err
	}
	return result, "authorized", nil
}
