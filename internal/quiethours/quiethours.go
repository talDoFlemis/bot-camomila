// Package quiethours provides a gate that determines whether the bot
// should stay silent during a configured time window.
package quiethours

import "time"

// IsActive reports whether now falls inside the quiet-hours window
// defined by start and end (both in "15:04" format) in the given location.
//
// Returns false when loc is nil or either start/end is empty, meaning
// quiet hours are not configured. The window is start-inclusive and
// end-exclusive. Midnight wrap-around (e.g. "22:00"-"08:00") is handled
// automatically: the window is interpreted as spanning midnight.
//
// The caller must supply a pre-resolved *time.Location; this function
// never falls back to time.Local.
func IsActive(now time.Time, loc *time.Location, start, end string) bool {
	if loc == nil {
		return false
	}
	if start == "" || end == "" {
		return false
	}

	const layout = "15:04"

	startTime, err := time.Parse(layout, start)
	if err != nil {
		return false
	}
	endTime, err := time.Parse(layout, end)
	if err != nil {
		return false
	}

	// Convert now to the target location.
	now = now.In(loc)

	// Build today's start and end in the target timezone.
	y, m, d := now.Date()
	startToday := time.Date(y, m, d, startTime.Hour(), startTime.Minute(), 0, 0, loc)
	endToday := time.Date(y, m, d, endTime.Hour(), endTime.Minute(), 0, 0, loc)

	if startToday.Before(endToday) {
		// Normal window (e.g. 08:00–12:00): start <= now < end.
		return !now.Before(startToday) && now.Before(endToday)
	}

	// Wrap-around window (e.g. 22:00–08:00): now >= start || now < end.
	return !now.Before(startToday) || now.Before(endToday)
}
