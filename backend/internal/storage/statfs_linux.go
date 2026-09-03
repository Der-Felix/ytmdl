//go:build linux

package storage

import (
	"fmt"
	"syscall"
)

// queryFS returns filesystem type and disk space metrics for path on Linux.
func queryFS(path string) (fsType string, total uint64, free uint64, avail uint64, err error) {
	var buf syscall.Statfs_t
	if err := syscall.Statfs(path, &buf); err != nil {
		return "", 0, 0, 0, err
	}

	bsize := uint64(buf.Bsize)
	total = buf.Blocks * bsize
	free = buf.Bfree * bsize
	avail = buf.Bavail * bsize

	// Magic constants from linux/magic.h
	const (
		nfsMagic     = 0x6969
		cifsMagic    = 0xff534d42
		smb2Magic    = 0xfe534d42
		ext4Magic    = 0xef53
		btrfsMagic   = 0x9123683e
		zfsMagic     = 0x2fc12fc1
		xfsMagic     = 0x58465342
		tmpfsMagic   = 0x01021994
		overlayMagic = 0x794c7630
	)

	switch uint64(buf.Type) {
	case nfsMagic:
		fsType = "NFS"
	case cifsMagic, smb2Magic:
		fsType = "CIFS/SMB"
	case ext4Magic:
		fsType = "ext4"
	case btrfsMagic:
		fsType = "btrfs"
	case zfsMagic:
		fsType = "zfs"
	case xfsMagic:
		fsType = "xfs"
	case tmpfsMagic:
		fsType = "tmpfs"
	case overlayMagic:
		fsType = "overlayfs"
	default:
		fsType = fmt.Sprintf("linux(0x%x)", buf.Type)
	}

	return fsType, total, free, avail, nil
}

// QueryFS returns filesystem type and disk space metrics for path on Linux.
func QueryFS(path string) (fsType string, total uint64, free uint64, avail uint64, err error) {
	return queryFS(path)
}
