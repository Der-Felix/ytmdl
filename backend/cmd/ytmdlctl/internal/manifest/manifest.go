// Package manifest defines the release-manifest.json schema and validation rules.
package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"ytdm/backend/internal/update"
)

// CurrentManifestVersion is the supported schema version.
const CurrentManifestVersion = 3

// Supported manifest versions: 1, 2, and 3.
const (
	ManifestVersion1 = 1
	ManifestVersion2 = 2
	ManifestVersion3 = 3
)

// MaxManifestBytes bounds the size of a release manifest asset.
const MaxManifestBytes = 64 * 1024

// Canonical public repositories for images.
const (
	ExpectedBackendRepo  = "ghcr.io/der-felix/ytmdl-backend"
	ExpectedFrontendRepo = "ghcr.io/der-felix/ytmdl-frontend"
)

// UpdateClassification defines supported update classifications.
type UpdateClassification string

const (
	UpdateSchemaNeutral UpdateClassification = "schema_neutral"
	UpdateSchemaForward UpdateClassification = "schema_forward"
)

// RollbackClassification defines supported rollback classifications.
type RollbackClassification string

const (
	RollbackSchemaNeutral         RollbackClassification = "schema_neutral"
	RollbackBackupRestoreRequired RollbackClassification = "backup_restore_required"
)

