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
			json:        strings.Replace(validManifestJSON, `"manifest_version": 1`, `"manifest_version": 4`, 1),
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

const validManifestV2JSON = `{
  "manifest_version": 2,
  "release_version": "0.17.0",
  "release_tag": "v0.17.0",
  "target_schema": 9,
  "update_classification": "schema_forward",
  "rollback_classification": "backup_restore_required",
  "supported_source_schemas": [8],
  "min_upgrade_from": "0.15.0",
  "images": {
    "backend": {
      "repository": "ghcr.io/der-felix/ytmdl-backend",
      "tag": "0.17.0",
      "digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111"
    },
    "frontend": {
      "repository": "ghcr.io/der-felix/ytmdl-frontend",
      "tag": "0.17.0",
      "digest": "sha256:2222222222222222222222222222222222222222222222222222222222222222"
    }
  },
  "required_env": ["POSTGRES_PASSWORD", "MUSICDL_DATABASE_URL"]
}`

func TestDecodeValidManifestV2_SchemaForward(t *testing.T) {
	m, err := manifest.Decode([]byte(validManifestV2JSON))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if m.ManifestVersion != 2 {
		t.Errorf("ManifestVersion = %d, want 2", m.ManifestVersion)
	}
	if !m.IsSchemaForward() {
		t.Error("expected IsSchemaForward() to be true")
	}
	if m.UpdateClassification != manifest.UpdateSchemaForward {
		t.Errorf("UpdateClassification = %q, want schema_forward", m.UpdateClassification)
	}
	if m.RollbackClassification != manifest.RollbackBackupRestoreRequired {
		t.Errorf("RollbackClassification = %q, want backup_restore_required", m.RollbackClassification)
	}
	if m.TargetSchema != 9 {
		t.Errorf("TargetSchema = %d, want 9", m.TargetSchema)
	}

	if err := m.Validate("v0.17.0"); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Schema compatibility: schema 8 must pass
	if err := m.ValidateSchemaCompatibility(8); err != nil {
		t.Errorf("expected schema 8 to be compatible, got: %v", err)
	}

	// Schema compatibility: schema 7 must fail
	if err := m.ValidateSchemaCompatibility(7); err == nil {
		t.Error("expected schema 7 to be incompatible, got nil")
	}

	// Schema compatibility: schema 9 must fail (already at target)
	if err := m.ValidateSchemaCompatibility(9); err == nil {
		t.Error("expected schema 9 to be incompatible with supported [8], got nil")
	}
}

