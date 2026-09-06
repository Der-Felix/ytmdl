// Package manifest provides deterministic generation and validation of release-manifest.json.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// GeneratorOptions contains all inputs needed to construct a release-manifest.json.
type GeneratorOptions struct {
	ManifestVersion        int
	ReleaseVersion         string
	ReleaseTag             string
	TargetSchema           int
	UpdateClassification   UpdateClassification
	RollbackClassification RollbackClassification
	SupportedSourceSchemas []int
	UpgradePaths           []UpgradePath
	MinUpgradeFrom         string
	BackendDigest          string
	BackendPlatforms       map[string]string // platform (e.g. linux/amd64) -> digest
	FrontendDigest         string
	FrontendPlatforms      map[string]string // platform (e.g. linux/amd64) -> digest
	RequiredEnv            []string
}

// Generate produces formatted, validated JSON bytes for release-manifest.json.
func Generate(opts GeneratorOptions) ([]byte, error) {
	relVer := strings.TrimSpace(opts.ReleaseVersion)
	if relVer == "" {
		return nil, errors.New("release version is required")
	}

	relTag := strings.TrimSpace(opts.ReleaseTag)
	if relTag == "" {
		relTag = "v" + relVer
	}

	minUp := strings.TrimSpace(opts.MinUpgradeFrom)
	if minUp == "" {
		return nil, errors.New("min_upgrade_from is required")
	}

	if opts.TargetSchema <= 0 {
		return nil, errors.New("target_schema must be greater than 0")
	}

	mVer := opts.ManifestVersion
	if mVer == 0 {
		if len(opts.UpgradePaths) > 0 || len(opts.BackendPlatforms) > 0 {
			mVer = ManifestVersion3
		} else if opts.TargetSchema > 8 || opts.UpdateClassification == UpdateSchemaForward || opts.RollbackClassification == RollbackBackupRestoreRequired {
			mVer = ManifestVersion2
		} else {
			mVer = ManifestVersion1
		}
	}

	updateClass := opts.UpdateClassification
	if updateClass == "" && mVer != ManifestVersion3 {
		if opts.TargetSchema > 8 || mVer == ManifestVersion2 {
			updateClass = UpdateSchemaForward
		} else {
			updateClass = ""
		}
	}

	class := opts.RollbackClassification
	if class == "" && mVer != ManifestVersion3 {
		if updateClass == UpdateSchemaForward {
			class = RollbackBackupRestoreRequired
		} else {
			class = RollbackSchemaNeutral
		}
	}

	supportedSources := opts.SupportedSourceSchemas
	if len(supportedSources) == 0 && updateClass == UpdateSchemaForward && mVer == ManifestVersion2 {
		if opts.TargetSchema == 9 {
			supportedSources = []int{8}
		} else if opts.TargetSchema == 10 {
			supportedSources = []int{8, 9}
		}
	}

	upgradePaths := opts.UpgradePaths
	if len(upgradePaths) == 0 && mVer == ManifestVersion3 && opts.TargetSchema == 9 {
		upgradePaths = []UpgradePath{
			{
				SourceSchema:           8,
				TargetSchema:           9,
				UpdateClassification:   UpdateSchemaForward,
				RollbackClassification: RollbackBackupRestoreRequired,
			},
			{
				SourceSchema:           9,
				TargetSchema:           9,
				UpdateClassification:   UpdateSchemaNeutral,
				RollbackClassification: RollbackSchemaNeutral,
			},
		}
	}
	if len(upgradePaths) == 0 && mVer == ManifestVersion3 && opts.TargetSchema == 10 {
		upgradePaths = []UpgradePath{
			{
				SourceSchema:           8,
				TargetSchema:           10,
				UpdateClassification:   UpdateSchemaForward,
				RollbackClassification: RollbackBackupRestoreRequired,
			},
			{
				SourceSchema:           9,
				TargetSchema:           10,
				UpdateClassification:   UpdateSchemaForward,
				RollbackClassification: RollbackBackupRestoreRequired,
			},
			{
				SourceSchema:           10,
				TargetSchema:           10,
				UpdateClassification:   UpdateSchemaNeutral,
				RollbackClassification: RollbackSchemaNeutral,
			},
		}
	}

	backendDigest := strings.TrimSpace(opts.BackendDigest)
	if backendDigest == "" {
		return nil, errors.New("backend digest is required")
	}

	frontendDigest := strings.TrimSpace(opts.FrontendDigest)
	if frontendDigest == "" {
		return nil, errors.New("frontend digest is required")
	}

	reqEnv := make([]string, 0, len(opts.RequiredEnv))
	for _, env := range opts.RequiredEnv {
		trimmed := strings.TrimSpace(env)
		if trimmed != "" {
			reqEnv = append(reqEnv, trimmed)
		}
	}

	m := Manifest{
		ManifestVersion:        mVer,
		ReleaseVersion:         relVer,
		ReleaseTag:             relTag,
		TargetSchema:           opts.TargetSchema,
		UpdateClassification:   updateClass,
		RollbackClassification: class,
		SupportedSourceSchemas: supportedSources,
		UpgradePaths:           upgradePaths,
		MinUpgradeFrom:         minUp,
		RequiredEnv:            reqEnv,
	}

	var backendPlats map[string]PlatformSpec
	if len(opts.BackendPlatforms) > 0 {
		backendPlats = make(map[string]PlatformSpec, len(opts.BackendPlatforms))
		for k, v := range opts.BackendPlatforms {
			backendPlats[k] = PlatformSpec{Digest: strings.TrimSpace(v)}
		}
	}

	var frontendPlats map[string]PlatformSpec
	if len(opts.FrontendPlatforms) > 0 {
		frontendPlats = make(map[string]PlatformSpec, len(opts.FrontendPlatforms))
		for k, v := range opts.FrontendPlatforms {
			frontendPlats[k] = PlatformSpec{Digest: strings.TrimSpace(v)}
		}
	}

	m.Images.Backend = ImageSpec{
		Repository: ExpectedBackendRepo,
		Tag:        relVer,
		Digest:     backendDigest,
		Platforms:  backendPlats,
	}

	m.Images.Frontend = ImageSpec{
		Repository: ExpectedFrontendRepo,
		Tag:        relVer,
		Digest:     frontendDigest,
		Platforms:  frontendPlats,
	}

	// Validate using the strict manifest validator
	if err := m.Validate(relTag); err != nil {
		return nil, fmt.Errorf("generated manifest failed validation: %w", err)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed marshaling manifest JSON: %w", err)
	}

	// Append trailing newline for POSIX file convention
	data = append(data, '\n')
	return data, nil
}
