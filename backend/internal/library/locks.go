package library

import (
	"sync"
)

// KeyedMutex coordinates access to items by key (such as track ID or file path)
// with automatic cleanup of idle locks to avoid memory leaks.
type KeyedMutex struct {
	mu    sync.Mutex
	locks map[string]*refLock
}

type refLock struct {
	mu       sync.Mutex
	refCount int
}

// NewKeyedMutex constructs a KeyedMutex.
func NewKeyedMutex() *KeyedMutex {
	return &KeyedMutex{locks: make(map[string]*refLock)}
}

// Lock acquires the lock for key and returns an unlock function.
// When the lock is released and no other goroutines are waiting, the key is
// pruned from the internal map.
func (km *KeyedMutex) Lock(key string) func() {
	km.mu.Lock()
	l, ok := km.locks[key]
	if !ok {
		l = &refLock{}
		km.locks[key] = l
	}
	l.refCount++
	km.mu.Unlock()

	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		km.mu.Lock()
		l.refCount--
		if l.refCount == 0 {
			delete(km.locks, key)
		}
		km.mu.Unlock()
	}
}

// TryLock attempts to acquire the lock for key without blocking.
// It returns an unlock function and true if acquired, or nil and false if busy.
func (km *KeyedMutex) TryLock(key string) (func(), bool) {
	km.mu.Lock()
	l, exists := km.locks[key]
	if !exists {
		l = &refLock{}
		km.locks[key] = l
	}
	if !l.mu.TryLock() {
		if !exists {
			delete(km.locks, key)
		}
		km.mu.Unlock()
		return nil, false
	}
	l.refCount++
	km.mu.Unlock()

	return func() {
		l.mu.Unlock()
		km.mu.Lock()
		l.refCount--
		if l.refCount == 0 {
			delete(km.locks, key)
		}
		km.mu.Unlock()
	}, true
}

// Len returns the number of active/pending keys in the lock map.
func (km *KeyedMutex) Len() int {
	km.mu.Lock()
	defer km.mu.Unlock()
	return len(km.locks)
}