var (
	digestRegex  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	envNameRegex = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

// PlatformSpec defines the digest for a specific target platform.
type PlatformSpec struct {
	Digest string `json:"digest"`
}

// UpgradePath defines an explicit upgrade path from a specific source schema.
type UpgradePath struct {
	SourceSchema           int                    `json:"source_schema"`
	TargetSchema           int                    `json:"target_schema"`
	UpdateClassification   UpdateClassification   `json:"update_classification"`
	RollbackClassification RollbackClassification `json:"rollback_classification"`
}

// ImageSpec defines image repository, tag, expected sha256 digest, and optional platform map.
type ImageSpec struct {
	Repository string                  `json:"repository"`
	Tag        string                  `json:"tag"`
	Digest     string                  `json:"digest"`
	Platforms  map[string]PlatformSpec `json:"platforms,omitempty"`
}

// Manifest defines the structure of release-manifest.json.
type Manifest struct {
	ManifestVersion        int                    `json:"manifest_version"`
	ReleaseVersion         string                 `json:"release_version"`
	ReleaseTag             string                 `json:"release_tag"`
	TargetSchema           int                    `json:"target_schema"`
	UpdateClassification   UpdateClassification   `json:"update_classification,omitempty"`
	RollbackClassification RollbackClassification `json:"rollback_classification,omitempty"`
	SupportedSourceSchemas []int                  `json:"supported_source_schemas,omitempty"`
	UpgradePaths           []UpgradePath          `json:"upgrade_paths,omitempty"`
	MinUpgradeFrom         string                 `json:"min_upgrade_from"`
	Images                 struct {
		Backend  ImageSpec `json:"backend"`
		Frontend ImageSpec `json:"frontend"`
	} `json:"images"`
	RequiredEnv []string `json:"required_env"`
}

// Decode parses and strictly validates the JSON format of a manifest.
func Decode(data []byte) (*Manifest, error) {
	if len(data) == 0 {
		return nil, errors.New("empty manifest document")
	}
	if len(data) > MaxManifestBytes {
		return nil, fmt.Errorf("manifest exceeds maximum allowed size of %d bytes", MaxManifestBytes)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var m Manifest
	if err := decoder.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest decode failed: %w", err)
	}

	// Reject trailing garbage
	var dummy any
	if err := decoder.Decode(&dummy); !errors.Is(err, io.EOF) {
		return nil, errors.New("manifest contains trailing garbage after JSON object")
	}

	return &m, nil
}

// Validate executes all integrity, schema, tag consistency, and allowlist checks on the manifest.
func (m *Manifest) Validate(gitHubTag string) error {
	if m.ManifestVersion < ManifestVersion1 || m.ManifestVersion > ManifestVersion3 {
		return fmt.Errorf("unsupported manifest version %d (expected 1, 2, or 3)", m.ManifestVersion)
	}

	if m.ReleaseVersion == "" || m.ReleaseTag == "" {
		return errors.New("manifest missing release_version or release_tag")
	}

	expectedTag := "v" + m.ReleaseVersion
	if m.ReleaseTag != expectedTag {
		return fmt.Errorf("release_version and release_tag mismatch: %s vs %s", m.ReleaseVersion, m.ReleaseTag)
	}

	if gitHubTag != "" && m.ReleaseTag != gitHubTag {
		return fmt.Errorf("GitHub release tag mismatch: manifest has %s, GitHub has %s", m.ReleaseTag, gitHubTag)
	}

	if m.TargetSchema <= 0 {
		return errors.New("manifest missing or invalid target_schema")
	}

	validateImage := func(name string, img ImageSpec, expectedRepo string) error {
		if img.Repository != expectedRepo {
			return fmt.Errorf("%s image repository not allowed: %q (expected %q)", name, img.Repository, expectedRepo)
		}
		if img.Tag != m.ReleaseVersion {
			return fmt.Errorf("%s image tag %q does not match release_version %q", name, img.Tag, m.ReleaseVersion)
		}
		if !digestRegex.MatchString(img.Digest) {
			return fmt.Errorf("invalid digest syntax for %s: %q", name, img.Digest)
		}
		for plat, spec := range img.Platforms {
			norm, err := NormalizePlatform(plat)
			if err != nil {
				return fmt.Errorf("invalid platform %q for %s: %w", plat, name, err)
			}
			if norm != plat {
				return fmt.Errorf("platform %q for %s must be normalized (expected %q)", plat, name, norm)
			}
			if !digestRegex.MatchString(spec.Digest) {
				return fmt.Errorf("invalid platform digest syntax for %s (%s): %q", name, plat, spec.Digest)
			}
		}
		return nil
	}

	if err := validateImage("backend", m.Images.Backend, ExpectedBackendRepo); err != nil {
		return err
	}
	if err := validateImage("frontend", m.Images.Frontend, ExpectedFrontendRepo); err != nil {
		return err
	}

	if m.ManifestVersion == ManifestVersion1 {
		if m.RollbackClassification != RollbackSchemaNeutral {
			return fmt.Errorf("unsupported rollback classification %q for manifest version 1", m.RollbackClassification)
		}
		if m.UpdateClassification != "" && m.UpdateClassification != UpdateSchemaNeutral {
			return fmt.Errorf("unsupported update classification %q for manifest version 1", m.UpdateClassification)
		}
	} else if m.ManifestVersion == ManifestVersion2 {
		switch m.UpdateClassification {
		case UpdateSchemaForward:
			if m.RollbackClassification != RollbackBackupRestoreRequired {
				return fmt.Errorf("schema_forward update requires rollback_classification %q, got %q", RollbackBackupRestoreRequired, m.RollbackClassification)
			}
			if len(m.SupportedSourceSchemas) == 0 {
				return errors.New("schema_forward manifest requires non-empty supported_source_schemas")
			}
			has8 := false
			for _, src := range m.SupportedSourceSchemas {
				if src >= m.TargetSchema {
					return fmt.Errorf("supported source schema %d must be strictly less than target_schema %d", src, m.TargetSchema)
				}
				if src == 8 {
					has8 = true
				}
			}
			if m.TargetSchema != 9 {
				return fmt.Errorf("unsupported target_schema %d for v0.17 schema_forward (expected 9)", m.TargetSchema)
			}
			if !has8 {
				return fmt.Errorf("supported_source_schemas must contain 8 for v0.17 schema_forward update")
			}
		case UpdateSchemaNeutral:
			if m.RollbackClassification != RollbackSchemaNeutral {
				return fmt.Errorf("schema_neutral update requires rollback_classification %q, got %q", RollbackSchemaNeutral, m.RollbackClassification)
			}
		default:
			return fmt.Errorf("unsupported update classification %q for manifest version 2", m.UpdateClassification)
		}
	} else if m.ManifestVersion == ManifestVersion3 {
		if len(m.UpgradePaths) == 0 {
			return errors.New("manifest v3 requires non-empty upgrade_paths")
		}
		seenSources := make(map[int]bool, len(m.UpgradePaths))
		for _, p := range m.UpgradePaths {
			if p.TargetSchema != m.TargetSchema {
				return fmt.Errorf("upgrade path target_schema %d does not match manifest target_schema %d", p.TargetSchema, m.TargetSchema)
			}
			if p.SourceSchema <= 0 {
				return fmt.Errorf("invalid source_schema %d in upgrade_paths", p.SourceSchema)
			}
			if seenSources[p.SourceSchema] {
				return fmt.Errorf("duplicate source_schema %d in upgrade_paths", p.SourceSchema)
			}
			seenSources[p.SourceSchema] = true

			switch p.UpdateClassification {
			case UpdateSchemaForward:
				if p.SourceSchema >= p.TargetSchema {
					return fmt.Errorf("schema_forward source_schema %d must be strictly less than target_schema %d", p.SourceSchema, p.TargetSchema)
				}
				if p.RollbackClassification != RollbackBackupRestoreRequired {
					return fmt.Errorf("schema_forward path (%d -> %d) requires rollback_classification %q, got %q", p.SourceSchema, p.TargetSchema, RollbackBackupRestoreRequired, p.RollbackClassification)
				}
			case UpdateSchemaNeutral:
				if p.SourceSchema != p.TargetSchema {
					return fmt.Errorf("schema_neutral path source_schema %d must equal target_schema %d", p.SourceSchema, p.TargetSchema)
				}
				if p.RollbackClassification != RollbackSchemaNeutral {
					return fmt.Errorf("schema_neutral path (%d -> %d) requires rollback_classification %q, got %q", p.SourceSchema, p.TargetSchema, RollbackSchemaNeutral, p.RollbackClassification)
				}
			default:
				return fmt.Errorf("unsupported update classification %q in upgrade_paths", p.UpdateClassification)
			}
		}

		checkRequiredPlatforms := func(name string, img ImageSpec) error {
			if len(img.Platforms) == 0 {
				return fmt.Errorf("manifest v3 requires platforms map for %s", name)
			}
			for _, requiredPlat := range []string{"linux/amd64", "linux/arm64"} {
				spec, ok := img.Platforms[requiredPlat]
				if !ok {
					return fmt.Errorf("manifest v3 requires %s platform for %s", requiredPlat, name)
				}
				if strings.EqualFold(strings.TrimSpace(spec.Digest), strings.TrimSpace(img.Digest)) {
					return fmt.Errorf("platform digest for %s (%s) cannot be identical to OCI index digest", name, requiredPlat)
				}
			}
			return nil
		}

		if err := checkRequiredPlatforms("backend", m.Images.Backend); err != nil {
			return err
		}
		if err := checkRequiredPlatforms("frontend", m.Images.Frontend); err != nil {
			return err
		}
	}

	if m.MinUpgradeFrom == "" {
		return errors.New("manifest missing min_upgrade_from")
	}

	minSemVer, err := update.ParseSemVer(m.MinUpgradeFrom)
	if err != nil {
		return fmt.Errorf("invalid min_upgrade_from version %q: %w", m.MinUpgradeFrom, err)
	}

	relSemVer, err := update.ParseSemVer(m.ReleaseVersion)
	if err != nil {
		return fmt.Errorf("invalid release_version %q: %w", m.ReleaseVersion, err)
	}

	if minSemVer.Compare(relSemVer) > 0 {
		return fmt.Errorf("min_upgrade_from %s cannot be greater than release_version %s", m.MinUpgradeFrom, m.ReleaseVersion)
	}

	// Validate required_env names and reject duplicates
	seenEnv := make(map[string]struct{}, len(m.RequiredEnv))
	for _, envKey := range m.RequiredEnv {
		if !envNameRegex.MatchString(envKey) {
			return fmt.Errorf("invalid required_env variable name %q", envKey)
		}
		if _, exists := seenEnv[envKey]; exists {
			return fmt.Errorf("duplicate required_env variable name %q", envKey)
		}
		seenEnv[envKey] = struct{}{}
	}

	return nil
}

// NormalizePlatform normalizes platform string to standard linux/amd64 or linux/arm64.
func NormalizePlatform(p string) (string, error) {
	p = strings.ToLower(strings.TrimSpace(p))
	parts := strings.Split(p, "/")
	if len(parts) == 1 {
		parts = []string{"linux", parts[0]}
	}
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid platform %q (expected os/arch)", p)
	}
	osName := parts[0]
	arch := parts[1]
	if osName != "linux" {
		return "", fmt.Errorf("unsupported OS %q (only linux is supported)", osName)
	}
	switch arch {
	case "amd64", "x86_64":
		return "linux/amd64", nil
	case "arm64", "aarch64":
		return "linux/arm64", nil
	default:
		return "", fmt.Errorf("unsupported architecture %q (expected amd64 or arm64)", arch)
	}
}

