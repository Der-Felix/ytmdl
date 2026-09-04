// Package compose handles deterministic compose file discovery and ambiguity resolution.
package compose

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrFileNotFound indicates the requested compose file does not exist.
	ErrFileNotFound = errors.New("compose file not found")
	// ErrPathEscape indicates the compose file path attempts to escape the project directory.
	ErrPathEscape = errors.New("compose file must reside within the project directory")
	// ErrNoComposeFound indicates no supported compose files were found in the project.
	ErrNoComposeFound = errors.New("no supported compose file found in project directory")
	// ErrAmbiguousCompose indicates multiple compose files exist and require explicit selection for mutation.
	ErrAmbiguousCompose = errors.New("multiple compose files found; explicit --file is required for mutation")
)

// SupportedCandidates lists candidate compose filenames in standard priority order.
var SupportedCandidates = []string{
	"compose.ghcr.yaml",
	"compose.registry.yaml",
	"compose.yaml",
	"docker-compose.yaml",
	"docker-compose.yml",
}

// ResolveOptions controls the compose resolution process.
type ResolveOptions struct {
	ProjectDir    string
	ExplicitFile  string
	PersistedFile string
	IsMutating    bool
}

// ResolutionResult returns the outcome of compose file resolution.
type ResolutionResult struct {
	SelectedFile string   // filename relative to ProjectDir
	FullPath     string   // absolute path to file
	Candidates   []string // all detected candidate filenames
	IsAmbiguous  bool     // true if multiple candidates exist without explicit/persisted selection
}

// Resolve identifies the target compose file following the hardened v0.16 rules.
func Resolve(opts ResolveOptions) (*ResolutionResult, error) {
	projDir, err := filepath.Abs(opts.ProjectDir)
	if err != nil {
		return nil, fmt.Errorf("compose: failed to resolve project directory: %w", err)
	}

	// 1. Explicit file passed via CLI flag
	if opts.ExplicitFile != "" {
		relPath, fullPath, err := validateAndClean(projDir, opts.ExplicitFile)
		if err != nil {
			return nil, err
		}
		return &ResolutionResult{
			SelectedFile: relPath,
			FullPath:     fullPath,
			Candidates:   []string{relPath},
			IsAmbiguous:  false,
		}, nil
	}

	// 2. Persisted file in .ytmdl/config.json
	if opts.PersistedFile != "" {
		relPath, fullPath, err := validateAndClean(projDir, opts.PersistedFile)
		if err != nil {
			return nil, err
		}
		return &ResolutionResult{
			SelectedFile: relPath,
			FullPath:     fullPath,
			Candidates:   []string{relPath},
			IsAmbiguous:  false,
		}, nil
	}

	// 3. Scan directory for candidate files
	candidates := make([]string, 0, len(SupportedCandidates))
	for _, candidate := range SupportedCandidates {
		p := filepath.Join(projDir, candidate)
		fi, err := os.Stat(p)
		if err == nil && !fi.IsDir() {
			candidates = append(candidates, candidate)
		}
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w in %s", ErrNoComposeFound, projDir)
	}

	if len(candidates) == 1 {
		selected := candidates[0]
		return &ResolutionResult{
			SelectedFile: selected,
			FullPath:     filepath.Join(projDir, selected),
			Candidates:   candidates,
			IsAmbiguous:  false,
		}, nil
	}

	// Multiple candidate files detected
	if opts.IsMutating {
		return nil, fmt.Errorf("%w: %s", ErrAmbiguousCompose, strings.Join(candidates, ", "))
	}

	// Read-only inspection allowed: report candidates and ambiguity
	return &ResolutionResult{
		SelectedFile: candidates[0], // advisory primary
		FullPath:     filepath.Join(projDir, candidates[0]),
		Candidates:   candidates,
		IsAmbiguous:  true,
	}, nil
}

// validateAndClean ensures the target file is strictly inside projDir and exists.
func validateAndClean(projDir, target string) (string, string, error) {
	var fullPath string
	if filepath.IsAbs(target) {
		fullPath = filepath.Clean(target)
	} else {
		fullPath = filepath.Clean(filepath.Join(projDir, target))
	}

	// Check path traversal
	rel, err := filepath.Rel(projDir, fullPath)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		return "", "", fmt.Errorf("%w: %q (project dir: %q)", ErrPathEscape, target, projDir)
	}

	fi, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("%w: %q", ErrFileNotFound, fullPath)
		}
		return "", "", fmt.Errorf("compose: cannot stat %q: %w", fullPath, err)
	}

	if fi.IsDir() {
		return "", "", fmt.Errorf("compose: %q is a directory, not a compose file", fullPath)
	}

	// Verify symlinks do not escape the project directory
	realProjDir, err := filepath.EvalSymlinks(projDir)
	if err != nil {
		realProjDir = projDir
	}
	realFullPath, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return "", "", fmt.Errorf("compose: cannot resolve %q: %w", fullPath, err)
	}
	realRel, err := filepath.Rel(realProjDir, realFullPath)
	if err != nil || strings.HasPrefix(realRel, "..") {
		return "", "", fmt.Errorf("%w: symlink %q points outside project directory (%s)", ErrPathEscape, target, realFullPath)
	}

	return rel, fullPath, nil
}
