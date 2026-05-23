// Package killswitch provides an atomic toggle that lets the bot owner
// pause and resume the bot at runtime without reloading configuration.
//
// The Switch is safe for concurrent use from multiple goroutines.
package killswitch

import "sync/atomic"

// Switch is a thread-safe pause/resume gate backed by an atomic.Bool.
// A zero-value Switch is unpaused (active == false).
type Switch struct {
	paused atomic.Bool
}

// New returns a Switch that starts in the unpaused state.
func New() *Switch {
	return &Switch{}
}

// Pause sets the switch so that IsPaused returns true.
func (s *Switch) Pause() {
	s.paused.Store(true)
}

// Resume clears the switch so that IsPaused returns false.
func (s *Switch) Resume() {
	s.paused.Store(false)
}

// IsPaused reports whether the switch is currently paused.
func (s *Switch) IsPaused() bool {
	return s.paused.Load()
}
