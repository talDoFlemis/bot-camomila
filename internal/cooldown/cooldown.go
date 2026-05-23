// Package cooldown provides an in-memory cooldown engine that manages
// per-matcher and per-user-per-matcher cooldown windows. Both gates must
// pass for a match to fire. A background reaper periodically cleans
// expired entries.
package cooldown

import (
	"context"
	"sync"
	"time"
)

// Clock is an injectable time source. Defaults to time.Now when nil is
// passed to NewTracker.
type Clock func() time.Time

// Tracker manages per-matcher and per-user-per-matcher cooldown state.
// All methods are safe for concurrent use.
type Tracker struct {
	perMatcher sync.Map // map[string]time.Time — keyed by matcher name
	perUser    sync.Map // map[string]time.Time — keyed by "matcherName:senderJID"
	clock      Clock
}

// NewTracker returns an initialized Tracker. If clock is nil, time.Now is
// used as the default clock source.
func NewTracker(clock Clock) *Tracker {
	if clock == nil {
		clock = time.Now
	}
	return &Tracker{clock: clock}
}

// Allow checks whether the given matcher/user combination is off cooldown.
// It checks the per-matcher cooldown first (cheaper single-key lookup),
// then the per-user cooldown. Both gates must pass for Allow to return true.
//
// Allow does NOT record the fire time — the caller must call Record()
// explicitly after a successful send.
func (t *Tracker) Allow(matcherName, senderJID string, matcherCooldown, userCooldown time.Duration) bool {
	now := t.clock()

	// Check per-matcher cooldown (cheaper — single key lookup).
	if v, ok := t.perMatcher.Load(matcherName); ok {
		lastFire := v.(time.Time)
		if now.Sub(lastFire) < matcherCooldown {
			return false
		}
	}

	// Check per-user cooldown.
	userKey := matcherName + ":" + senderJID
	if v, ok := t.perUser.Load(userKey); ok {
		lastFire := v.(time.Time)
		if now.Sub(lastFire) < userCooldown {
			return false
		}
	}

	return true
}

// Record stores the current clock time as the last-fire timestamp for both
// the matcher and the user-specific key.
func (t *Tracker) Record(matcherName, senderJID string) {
	now := t.clock()
	t.perMatcher.Store(matcherName, now)
	t.perUser.Store(matcherName+":"+senderJID, now)
}

// StartReaper launches a background goroutine that periodically removes
// expired entries from both maps. An entry is considered expired when it is
// older than 1 hour. The goroutine stops when ctx is cancelled.
func (t *Tracker) StartReaper(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				t.reap()
			}
		}
	}()
}

// reap deletes entries older than 1 hour from both maps.
func (t *Tracker) reap() {
	const expiry = time.Hour
	now := t.clock()

	t.perMatcher.Range(func(key, value any) bool {
		if now.Sub(value.(time.Time)) > expiry {
			t.perMatcher.Delete(key)
		}
		return true
	})

	t.perUser.Range(func(key, value any) bool {
		if now.Sub(value.(time.Time)) > expiry {
			t.perUser.Delete(key)
		}
		return true
	})
}
