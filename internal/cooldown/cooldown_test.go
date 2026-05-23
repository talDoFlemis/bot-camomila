package cooldown

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// fakeClock returns a Clock that always reports the time stored in *now.
// Advance time by mutating *now — no time.Sleep needed.
func fakeClock(now *time.Time) Clock {
	return func() time.Time { return *now }
}

func TestAllow_NoCooldownRecorded(t *testing.T) {
	now := time.Now()
	tr := NewTracker(fakeClock(&now))

	allowed := tr.Allow("greeting", "user1@s.whatsapp.net", 5*time.Minute, 1*time.Minute)
	assert.True(t, allowed, "fresh tracker should allow any match")
}

func TestAllow_MatcherCooldownBlocks(t *testing.T) {
	now := time.Now()
	tr := NewTracker(fakeClock(&now))

	tr.Record("greeting", "user1@s.whatsapp.net")

	// Immediately after recording, matcher cooldown should block.
	allowed := tr.Allow("greeting", "user2@s.whatsapp.net", 5*time.Minute, 0)
	assert.False(t, allowed, "matcher cooldown should block within window")

	// Advance clock past matcher cooldown.
	now = now.Add(5*time.Minute + time.Second)

	allowed = tr.Allow("greeting", "user2@s.whatsapp.net", 5*time.Minute, 0)
	assert.True(t, allowed, "matcher cooldown should allow after window expires")
}

func TestAllow_UserCooldownBlocks(t *testing.T) {
	now := time.Now()
	tr := NewTracker(fakeClock(&now))

	tr.Record("greeting", "user1@s.whatsapp.net")

	// Same user should be blocked by user cooldown.
	allowed := tr.Allow("greeting", "user1@s.whatsapp.net", 0, 2*time.Minute)
	assert.False(t, allowed, "user cooldown should block same user within window")

	// Different user should NOT be blocked by per-user cooldown.
	allowed = tr.Allow("greeting", "user2@s.whatsapp.net", 0, 2*time.Minute)
	assert.True(t, allowed, "user cooldown should not block different user")
}

func TestAllow_BothMustPass(t *testing.T) {
	now := time.Now()
	tr := NewTracker(fakeClock(&now))

	tr.Record("greeting", "user1@s.whatsapp.net")

	matcherCD := 3 * time.Minute
	userCD := 5 * time.Minute

	// Advance past matcher cooldown but not user cooldown.
	now = now.Add(3*time.Minute + time.Second)

	allowed := tr.Allow("greeting", "user1@s.whatsapp.net", matcherCD, userCD)
	assert.False(t, allowed, "should block when matcher passed but user cooldown still active")

	// Reset: record again with fresh timestamps.
	now = time.Now()
	tr2 := NewTracker(fakeClock(&now))
	tr2.Record("greeting", "user1@s.whatsapp.net")

	// Advance past user cooldown but not matcher cooldown.
	now = now.Add(4 * time.Minute)

	// Matcher cooldown is 5 min, user cooldown is 3 min.
	allowed = tr2.Allow("greeting", "user1@s.whatsapp.net", 5*time.Minute, 3*time.Minute)
	assert.False(t, allowed, "should block when user passed but matcher cooldown still active")

	// Advance past both.
	now = now.Add(2 * time.Minute)

	allowed = tr2.Allow("greeting", "user1@s.whatsapp.net", 5*time.Minute, 3*time.Minute)
	assert.True(t, allowed, "should allow when both cooldowns expired")
}

func TestRecord_SetsTimestamps(t *testing.T) {
	now := time.Now()
	tr := NewTracker(fakeClock(&now))

	// Before recording — everything allowed.
	assert.True(t, tr.Allow("ping", "userA@s.whatsapp.net", 10*time.Second, 10*time.Second))

	tr.Record("ping", "userA@s.whatsapp.net")

	// Right after recording — both gates block.
	assert.False(t, tr.Allow("ping", "userA@s.whatsapp.net", 10*time.Second, 10*time.Second))

	// Advance 5 seconds — still blocked.
	now = now.Add(5 * time.Second)
	assert.False(t, tr.Allow("ping", "userA@s.whatsapp.net", 10*time.Second, 10*time.Second))

	// Advance past 10 seconds total — allowed.
	now = now.Add(6 * time.Second)
	assert.True(t, tr.Allow("ping", "userA@s.whatsapp.net", 10*time.Second, 10*time.Second))
}

func TestReaper_CleansExpiredEntries(t *testing.T) {
	now := time.Now()
	tr := NewTracker(fakeClock(&now))

	tr.Record("greeting", "user1@s.whatsapp.net")

	// Verify entry exists (blocked).
	assert.False(t, tr.Allow("greeting", "user1@s.whatsapp.net", 24*time.Hour, 24*time.Hour),
		"entry should exist and block with a long cooldown")

	// Advance clock past the 1-hour expiry threshold.
	now = now.Add(time.Hour + time.Minute)

	// Manually trigger reap (instead of relying on ticker).
	tr.reap()

	// Entries should be cleaned — Allow with long cooldowns should return true
	// because there are no recorded timestamps left.
	assert.True(t, tr.Allow("greeting", "user1@s.whatsapp.net", 24*time.Hour, 24*time.Hour),
		"reaper should have cleaned expired entries")
}

func TestReaper_StopsOnContextCancel(t *testing.T) {
	now := time.Now()
	tr := NewTracker(fakeClock(&now))

	ctx, cancel := context.WithCancel(context.Background())
	tr.StartReaper(ctx, 50*time.Millisecond)

	// Cancel immediately — reaper goroutine should exit cleanly.
	cancel()

	// Give the goroutine a moment to stop. This is the only place we wait,
	// and it is to verify the goroutine exits — not for cooldown logic.
	time.Sleep(100 * time.Millisecond)

	// No assertion needed — we just verify no panic or hang.
}
