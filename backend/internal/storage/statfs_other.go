//go:build !linux && !darwin

package storage

// queryFS returns filesystem type and disk space metrics for path on other platforms.
func queryFS(path string) (fsType string, total uint64, free uint64, avail uint64, err error) {
	return "generic", 0, 0, 0, nil
}

// QueryFS returns filesystem type and disk space metrics for path.
func QueryFS(path string) (fsType string, total uint64, free uint64, avail uint64, err error) {
	return queryFS(path)
}
