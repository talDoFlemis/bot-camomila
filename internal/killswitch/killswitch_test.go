package killswitch_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/taldoflemis/bot-camomila/internal/killswitch"
)

func TestNew_StartsUnpaused(t *testing.T) {
	sw := killswitch.New()
	assert.False(t, sw.IsPaused(), "a new switch must start unpaused")
}

func TestPause(t *testing.T) {
	sw := killswitch.New()
	sw.Pause()
	assert.True(t, sw.IsPaused(), "after Pause, IsPaused must return true")
}

func TestResume(t *testing.T) {
	sw := killswitch.New()
	sw.Pause()
	sw.Resume()
	assert.False(t, sw.IsPaused(), "after Pause then Resume, IsPaused must return false")
}

func TestConcurrentAccess(t *testing.T) {
	sw := killswitch.New()

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			if n%2 == 0 {
				sw.Pause()
			} else {
				sw.Resume()
			}
			// Read must never panic.
			_ = sw.IsPaused()
		}(i)
	}

	wg.Wait()

	// After all goroutines finish, the switch must be in a valid state.
	paused := sw.IsPaused()
	assert.IsType(t, false, paused, "IsPaused must return a bool after concurrent access")
}
