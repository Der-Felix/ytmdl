//go:build darwin

package storage

import (
	"bytes"
	"syscall"
)

// queryFS returns filesystem type and disk space metrics for path on Darwin.
func queryFS(path string) (fsType string, total uint64, free uint64, avail uint64, err error) {
	var buf syscall.Statfs_t
	if err := syscall.Statfs(path, &buf); err != nil {
		return "", 0, 0, 0, err
	}

	bsize := uint64(buf.Bsize)
	total = buf.Blocks * bsize
	free = buf.Bfree * bsize
	avail = buf.Bavail * bsize

	// Convert Fstypename [16]int8 to string
	var nameBytes []byte
	for _, c := range buf.Fstypename {
		if c == 0 {
			break
		}
		nameBytes = append(nameBytes, byte(c))
	}
	fsType = string(bytes.TrimSpace(nameBytes))
	if fsType == "" {
		fsType = "darwin-fs"
	}

	return fsType, total, free, avail, nil
}

// QueryFS returns filesystem type and disk space metrics for path on Darwin.
func QueryFS(path string) (fsType string, total uint64, free uint64, avail uint64, err error) {
	return queryFS(path)
}
