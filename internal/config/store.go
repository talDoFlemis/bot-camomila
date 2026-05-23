package config

import "sync/atomic"

// Store holds the active config Snapshot under an atomic pointer.
// The zero value is not usable — create via NewStore.
type Store struct {
	ptr atomic.Pointer[Snapshot]
}

// NewStore creates a Store initialised with the given Snapshot.
func NewStore(initial *Snapshot) *Store {
	s := &Store{}
	s.ptr.Store(initial)
	return s
}

// Get returns the current snapshot. Callers MUST hold the returned pointer for
// the full duration of one message-handling call and must not call Get
// repeatedly within one call.
func (s *Store) Get() *Snapshot {
	return s.ptr.Load()
}

// Swap atomically replaces the snapshot. Only the watcher goroutine calls this.
func (s *Store) Swap(n *Snapshot) {
	s.ptr.Store(n)
}
