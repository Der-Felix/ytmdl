//go:build linux

package storage

import (
	"errors"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// renameNoReplace atomically moves oldpath to newpath without replacing newpath if it exists.
// On Linux, it attempts the kernel syscall renameat2 with RENAME_NOREPLACE (3.15+).
// If the filesystem or kernel returns ENOSYS, EINVAL, EOPNOTSUPP or ENOTSUP (e.g. some CIFS/NFS mounts),
// it falls back to hardlink (os.Link) or atomic exclusive open (O_EXCL), guaranteeing no silent overwrite.
func renameNoReplace(oldpath, newpath string) error {
	err := unix.Renameat2(unix.AT_FDCWD, oldpath, unix.AT_FDCWD, newpath, unix.RENAME_NOREPLACE)
	if err == nil {
		return nil
	}
	if errors.Is(err, unix.EEXIST) {
		return os.ErrExist
	}
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.ENOTSUP) {
		// Fallback: try hardlink on same filesystem
		linkErr := os.Link(oldpath, newpath)
		if linkErr == nil {
			_ = os.Remove(oldpath)
			return nil
		}
		if errors.Is(linkErr, os.ErrExist) {
			return os.ErrExist
		}
		// If hardlinks are unsupported on this filesystem (e.g. CIFS/SMB without unix extensions),
		// fall back to exclusive file copy with O_EXCL.
		if isCrossDevice(linkErr) || errors.Is(linkErr, syscall.EPERM) || errors.Is(linkErr, syscall.EOPNOTSUPP) || errors.Is(linkErr, syscall.ENOSYS) {
			return copyExclusiveNoReplace(oldpath, newpath)
		}
		return linkErr
	}
	return err
}

func copyExclusiveNoReplace(oldpath, newpath string) error {
	out, err := os.OpenFile(newpath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return os.ErrExist
		}
		return err
	}
	in, err := os.Open(oldpath)
	if err != nil {
		out.Close()
		_ = os.Remove(newpath)
		return err
	}
	defer in.Close()

	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(newpath)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		_ = os.Remove(newpath)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(newpath)
		return err
	}
	_ = os.Remove(oldpath)
	return nil
}
