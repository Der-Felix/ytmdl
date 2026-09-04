package manifest_test

import (
	"strings"
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/manifest"
)

const validManifestJSON = `{
  "manifest_version": 1,
  "release_version": "0.16.0",
  "release_tag": "v0.16.0",
  "target_schema": 8,
  "rollback_classification": "schema_neutral",
  "min_upgrade_from": "0.15.0",
  "images": {
    "backend": {
      "repository": "ghcr.io/der-felix/ytmdl-backend",
      "tag": "0.16.0",
      "digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111"
    },
    "frontend": {
      "repository": "ghcr.io/der-felix/ytmdl-frontend",
      "tag": "0.16.0",
      "digest": "sha256:2222222222222222222222222222222222222222222222222222222222222222"
    }
  },
  "required_env": ["POSTGRES_PASSWORD", "MUSICDL_DATABASE_URL"]
}`

func TestDecodeValidManifest(t *testing.T) {
	m, err := manifest.Decode([]byte(validManifestJSON))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if m.ManifestVersion != 1 {
		t.Errorf("ManifestVersion = %d, want 1", m.ManifestVersion)
	}
	if m.ReleaseVersion != "0.16.0" {
		t.Errorf("ReleaseVersion = %q, want 0.16.0", m.ReleaseVersion)
	}
	if m.ReleaseTag != "v0.16.0" {
		t.Errorf("ReleaseTag = %q, want v0.16.0", m.ReleaseTag)
	}
	if m.TargetSchema != 8 {
		t.Errorf("TargetSchema = %d, want 8", m.TargetSchema)
	}
	if m.RollbackClassification != manifest.RollbackSchemaNeutral {
		t.Errorf("RollbackClassification = %q, want schema_neutral", m.RollbackClassification)
	}
	if m.MinUpgradeFrom != "0.15.0" {
		t.Errorf("MinUpgradeFrom = %q, want 0.15.0", m.MinUpgradeFrom)
	}

	// Validate against GitHub release tag "v0.16.0"
	if err := m.Validate("v0.16.0"); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Schema compatibility
	if err := m.ValidateSchemaCompatibility(8); err != nil {
		t.Errorf("schema 8 compatibility failed: %v", err)
	}
}

func TestDecodeInvalidManifests(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		expectedErr string
	}{
		{
			name:        "empty document",
			json:        "",
			expectedErr: "empty manifest",
		},
		{
			name:        "trailing garbage",
			json:        validManifestJSON + "\n trailing garbage",
			expectedErr: "trailing garbage",
		},
		{
			name:        "unknown field",
			json:        `{"manifest_version": 1, "release_version": "0.16.0", "unknown_field": "bad"}`,
			expectedErr: "unknown field",
		},
		{
			name:        "wrong manifest_version",
			json:        strings.Replace(validManifestJSON, `"manifest_version": 1`, `"manifest_version": 2`, 1),
			expectedErr: "unsupported manifest version",
		},
		{
			name:        "version tag mismatch",
			json:        strings.Replace(validManifestJSON, `"release_tag": "v0.16.0"`, `"release_tag": "v0.17.0"`, 1),
			expectedErr: "release_version and release_tag mismatch",
		},
		{
			name:        "missing target_schema",
			json:        strings.Replace(validManifestJSON, `"target_schema": 8,`, `"target_schema": 0,`, 1),
			expectedErr: "missing or invalid target_schema",
		},
		{
			name:        "wrong backend repository",
			json:        strings.Replace(validManifestJSON, `ghcr.io/der-felix/ytmdl-backend`, `docker.io/malicious/backend`, 1),
			expectedErr: "backend image repository not allowed",
		},
		{
			name:        "backend image tag mismatch",
			json:        strings.Replace(validManifestJSON, `"tag": "0.16.0"`, `"tag": "latest"`, 1),
			expectedErr: "does not match release_version",
		},
		{
			name:        "wrong frontend repository",
			json:        strings.Replace(validManifestJSON, `ghcr.io/der-felix/ytmdl-frontend`, `ghcr.io/other/frontend`, 1),
			expectedErr: "frontend image repository not allowed",
		},
		{
			name:        "uppercase digest hex",
			json:        strings.Replace(validManifestJSON, "1111111111111111111111111111111111111111111111111111111111111111", "111111111111111111111111111111111111111111111111111111111111111A", 1),
			expectedErr: "invalid digest syntax",
		},
		{
			name:        "short digest",
			json:        strings.Replace(validManifestJSON, "sha256:1111111111111111111111111111111111111111111111111111111111111111", "sha256:123", 1),
			expectedErr: "invalid digest syntax",
		},
		{
			name:        "unknown rollback classification",
			json:        strings.Replace(validManifestJSON, `"schema_neutral"`, `"unknown_rollback"`, 1),
			expectedErr: "unsupported rollback classification",
		},
		{
			name:        "invalid min_upgrade_from",
			json:        strings.Replace(validManifestJSON, `"0.15.0"`, `"not_a_version"`, 1),
			expectedErr: "invalid min_upgrade_from",
		},
		{
			name:        "min_upgrade_from greater than release_version",
			json:        strings.Replace(validManifestJSON, `"0.15.0"`, `"0.17.0"`, 1),
			expectedErr: "cannot be greater than release_version",
		},
		{
			name:        "duplicate required_env",
			json:        strings.Replace(validManifestJSON, `"POSTGRES_PASSWORD", "MUSICDL_DATABASE_URL"`, `"FOO", "FOO"`, 1),
			expectedErr: "duplicate required_env",
		},
		{
			name:        "invalid required_env format",
			json:        strings.Replace(validManifestJSON, `"POSTGRES_PASSWORD"`, `"FOO=bar"`, 1),
			expectedErr: "invalid required_env variable name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, err := manifest.Decode([]byte(tc.json))
			if err == nil {
				err = m.Validate("v0.16.0")
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.expectedErr)
			}
			if !strings.Contains(err.Error(), tc.expectedErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.expectedErr)
			}
		})
	}
}

func TestValidateGitHubTagMismatch(t *testing.T) {
	m, err := manifest.Decode([]byte(validManifestJSON))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	err = m.Validate("v0.17.0")
	if err == nil || !strings.Contains(err.Error(), "GitHub release tag mismatch") {
		t.Errorf("expected GitHub release tag mismatch, got: %v", err)
	}
}

func TestCheckUpgradeEligibility(t *testing.T) {
	m, err := manifest.Decode([]byte(validManifestJSON))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// 0.15.0 >= 0.15.0 -> eligible
	if err := m.CheckEligibility("0.15.0"); err != nil {
		t.Errorf("expected 0.15.0 to be eligible, got: %v", err)
	}

	// 0.16.0 >= 0.15.0 -> eligible
	if err := m.CheckEligibility("0.16.0"); err != nil {
		t.Errorf("expected 0.16.0 to be eligible, got: %v", err)
	}

	// 0.14.0 < 0.15.0 -> ineligible
	if err := m.CheckEligibility("0.14.0"); err == nil {
		t.Error("expected 0.14.0 to be ineligible, got nil error")
	}
}

func TestSchemaNeutralConsistency(t *testing.T) {
	m, err := manifest.Decode([]byte(validManifestJSON))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Target schema is 8; current schema is 9 -> inconsistent for schema_neutral
	err = m.ValidateSchemaCompatibility(9)
	if err == nil || !strings.Contains(err.Error(), "schema_neutral rollback requires target_schema") {
		t.Errorf("expected schema compatibility error, got: %v", err)
	}
}
