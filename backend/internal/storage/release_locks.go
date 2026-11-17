package storage

import (
	"path/filepath"
	"sort"
	"sync"
)

// releaseDirLocks coordinates writers of a release directory. A download that
// places a track holds its release directory shared, so several tracks of one
// album still finalize in parallel. Moving files between release directories
// (the naming migration) holds every directory involved exclusively, so it
// never races a download that writes into the same folder.
type releaseDirLocks struct {
	mu    sync.Mutex
	locks map[string]*releaseDirLock
}

type releaseDirLock struct {
	rw   sync.RWMutex
	refs int
}

func (d *releaseDirLocks) acquire(key string) *releaseDirLock {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.locks == nil {
		d.locks = make(map[string]*releaseDirLock)
	}
	l, ok := d.locks[key]
	if !ok {
		l = &releaseDirLock{}
		d.locks[key] = l
	}
	l.refs++
	return l
}

func (d *releaseDirLocks) release(key string, l *releaseDirLock) {
	d.mu.Lock()
	defer d.mu.Unlock()
	l.refs--
	if l.refs == 0 {
		delete(d.locks, key)
	}
}

// ShareReleaseDir holds a library relative release directory shared until the
// returned function is called.
func (l *Library) ShareReleaseDir(rel string) func() {
	key := filepath.Clean(rel)
	lock := l.dirLocks.acquire(key)
	lock.rw.RLock()
	return func() {
		lock.rw.RUnlock()
		l.dirLocks.release(key, lock)
	}
}

// LockReleaseDirs holds every given library relative directory exclusively
// until the returned function is called. The directories are locked in a
// fixed order, so two callers can never deadlock on each other.
func (l *Library) LockReleaseDirs(rels ...string) func() {
	seen := make(map[string]struct{}, len(rels))
	keys := make([]string, 0, len(rels))
	for _, rel := range rels {
		key := filepath.Clean(rel)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	held := make([]*releaseDirLock, 0, len(keys))
	for _, key := range keys {
		lock := l.dirLocks.acquire(key)
		lock.rw.Lock()
		held = append(held, lock)
	}
	return func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i].rw.Unlock()
			l.dirLocks.release(keys[i], held[i])
		}
	}
}
