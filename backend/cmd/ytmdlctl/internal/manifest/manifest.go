// Package manifest defines the release-manifest.json schema and validation rules.
package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"

	"ytdm/backend/internal/update"
)

// CurrentManifestVersion is the supported schema version.
const CurrentManifestVersion = 1

// MaxManifestBytes bounds the size of a release manifest asset.
const MaxManifestBytes = 64 * 1024

// Canonical public repositories for images.
const (
	ExpectedBackendRepo  = "ghcr.io/der-felix/ytmdl-backend"
	ExpectedFrontendRepo = "ghcr.io/der-felix/ytmdl-frontend"
)

// RollbackClassification defines supported rollback classifications.
type RollbackClassification string

const (
	RollbackSchemaNeutral RollbackClassification = "schema_neutral"
)

var (
	digestRegex  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	envNameRegex = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

// ImageSpec defines image repository, tag, and expected sha256 digest.
type ImageSpec struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Digest     string `json:"digest"`
}

// Manifest defines the structure of release-manifest.json.
type Manifest struct {
	ManifestVersion        int                    `json:"manifest_version"`
	ReleaseVersion         string                 `json:"release_version"`
	ReleaseTag             string                 `json:"release_tag"`
	TargetSchema           int                    `json:"target_schema"`
	RollbackClassification RollbackClassification `json:"rollback_classification"`
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
	if m.ManifestVersion != CurrentManifestVersion {
		return fmt.Errorf("unsupported manifest version %d (expected %d)", m.ManifestVersion, CurrentManifestVersion)
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

	if m.Images.Backend.Repository != ExpectedBackendRepo {
		return fmt.Errorf("backend image repository not allowed: %q (expected %q)", m.Images.Backend.Repository, ExpectedBackendRepo)
	}
	if m.Images.Backend.Tag != m.ReleaseVersion {
		return fmt.Errorf("backend image tag %q does not match release_version %q", m.Images.Backend.Tag, m.ReleaseVersion)
	}
	if !digestRegex.MatchString(m.Images.Backend.Digest) {
		return fmt.Errorf("invalid digest syntax for backend: %q", m.Images.Backend.Digest)
	}

	if m.Images.Frontend.Repository != ExpectedFrontendRepo {
		return fmt.Errorf("frontend image repository not allowed: %q (expected %q)", m.Images.Frontend.Repository, ExpectedFrontendRepo)
	}
	if m.Images.Frontend.Tag != m.ReleaseVersion {
		return fmt.Errorf("frontend image tag %q does not match release_version %q", m.Images.Frontend.Tag, m.ReleaseVersion)
	}
	if !digestRegex.MatchString(m.Images.Frontend.Digest) {
		return fmt.Errorf("invalid digest syntax for frontend: %q", m.Images.Frontend.Digest)
	}

	switch m.RollbackClassification {
	case RollbackSchemaNeutral:
		// Valid
	default:
		return fmt.Errorf("unsupported rollback classification %q", m.RollbackClassification)
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

// ValidateSchemaCompatibility verifies semantic consistency between rollback classification and current schema.
func (m *Manifest) ValidateSchemaCompatibility(currentSchema int) error {
	if m.RollbackClassification == RollbackSchemaNeutral {
		if currentSchema > 0 && m.TargetSchema != currentSchema {
			return fmt.Errorf("inconsistent manifest: schema_neutral rollback requires target_schema %d to match current_schema %d", m.TargetSchema, currentSchema)
		}
	}
	return nil
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
