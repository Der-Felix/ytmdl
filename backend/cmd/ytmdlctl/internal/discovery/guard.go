package discovery

import (
	"context"
	"errors"
	"strings"

	"ytdm/backend/cmd/ytmdlctl/internal/engine"
)

// GuardStatus describes the verification outcome.
type GuardStatus string

// ResolveStorageGuardID keeps status, dry runs and real updates consistent with
// the backend environment aliases. Conflicting identities fail closed.
func ResolveStorageGuardID(env map[string]string) (string, error) {
	identity := ""
	for _, key := range []string{"YTMDL_STORAGE_GUARD_ID", "MUSICDL_STORAGE_GUARD_ID", "YTDM_STORAGE_GUARD_ID"} {
		value := strings.TrimPrefix(strings.TrimSpace(env[key]), "ytmdl-storage:")
		if value == "" {
			continue
		}
		if identity != "" && identity != value {
			return "", errors.New("conflicting configured storage guard identities")
		}
		identity = value
	}
	return identity, nil
}

const (
	GuardStatusVerified    GuardStatus = "verified"
	GuardStatusMissing     GuardStatus = "missing"
	GuardStatusMismatch    GuardStatus = "mismatch"
	GuardStatusDisabled    GuardStatus = "disabled"
	GuardStatusUnavailable GuardStatus = "unavailable"
)

// AllowsUpdate reports whether a storage guard verification outcome permits proceeding with an update.
// Updates are permitted when the guard is either intentionally disabled or successfully verified.
// All configured but unverified, missing, mismatched, unavailable, or unknown states are blocked.
func (s GuardStatus) AllowsUpdate() bool {
	switch s {
	case GuardStatusVerified, GuardStatusDisabled:
		return true
	default:
		return false
	}
}

// StaticStorageGuardScript is the FIXED, constant script executed inside backend container.
// It never interpolates user variables. Expected guard ID is passed strictly via stdin.
const StaticStorageGuardScript = `read -r EXPECTED
FILE="/music/.ytmdl-storage-id"
if [ ! -f "$FILE" ]; then exit 2; fi
ACTUAL=$(cat "$FILE" | tr -d '\r\n')
if [ "$ACTUAL" = "$EXPECTED" ] || [ "$ACTUAL" = "ytmdl-storage:$EXPECTED" ] || [ "ytmdl-storage:$ACTUAL" = "$EXPECTED" ]; then
    exit 0
else
    exit 3
fi`

// VerifyStorageGuard checks storage guard identity.
// Container mount namespace probe is the ONLY authoritative source of truth.
// A host filesystem path is NEVER authoritative and cannot produce GuardStatusVerified.
func VerifyStorageGuard(ctx context.Context, eng engine.Engine, projectDir, composeFile, localMusicPath, expectedGuardID string) (GuardStatus, error) {
	expectedGuardID = strings.TrimSpace(expectedGuardID)
	if expectedGuardID == "" {
		return GuardStatusDisabled, nil
	}

	// Authoritative verification: container mount namespace probe
	if eng != nil && composeFile != "" {
		return VerifyStorageGuardViaContainer(ctx, eng, projectDir, composeFile, expectedGuardID)
	}

	// Without container engine / mount namespace verification, Guard cannot be verified.
	return GuardStatusUnavailable, errors.New("storage guard verification unavailable without container mount namespace probe")
}

// VerifyStorageGuardViaContainer executes the fixed static probe in the backend container.
func VerifyStorageGuardViaContainer(ctx context.Context, eng engine.Engine, projectDir, composeFile, expectedGuardID string) (GuardStatus, error) {
	if expectedGuardID == "" {
		return GuardStatusDisabled, nil
	}

	stdin := strings.NewReader(expectedGuardID + "\n")
	res, err := eng.Exec(ctx, projectDir, composeFile, "backend", stdin, "sh", "-c", StaticStorageGuardScript)
	if err != nil || (res != nil && res.ExitCode != 0) {
		if res != nil {
			switch res.ExitCode {
			case 2:
				return GuardStatusMissing, errors.New("storage guard marker missing inside container")
			case 3:
				return GuardStatusMismatch, errors.New("storage guard marker mismatch inside container")
			}
		}
		return GuardStatusUnavailable, errors.New("failed executing container storage guard probe")
	}

	return GuardStatusVerified, nil
}
