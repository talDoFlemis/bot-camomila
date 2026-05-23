package quiethours_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/taldoflemis/bot-camomila/internal/quiethours"
)

func makeTime(t *testing.T, loc *time.Location, hour, min int) time.Time {
	t.Helper()
	return time.Date(2026, time.May, 23, hour, min, 0, 0, loc)
}

func TestIsActive_NilLocation(t *testing.T) {
	now := time.Now()
	assert.False(t, quiethours.IsActive(now, nil, "22:00", "08:00"),
		"nil location must always return false")
}

func TestIsActive_EmptyTimes(t *testing.T) {
	loc := time.UTC
	now := makeTime(t, loc, 23, 0)

	assert.False(t, quiethours.IsActive(now, loc, "", "08:00"),
		"empty start must return false")
	assert.False(t, quiethours.IsActive(now, loc, "22:00", ""),
		"empty end must return false")
	assert.False(t, quiethours.IsActive(now, loc, "", ""),
		"both empty must return false")
}

func TestIsActive_DuringQuietHours(t *testing.T) {
	loc := time.UTC
	now := makeTime(t, loc, 23, 0) // 23:00 inside 22:00-08:00

	assert.True(t, quiethours.IsActive(now, loc, "22:00", "08:00"),
		"23:00 should be inside 22:00-08:00 window")
}

func TestIsActive_OutsideQuietHours(t *testing.T) {
	loc := time.UTC
	now := makeTime(t, loc, 12, 0) // 12:00 outside 22:00-08:00

	assert.False(t, quiethours.IsActive(now, loc, "22:00", "08:00"),
		"12:00 should be outside 22:00-08:00 window")
}

func TestIsActive_MidnightWrapAround(t *testing.T) {
	loc := time.UTC
	now := makeTime(t, loc, 3, 0) // 03:00 inside 22:00-08:00

	assert.True(t, quiethours.IsActive(now, loc, "22:00", "08:00"),
		"03:00 should be inside 22:00-08:00 wrap-around window")
}

func TestIsActive_NormalWindow(t *testing.T) {
	loc := time.UTC

	inside := makeTime(t, loc, 10, 0) // 10:00 inside 08:00-12:00
	assert.True(t, quiethours.IsActive(inside, loc, "08:00", "12:00"),
		"10:00 should be inside 08:00-12:00 window")

	outside := makeTime(t, loc, 14, 0) // 14:00 outside 08:00-12:00
	assert.False(t, quiethours.IsActive(outside, loc, "08:00", "12:00"),
		"14:00 should be outside 08:00-12:00 window")
}

func TestIsActive_ExactBoundary(t *testing.T) {
	loc := time.UTC

	// Start is inclusive.
	atStart := makeTime(t, loc, 22, 0)
	assert.True(t, quiethours.IsActive(atStart, loc, "22:00", "08:00"),
		"exact start boundary should be inclusive")

	// End is exclusive.
	atEnd := makeTime(t, loc, 8, 0)
	assert.False(t, quiethours.IsActive(atEnd, loc, "22:00", "08:00"),
		"exact end boundary should be exclusive")

	// Normal window boundaries.
	atNormalStart := makeTime(t, loc, 8, 0)
	assert.True(t, quiethours.IsActive(atNormalStart, loc, "08:00", "12:00"),
		"exact start boundary of normal window should be inclusive")

	atNormalEnd := makeTime(t, loc, 12, 0)
	assert.False(t, quiethours.IsActive(atNormalEnd, loc, "08:00", "12:00"),
		"exact end boundary of normal window should be exclusive")
}

func TestIsActive_DifferentTimezone(t *testing.T) {
	// The caller's now is in UTC, but quiet hours are in São Paulo (UTC-3).
	sp, err := time.LoadLocation("America/Sao_Paulo")
	assert.NoError(t, err)

	// 01:00 UTC == 22:00 São Paulo (previous day). Inside 22:00-08:00 in SP.
	now := time.Date(2026, time.May, 24, 1, 0, 0, 0, time.UTC)
	assert.True(t, quiethours.IsActive(now, sp, "22:00", "08:00"),
		"01:00 UTC (22:00 SP) should be inside 22:00-08:00 window in SP")

	// 15:00 UTC == 12:00 São Paulo. Outside 22:00-08:00 in SP.
	now2 := time.Date(2026, time.May, 24, 15, 0, 0, 0, time.UTC)
	assert.False(t, quiethours.IsActive(now2, sp, "22:00", "08:00"),
		"15:00 UTC (12:00 SP) should be outside 22:00-08:00 window in SP")
}
