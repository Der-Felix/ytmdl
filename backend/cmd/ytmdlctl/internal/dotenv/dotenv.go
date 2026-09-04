// Package dotenv provides safe, deterministic, read-only parsing of .env files.
package dotenv

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// ErrEnvSymlink is returned when .env is a symlink.
	ErrEnvSymlink = errors.New(".env is a symlink; refusing mutation")
	// ErrEnvNotRegular is returned when .env is not a regular file.
	ErrEnvNotRegular = errors.New(".env is not a regular file")
	// ErrVersionNotFound is returned when .env does not define active YTMDL_VERSION.
	ErrVersionNotFound = errors.New(".env does not define active YTMDL_VERSION")
	// ErrDuplicateVersionKey is returned when .env defines multiple active YTMDL_VERSION lines.
	ErrDuplicateVersionKey = errors.New(".env contains multiple active YTMDL_VERSION definitions")
	// ErrInvalidVersionValue is returned when YTMDL_VERSION is invalid or unpinned (e.g. 'latest').
	ErrInvalidVersionValue = errors.New("invalid or unpinned YTMDL_VERSION value")
)

var semverRegex = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// ParseFile loads and parses a .env file.
// If the file does not exist, it returns an empty map and nil error.
func ParseFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return make(map[string]string), nil
		}
		return nil, err
	}
	return Parse(data), nil
}

// Parse extracts key-value pairs from bytes, handling comments, whitespace, quotes, and duplicates.
func Parse(data []byte) map[string]string {
	result := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Handle export prefix if present
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		// Strip inline comments if unquoted
		if !strings.HasPrefix(val, "\"") && !strings.HasPrefix(val, "'") {
			if idx := strings.Index(val, " #"); idx != -1 {
				val = strings.TrimSpace(val[:idx])
			} else if idx := strings.Index(val, "\t#"); idx != -1 {
				val = strings.TrimSpace(val[:idx])
			}
		}

		// Strip surrounding quotes
		if len(val) >= 2 {
			if (strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"")) ||
				(strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'")) {
				val = val[1 : len(val)-1]
			}
		}

		// Deterministic overwrite for duplicate keys
		result[key] = val
	}

	return result
}

// ValidateForUpdate inspects .env for managed update preconditions:
// - must be a regular file (not a symlink)
// - must contain exactly one active YTMDL_VERSION definition
// - version must be pinned valid SemVer (e.g. not 'latest')
func ValidateForUpdate(dotEnvPath string) (string, error) {
	fi, err := os.Lstat(dotEnvPath)
	if err != nil {
		return "", err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", ErrEnvSymlink
	}
	if !fi.Mode().IsRegular() {
		return "", ErrEnvNotRegular
	}

	data, err := os.ReadFile(dotEnvPath)
	if err != nil {
		return "", err
	}

	activeCount, versionVal, _ := findActiveVersionLines(data)
	if activeCount == 0 {
		return "", ErrVersionNotFound
	}
	if activeCount > 1 {
		return "", ErrDuplicateVersionKey
	}

	cleanVal := strings.TrimPrefix(strings.TrimSpace(versionVal), "v")
	if strings.EqualFold(cleanVal, "latest") || !semverRegex.MatchString(cleanVal) {
		return "", fmt.Errorf("%w: %q (managed updates require an exact pinned SemVer)", ErrInvalidVersionValue, versionVal)
	}

	return cleanVal, nil
}

// UpdateVersion surgically modifies only the active YTMDL_VERSION line in .env,
// preserving comments, whitespace, and unrelated entries.
func UpdateVersion(dotEnvPath, newVersion string) error {
	cleanNew := strings.TrimPrefix(strings.TrimSpace(newVersion), "v")
	if cleanNew == "" || strings.EqualFold(cleanNew, "latest") || !semverRegex.MatchString(cleanNew) {
		return fmt.Errorf("%w: %q (target must be valid SemVer)", ErrInvalidVersionValue, newVersion)
	}

	fi, err := os.Lstat(dotEnvPath)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return ErrEnvSymlink
	}
	if !fi.Mode().IsRegular() {
		return ErrEnvNotRegular
	}

	raw, err := os.ReadFile(dotEnvPath)
	if err != nil {
		return err
	}

	activeCount, _, targetLineIndex := findActiveVersionLines(raw)
	if activeCount == 0 {
		return ErrVersionNotFound
	}
	if activeCount > 1 {
		return ErrDuplicateVersionKey
	}

	lines := splitLinesPreservingNewline(raw)
	oldLine := lines[targetLineIndex]
	var newLine string
	if strings.HasPrefix(strings.TrimSpace(oldLine), "export ") {
		newLine = "export YTMDL_VERSION=" + cleanNew
	} else {
		newLine = "YTMDL_VERSION=" + cleanNew
	}
	if strings.HasSuffix(oldLine, "\r\n") {
		newLine += "\r\n"
	} else if strings.HasSuffix(oldLine, "\n") {
		newLine += "\n"
	}
	lines[targetLineIndex] = newLine

	var out bytes.Buffer
	for _, l := range lines {
		out.WriteString(l)
	}

	dir := filepath.Dir(dotEnvPath)
	tmpPath := filepath.Join(dir, fmt.Sprintf(".env.tmp.%d", os.Getpid()))

	tmpFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return fmt.Errorf("failed creating temp .env: %w", err)
	}

	writeOk := false
	defer func() {
		if !writeOk {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(out.Bytes()); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed writing temp .env: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed syncing temp .env: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed closing temp .env: %w", err)
	}

	if err := os.Rename(tmpPath, dotEnvPath); err != nil {
		return fmt.Errorf("failed updating %s: %w", dotEnvPath, err)
	}
	writeOk = true

	// Sync parent directory where supported
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		_ = dirFile.Close()
	}

	return nil
}

func findActiveVersionLines(data []byte) (activeCount int, versionVal string, lastLineIndex int) {
	lines := splitLinesPreservingNewline(data)
	for i, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		candidate := trimmed
		if strings.HasPrefix(candidate, "export ") {
			candidate = strings.TrimSpace(strings.TrimPrefix(candidate, "export "))
		}
		key, val, ok := strings.Cut(candidate, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "YTMDL_VERSION" {
			activeCount++
			versionVal = strings.TrimSpace(val)
			if !strings.HasPrefix(versionVal, "\"") && !strings.HasPrefix(versionVal, "'") {
				if idx := strings.Index(versionVal, " #"); idx != -1 {
					versionVal = strings.TrimSpace(versionVal[:idx])
				} else if idx := strings.Index(versionVal, "\t#"); idx != -1 {
					versionVal = strings.TrimSpace(versionVal[:idx])
				}
			}
			if len(versionVal) >= 2 {
				if (strings.HasPrefix(versionVal, "\"") && strings.HasSuffix(versionVal, "\"")) ||
					(strings.HasPrefix(versionVal, "'") && strings.HasSuffix(versionVal, "'")) {
					versionVal = versionVal[1 : len(versionVal)-1]
				}
			}
			lastLineIndex = i
		}
	}
	return activeCount, versionVal, lastLineIndex
}

func splitLinesPreservingNewline(data []byte) []string {
	var lines []string
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			lines = append(lines, string(data[start:i+1]))
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}
