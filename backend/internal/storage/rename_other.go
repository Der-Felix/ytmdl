//go:build !linux

package storage

import (
	"errors"
	"io"
	"os"
	"syscall"
)

// renameNoReplace atomically links oldpath to newpath and unlinks oldpath on Darwin/other POSIX systems.
// If newpath already exists, link() returns os.ErrExist (EEXIST) atomically.
// If hard links are unsupported, it falls back to atomic exclusive creation (O_EXCL).
func renameNoReplace(oldpath, newpath string) error {
	linkErr := os.Link(oldpath, newpath)
	if linkErr == nil {
		_ = os.Remove(oldpath)
		return nil
	}
	if errors.Is(linkErr, os.ErrExist) {
		return os.ErrExist
	}
	if isCrossDevice(linkErr) || errors.Is(linkErr, syscall.EPERM) || errors.Is(linkErr, syscall.EOPNOTSUPP) || errors.Is(linkErr, syscall.ENOSYS) {
		return copyExclusiveNoReplace(oldpath, newpath)
	}
	return linkErr
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
