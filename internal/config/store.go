// Package config - store.go provides the atomic config snapshot store.
package config

import "sync/atomic"

// Store holds the current configuration snapshot behind an atomic pointer.
// It is safe for concurrent use. A single writer (the config watcher) calls Swap;
// many readers (event handlers) call Get and hold the returned pointer for the
// full duration of one message-handling call without calling Get again.
type Store struct {
	ptr atomic.Pointer[Snapshot]
}

// NewStore returns a Store initialised with the given snapshot.
func NewStore(initial *Snapshot) *Store {
	s := &Store{}
	s.ptr.Store(initial)
	return s
}

// Get returns the current immutable snapshot. Callers MUST hold the returned
// pointer for the full duration of one message-handling call — do NOT call Get
// repeatedly within a single call (CONFIG-03).
func (s *Store) Get() *Snapshot { return s.ptr.Load() }

// Swap atomically replaces the snapshot. Only the watcher goroutine should call this.
func (s *Store) Swap(n *Snapshot) { s.ptr.Store(n) }