func TestManifestV2_InvalidCases(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		expectedErr string
	}{
		{
			name:        "v2 schema_forward with wrong rollback classification",
			json:        strings.Replace(validManifestV2JSON, `"backup_restore_required"`, `"schema_neutral"`, 1),
			expectedErr: "schema_forward update requires rollback_classification",
		},
		{
			name:        "v2 schema_forward with empty supported_source_schemas",
			json:        strings.Replace(validManifestV2JSON, `"supported_source_schemas": [8],`, `"supported_source_schemas": [],`, 1),
			expectedErr: "schema_forward manifest requires non-empty supported_source_schemas",
		},
		{
			name:        "v2 schema_forward with source schema >= target schema",
			json:        strings.Replace(validManifestV2JSON, `"supported_source_schemas": [8],`, `"supported_source_schemas": [9],`, 1),
			expectedErr: "must be strictly less than target_schema",
		},
		{
			name:        "v2 schema_forward with unsupported target schema",
			json:        strings.Replace(validManifestV2JSON, `"target_schema": 9,`, `"target_schema": 15,`, 1),
			expectedErr: "unsupported target_schema 15 for v0.17",
		},
		{
			name:        "v2 schema_forward without schema 8 supported",
			json:        strings.Replace(validManifestV2JSON, `"supported_source_schemas": [8],`, `"supported_source_schemas": [7],`, 1),
			expectedErr: "supported_source_schemas must contain 8",
		},
		{
			name:        "v2 unknown update classification",
			json:        strings.Replace(validManifestV2JSON, `"schema_forward"`, `"unknown_class"`, 1),
			expectedErr: "unsupported update classification",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, err := manifest.Decode([]byte(tc.json))
			if err == nil {
				err = m.Validate("v0.17.0")
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

const validManifestV3JSON = `{
  "manifest_version": 3,
  "release_version": "0.17.3",
  "release_tag": "v0.17.3",
  "target_schema": 9,
  "min_upgrade_from": "0.15.0",
  "upgrade_paths": [
    {
      "source_schema": 8,
      "target_schema": 9,
      "update_classification": "schema_forward",
      "rollback_classification": "backup_restore_required"
    },
    {
      "source_schema": 9,
      "target_schema": 9,
      "update_classification": "schema_neutral",
      "rollback_classification": "schema_neutral"
    }
  ],
  "images": {
    "backend": {
      "repository": "ghcr.io/der-felix/ytmdl-backend",
      "tag": "0.17.3",
      "digest": "sha256:0000000000000000000000000000000000000000000000000000000000000001",
      "platforms": {
        "linux/amd64": {
          "digest": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
        },
        "linux/arm64": {
          "digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
        }
      }
    },
    "frontend": {
      "repository": "ghcr.io/der-felix/ytmdl-frontend",
      "tag": "0.17.3",
      "digest": "sha256:0000000000000000000000000000000000000000000000000000000000000002",
      "platforms": {
        "linux/amd64": {
          "digest": "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
        },
        "linux/arm64": {
          "digest": "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
        }
      }
    }
  },
  "required_env": ["POSTGRES_PASSWORD", "MUSICDL_DATABASE_URL"]
}`

func TestDecodeValidManifestV3_MultiArchAndUpgradePaths(t *testing.T) {
	m, err := manifest.Decode([]byte(validManifestV3JSON))
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if m.ManifestVersion != 3 {
		t.Errorf("ManifestVersion = %d, want 3", m.ManifestVersion)
	}
	if err := m.Validate("v0.17.3"); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	// Upgrade path from schema 8: schema_forward, backup_restore_required
	p8, err := m.FindUpgradePath(8)
	if err != nil {
		t.Fatalf("FindUpgradePath(8) failed: %v", err)
	}
	if p8.UpdateClassification != manifest.UpdateSchemaForward {
		t.Errorf("path(8) UpdateClassification = %q, want schema_forward", p8.UpdateClassification)
	}
	if p8.RollbackClassification != manifest.RollbackBackupRestoreRequired {
		t.Errorf("path(8) RollbackClassification = %q, want backup_restore_required", p8.RollbackClassification)
	}
	if !m.IsSchemaForwardFor(8) {
		t.Error("expected IsSchemaForwardFor(8) to be true")
	}

	// Upgrade path from schema 9: schema_neutral, schema_neutral
	p9, err := m.FindUpgradePath(9)
	if err != nil {
		t.Fatalf("FindUpgradePath(9) failed: %v", err)
	}
	if p9.UpdateClassification != manifest.UpdateSchemaNeutral {
		t.Errorf("path(9) UpdateClassification = %q, want schema_neutral", p9.UpdateClassification)
	}
	if p9.RollbackClassification != manifest.RollbackSchemaNeutral {
		t.Errorf("path(9) RollbackClassification = %q, want schema_neutral", p9.RollbackClassification)
	}
	if m.IsSchemaForwardFor(9) {
		t.Error("expected IsSchemaForwardFor(9) to be false")
	}

	// Unsupported schema 7: must fail
	if _, err := m.FindUpgradePath(7); err == nil {
		t.Error("expected FindUpgradePath(7) to fail, got nil")
	}
	if err := m.ValidateSchemaCompatibility(7); err == nil {
		t.Error("expected ValidateSchemaCompatibility(7) to fail, got nil")
	}

	// Digest verification for amd64: index + amd64 digest
	backendAmd64, err := m.GetExpectedDigests(m.Images.Backend, "linux/amd64")
	if err != nil {
		t.Fatalf("GetExpectedDigests(backend, linux/amd64) failed: %v", err)
	}
	if len(backendAmd64) != 2 || backendAmd64[0] != "sha256:0000000000000000000000000000000000000000000000000000000000000001" || backendAmd64[1] != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("unexpected backend amd64 digests: %v", backendAmd64)
	}

	// Digest verification for arm64 (and normalized aarch64): index + arm64 digest
	backendArm64, err := m.GetExpectedDigests(m.Images.Backend, "aarch64")
	if err != nil {
		t.Fatalf("GetExpectedDigests(backend, aarch64) failed: %v", err)
	}
	if len(backendArm64) != 2 || backendArm64[1] != "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("unexpected backend arm64 digests: %v", backendArm64)
	}

	// Unsupported platform: must fail
	if _, err := m.GetExpectedDigests(m.Images.Backend, "linux/riscv64"); err == nil {
		t.Error("expected GetExpectedDigests for riscv64 to fail, got nil")
	}
}

func TestManifestV3_InvalidCases(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		expectedErr string
	}{
		{
			name: "v3 empty upgrade_paths",
			json: strings.Replace(validManifestV3JSON, `"upgrade_paths": [
    {
      "source_schema": 8,
      "target_schema": 9,
      "update_classification": "schema_forward",
      "rollback_classification": "backup_restore_required"
    },
    {
      "source_schema": 9,
      "target_schema": 9,
      "update_classification": "schema_neutral",
      "rollback_classification": "schema_neutral"
    }
  ],`, `"upgrade_paths": [],`, 1),
			expectedErr: "manifest v3 requires non-empty upgrade_paths",
		},
		{
			name:        "v3 duplicate source_schema",
			json:        strings.Replace(validManifestV3JSON, `"source_schema": 9,`, `"source_schema": 8,`, 1),
			expectedErr: "duplicate source_schema 8 in upgrade_paths",
		},
		{
			name:        "v3 target_schema mismatch in path",
			json:        strings.Replace(validManifestV3JSON, `"target_schema": 9,`+"\n"+`      "update_classification": "schema_forward"`, `"target_schema": 10,`+"\n"+`      "update_classification": "schema_forward"`, 1),
			expectedErr: "does not match manifest target_schema",
		},
		{
			name: "v3 missing arm64 platform in backend",
			json: strings.Replace(validManifestV3JSON, `,
        "linux/arm64": {
          "digest": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
        }`, "", 1),
			expectedErr: "manifest v3 requires linux/arm64 platform for backend",
		},
		{
			name:        "v3 platform digest identical to index digest rejected",
			json:        strings.Replace(validManifestV3JSON, `"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`, `"sha256:0000000000000000000000000000000000000000000000000000000000000001"`, 1),
			expectedErr: "cannot be identical to OCI index digest",
		},
		{
			name:        "v3 schema_forward source >= target",
			json:        strings.Replace(validManifestV3JSON, `"source_schema": 8,`+"\n"+`      "target_schema": 9,`+"\n"+`      "update_classification": "schema_forward"`, `"source_schema": 9,`+"\n"+`      "target_schema": 9,`+"\n"+`      "update_classification": "schema_forward"`, 1),
			expectedErr: "must be strictly less than target_schema",
		},
		{
			name:        "v3 schema_neutral source != target",
			json:        strings.Replace(validManifestV3JSON, `"source_schema": 9,`+"\n"+`      "target_schema": 9,`+"\n"+`      "update_classification": "schema_neutral"`, `"source_schema": 7,`+"\n"+`      "target_schema": 9,`+"\n"+`      "update_classification": "schema_neutral"`, 1),
			expectedErr: "must equal target_schema",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, err := manifest.Decode([]byte(tc.json))
			if err == nil {
				err = m.Validate("v0.17.3")
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

func TestManifestV3_AmbiguousUpgradePaths(t *testing.T) {
	m := &manifest.Manifest{
		ManifestVersion: manifest.ManifestVersion3,
		TargetSchema:    9,
		UpgradePaths: []manifest.UpgradePath{
			{SourceSchema: 8, TargetSchema: 9},
			{SourceSchema: 8, TargetSchema: 9},
		},
	}
	_, err := m.FindUpgradePath(8)
	if err == nil || !strings.Contains(err.Error(), "ambiguous upgrade paths") {
		t.Fatalf("expected ambiguous upgrade paths error, got: %v", err)
	}
}
