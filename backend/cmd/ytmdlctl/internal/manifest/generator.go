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
	ReleaseVersion         string
	ReleaseTag             string
	TargetSchema           int
	RollbackClassification RollbackClassification
	MinUpgradeFrom         string
	BackendDigest          string
	FrontendDigest         string
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

	class := opts.RollbackClassification
	if class == "" {
		class = RollbackSchemaNeutral
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
		ManifestVersion:        CurrentManifestVersion,
		ReleaseVersion:         relVer,
		ReleaseTag:             relTag,
		TargetSchema:           opts.TargetSchema,
		RollbackClassification: class,
		MinUpgradeFrom:         minUp,
		RequiredEnv:            reqEnv,
	}

	m.Images.Backend = ImageSpec{
		Repository: ExpectedBackendRepo,
		Tag:        relVer,
		Digest:     backendDigest,
	}

	m.Images.Frontend = ImageSpec{
		Repository: ExpectedFrontendRepo,
		Tag:        relVer,
		Digest:     frontendDigest,
	}

	// Validate using the strict Stage 2 manifest validator
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