// FindUpgradePath finds the upgrade path for the given sourceSchema.
// For Manifest v1/v2, it synthesizes the UpgradePath based on version semantics.
func (m *Manifest) FindUpgradePath(sourceSchema int) (*UpgradePath, error) {
	if sourceSchema <= 0 {
		return nil, errors.New("source schema must be positive")
	}

	if m.ManifestVersion == ManifestVersion3 {
		var matched *UpgradePath
		count := 0
		for _, p := range m.UpgradePaths {
			if p.SourceSchema == sourceSchema {
				pathCopy := p
				matched = &pathCopy
				count++
			}
		}
		if count == 0 {
			return nil, fmt.Errorf("incompatible schema: current schema %d has no upgrade path to target schema %d in manifest v3", sourceSchema, m.TargetSchema)
		}
		if count > 1 {
			return nil, fmt.Errorf("ambiguous upgrade paths: current schema %d matches %d paths in manifest v3", sourceSchema, count)
		}
		return matched, nil
	}

	if m.ManifestVersion == ManifestVersion2 {
		if m.IsSchemaForward() {
			for _, src := range m.SupportedSourceSchemas {
				if src == sourceSchema {
					return &UpgradePath{
						SourceSchema:           sourceSchema,
						TargetSchema:           m.TargetSchema,
						UpdateClassification:   m.UpdateClassification,
						RollbackClassification: m.RollbackClassification,
					}, nil
				}
			}
			return nil, fmt.Errorf("incompatible schema: current schema %d is not in supported source schemas %v for schema_forward update", sourceSchema, m.SupportedSourceSchemas)
		}
		if m.TargetSchema == sourceSchema {
			return &UpgradePath{
				SourceSchema:           sourceSchema,
				TargetSchema:           m.TargetSchema,
				UpdateClassification:   UpdateSchemaNeutral,
				RollbackClassification: RollbackSchemaNeutral,
			}, nil
		}
		return nil, fmt.Errorf("inconsistent manifest: schema_neutral rollback requires target_schema %d to match current_schema %d", m.TargetSchema, sourceSchema)
	}

	// ManifestVersion1
	if m.TargetSchema == sourceSchema {
		return &UpgradePath{
			SourceSchema:           sourceSchema,
			TargetSchema:           m.TargetSchema,
			UpdateClassification:   UpdateSchemaNeutral,
			RollbackClassification: RollbackSchemaNeutral,
		}, nil
	}
	return nil, fmt.Errorf("inconsistent manifest: schema_neutral rollback requires target_schema %d to match current_schema %d", m.TargetSchema, sourceSchema)
}

