package manifest_test

import (
	"testing"

	"ytdm/backend/cmd/ytmdlctl/internal/manifest"
)

const (
	validDigest1 = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	validDigest2 = "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

func TestGenerateValidManifest(t *testing.T) {
	opts := manifest.GeneratorOptions{
		ReleaseVersion:         "0.16.0",
		ReleaseTag:             "v0.16.0",
		TargetSchema:           8,
		RollbackClassification: manifest.RollbackSchemaNeutral,
		MinUpgradeFrom:         "0.15.0",
		BackendDigest:          validDigest1,
		FrontendDigest:         validDigest2,
		RequiredEnv:            []string{"POSTGRES_PASSWORD", "MUSICDL_DATABASE_URL"},
	}

	data, err := manifest.Generate(opts)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Verify decoding and validating generated manifest
	decoded, err := manifest.Decode(data)
	if err != nil {
		t.Fatalf("Decode of generated manifest failed: %v", err)
	}

	if err := decoded.Validate("v0.16.0"); err != nil {
		t.Fatalf("Validate of generated manifest failed: %v", err)
	}

	if decoded.ReleaseVersion != "0.16.0" {
		t.Errorf("expected ReleaseVersion 0.16.0, got %s", decoded.ReleaseVersion)
	}
	if decoded.ReleaseTag != "v0.16.0" {
		t.Errorf("expected ReleaseTag v0.16.0, got %s", decoded.ReleaseTag)
	}
	if decoded.TargetSchema != 8 {
		t.Errorf("expected TargetSchema 8, got %d", decoded.TargetSchema)
	}
	if decoded.Images.Backend.Digest != validDigest1 {
		t.Errorf("backend digest mismatch")
	}
	if decoded.Images.Frontend.Digest != validDigest2 {
		t.Errorf("frontend digest mismatch")
	}
	if len(decoded.RequiredEnv) != 2 {
		t.Errorf("expected 2 required envs, got %d", len(decoded.RequiredEnv))
	}
}

func TestGenerateDefaultTag(t *testing.T) {
	opts := manifest.GeneratorOptions{
		ReleaseVersion: "0.16.0",
		// ReleaseTag left empty, should default to "v0.16.0"
		TargetSchema:   8,
		MinUpgradeFrom: "0.15.0",
		BackendDigest:  validDigest1,
		FrontendDigest: validDigest2,
	}

	data, err := manifest.Generate(opts)
	if err != nil {
		t.Fatalf("Generate failed with default tag: %v", err)
	}

	decoded, err := manifest.Decode(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if decoded.ReleaseTag != "v0.16.0" {
		t.Errorf("expected default tag v0.16.0, got %s", decoded.ReleaseTag)
	}
}

func TestGenerateInvalidScenarios(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*manifest.GeneratorOptions)
		wantErr string
	}{
		{
			name: "empty release version",
			mutate: func(o *manifest.GeneratorOptions) {
				o.ReleaseVersion = ""
			},
			wantErr: "release version is required",
		},
		{
			name: "mismatched release tag",
			mutate: func(o *manifest.GeneratorOptions) {
				o.ReleaseTag = "v0.17.0"
			},
			wantErr: "mismatch",
		},
		{
			name: "malformed release version",
			mutate: func(o *manifest.GeneratorOptions) {
				o.ReleaseVersion = "not-semver"
				o.ReleaseTag = "vnot-semver"
			},
			wantErr: "invalid release_version",
		},
		{
			name: "empty min_upgrade_from",
			mutate: func(o *manifest.GeneratorOptions) {
				o.MinUpgradeFrom = ""
			},
			wantErr: "min_upgrade_from is required",
		},
		{
			name: "min_upgrade_from higher than release version",
			mutate: func(o *manifest.GeneratorOptions) {
				o.MinUpgradeFrom = "0.17.0"
			},
			wantErr: "cannot be greater than release_version",
		},
		{
			name: "invalid target schema",
			mutate: func(o *manifest.GeneratorOptions) {
				o.TargetSchema = 0
			},
			wantErr: "target_schema must be greater than 0",
		},
		{
			name: "empty backend digest",
			mutate: func(o *manifest.GeneratorOptions) {
				o.BackendDigest = ""
			},
			wantErr: "backend digest is required",
		},
		{
			name: "malformed backend digest",
			mutate: func(o *manifest.GeneratorOptions) {
				o.BackendDigest = "sha256:tooshort"
			},
			wantErr: "invalid digest syntax for backend",
		},
		{
			name: "empty frontend digest",
			mutate: func(o *manifest.GeneratorOptions) {
				o.FrontendDigest = ""
			},
			wantErr: "frontend digest is required",
		},
		{
			name: "malformed frontend digest",
			mutate: func(o *manifest.GeneratorOptions) {
				o.FrontendDigest = "md5:abcdef"
			},
			wantErr: "invalid digest syntax for frontend",
		},
		{
			name: "unsupported rollback classification",
			mutate: func(o *manifest.GeneratorOptions) {
				o.RollbackClassification = "auto_migrate"
			},
			wantErr: "unsupported rollback classification",
		},
		{
			name: "invalid required env name",
			mutate: func(o *manifest.GeneratorOptions) {
				o.RequiredEnv = []string{"123_INVALID"}
			},
			wantErr: "invalid required_env variable name",
		},
		{
			name: "duplicate required env name",
			mutate: func(o *manifest.GeneratorOptions) {
				o.RequiredEnv = []string{"POSTGRES_USER", "POSTGRES_USER"}
			},
			wantErr: "duplicate required_env variable name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := manifest.GeneratorOptions{
				ReleaseVersion:         "0.16.0",
				ReleaseTag:             "v0.16.0",
				TargetSchema:           8,
				RollbackClassification: manifest.RollbackSchemaNeutral,
				MinUpgradeFrom:         "0.15.0",
				BackendDigest:          validDigest1,
				FrontendDigest:         validDigest2,
			}
			tc.mutate(&opts)

			_, err := manifest.Generate(opts)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if tc.wantErr != "" && !containsStr(err.Error(), tc.wantErr) {
				t.Errorf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (len(sub) > 0 && len(s) > 0 && searchSub(s, sub)))
}

func searchSub(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestGenerateSchema11Manifest(t *testing.T) {
	opts := manifest.GeneratorOptions{
		ManifestVersion: manifest.ManifestVersion3,
		ReleaseVersion:  "0.20.0",
		ReleaseTag:      "v0.20.0",
		TargetSchema:    11,
		MinUpgradeFrom:  "0.15.0",
		BackendDigest:   validDigest1,
		BackendPlatforms: map[string]string{
			"linux/amd64": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			"linux/arm64": "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		},
		FrontendDigest: validDigest2,
		FrontendPlatforms: map[string]string{
			"linux/amd64": "sha256:3333333333333333333333333333333333333333333333333333333333333333",
			"linux/arm64": "sha256:4444444444444444444444444444444444444444444444444444444444444444",
		},
	}

	data, err := manifest.Generate(opts)
	if err != nil {
		t.Fatalf("Generate Schema 11 manifest failed: %v", err)
	}

	decoded, err := manifest.Decode(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decoded.TargetSchema != 11 {
		t.Fatalf("expected TargetSchema 11, got %d", decoded.TargetSchema)
	}

	// Verify 10 -> 11 path is forward migration and requires backup restore on rollback
	path10, err := decoded.FindUpgradePath(10)
	if err != nil {
		t.Fatalf("FindUpgradePath(10) failed: %v", err)
	}
	if path10.UpdateClassification != manifest.UpdateSchemaForward {
		t.Errorf("expected 10->11 update_classification schema_forward, got %s", path10.UpdateClassification)
	}
	if path10.RollbackClassification != manifest.RollbackBackupRestoreRequired {
		t.Errorf("expected 10->11 rollback_classification backup_restore_required, got %s", path10.RollbackClassification)
	}

	// Verify 11 -> 11 path is schema neutral
	path11, err := decoded.FindUpgradePath(11)
	if err != nil {
		t.Fatalf("FindUpgradePath(11) failed: %v", err)
	}
	if path11.UpdateClassification != manifest.UpdateSchemaNeutral {
		t.Errorf("expected 11->11 update_classification schema_neutral, got %s", path11.UpdateClassification)
	}
	if path11.RollbackClassification != manifest.RollbackSchemaNeutral {
		t.Errorf("expected 11->11 rollback_classification schema_neutral, got %s", path11.RollbackClassification)
	}
}