// IsSchemaForward returns whether this manifest represents a forward schema migration.
func (m *Manifest) IsSchemaForward() bool {
	return m.UpdateClassification == UpdateSchemaForward || m.RollbackClassification == RollbackBackupRestoreRequired
}

// IsSchemaForwardFor returns whether the update from currentSchema represents a forward schema migration.
func (m *Manifest) IsSchemaForwardFor(currentSchema int) bool {
	p, err := m.FindUpgradePath(currentSchema)
	if err != nil {
		return m.IsSchemaForward()
	}
	return p.UpdateClassification == UpdateSchemaForward || p.RollbackClassification == RollbackBackupRestoreRequired
}

// ValidateSchemaCompatibility verifies semantic consistency between upgrade paths and current schema.
func (m *Manifest) ValidateSchemaCompatibility(currentSchema int) error {
	if currentSchema <= 0 {
		return nil
	}
	_, err := m.FindUpgradePath(currentSchema)
	return err
}

// GetExpectedDigests returns the valid expected digests for an image on targetPlatform.
// It includes the OCI index digest (img.Digest) and, if available, the platform-specific digest.
func (m *Manifest) GetExpectedDigests(img ImageSpec, targetPlatform string) ([]string, error) {
	cleanIndex := strings.ToLower(strings.TrimSpace(img.Digest))
	if targetPlatform == "" {
		return []string{cleanIndex}, nil
	}
	normPlatform, err := NormalizePlatform(targetPlatform)
	if err != nil {
		return nil, err
	}

	if len(img.Platforms) == 0 {
		// Single-image manifest (v1/v2)
		return []string{cleanIndex}, nil
	}

	spec, ok := img.Platforms[normPlatform]
	if !ok {
		return nil, fmt.Errorf("platform %s is not supported by image %s:%s", normPlatform, img.Repository, img.Tag)
	}

	res := []string{cleanIndex}
	platDigest := strings.ToLower(strings.TrimSpace(spec.Digest))
	if platDigest != "" && platDigest != cleanIndex {
		res = append(res, platDigest)
	}
	return res, nil
}

// CheckEligibility checks if currentVersion satisfies MinUpgradeFrom.
func (m *Manifest) CheckEligibility(currentVersion string) error {
	if m.MinUpgradeFrom == "" {
		return errors.New("manifest missing min_upgrade_from")
	}

	minSemVer, err := update.ParseSemVer(m.MinUpgradeFrom)
	if err != nil {
		return fmt.Errorf("invalid min_upgrade_from %q: %w", m.MinUpgradeFrom, err)
	}

	curSemVer, err := update.ParseSemVer(currentVersion)
	if err != nil {
		return fmt.Errorf("invalid current version %q: %w", currentVersion, err)
	}

	if curSemVer.Compare(minSemVer) < 0 {
		return fmt.Errorf("current version %s is below minimum upgrade version %s", currentVersion, m.MinUpgradeFrom)
	}

	return nil
}
