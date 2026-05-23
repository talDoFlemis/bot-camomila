// Package pipeline orchestrates the full message-handling gate chain.
// It composes kill switch → quiet hours → match → cooldown → rate cap → pick answer.
package pipeline

import (
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/taldoflemis/bot-camomila/internal/config"
	"github.com/taldoflemis/bot-camomila/internal/cooldown"
	"github.com/taldoflemis/bot-camomila/internal/killswitch"
	"github.com/taldoflemis/bot-camomila/internal/matcher"
	"github.com/taldoflemis/bot-camomila/internal/quiethours"
)

// Decision is the result of processing a message through the pipeline.
type Decision struct {
	Reply       bool   // true if a reply should be sent
	Answer      string // the final answer text (with variables substituted)
	MatcherName string // which matcher fired (empty if no match)
	MatchedWord string // the input token that matched (empty if no match)
	DropReason  string // why the message was dropped (empty if Reply == true)
}

// Pipeline composes the full gate chain for message handling.
type Pipeline struct {
	killSwitch  *killswitch.Switch
	cooldowns   *cooldown.Tracker
	rateLimiter *RateLimiter
	rng         *rand.Rand
	clock       func() time.Time
}

// New creates a Pipeline with the given gate components.
// If clock is nil, time.Now is used.
func New(ks *killswitch.Switch, cd *cooldown.Tracker, rl *RateLimiter, clock func() time.Time) *Pipeline {
	if clock == nil {
		clock = time.Now
	}
	return &Pipeline{
		killSwitch:  ks,
		cooldowns:   cd,
		rateLimiter: rl,
		rng:         rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(time.Now().UnixNano()>>1))),
		clock:       clock,
	}
}

// Handle runs the full gate chain on a message and returns a Decision.
// matchers is the resolved matcher list for the listener that received this message.
// The gate order is: kill switch → quiet hours → match → cooldown → rate cap → pick answer.
func (p *Pipeline) Handle(msg Message, snap *config.Snapshot, matchers []config.ResolvedMatcher) Decision {
	now := p.clock()

	// Gate 1 — Kill switch.
	if p.killSwitch.IsPaused() {
		return Decision{DropReason: "kill_switch"}
	}

	// Gate 2 — Quiet hours.
	if quiethours.IsActive(now, snap.Location, snap.Limits.QuietHours.Start, snap.Limits.QuietHours.End) {
		return Decision{DropReason: "quiet_hours"}
	}

	// Gate 3 — Match (body first, then quoted text if no body match).
	// mentionedBot only applies to the body — mentions live in the message's own ContextInfo.
	normalizedBody := matcher.Normalize(msg.Text)
	result := matcher.Match(normalizedBody, msg.MentionedBot, matchers)

	if result == nil && msg.QuotedBody != "" && msg.QuotedSenderJID != "" {
		// Quoted text is eligible for matching. QuotedSenderJID == "" means the
		// quoted author is the bot itself (quote-chain loop prevention).
		normalizedQuoted := matcher.Normalize(msg.QuotedBody)
		result = matcher.Match(normalizedQuoted, false, matchers)
	}

	if result == nil {
		return Decision{DropReason: "no_match"}
	}

	// Look up the matched ResolvedMatcher to get its CooldownDuration.
	var matcherCooldown time.Duration
	for i := range matchers {
		if matchers[i].Name == result.MatcherName {
			matcherCooldown = matchers[i].CooldownDuration
			break
		}
	}

	// Gate 4 — Cooldown (per-matcher + per-user).
	if !p.cooldowns.Allow(result.MatcherName, msg.SenderJID, matcherCooldown, snap.UserCooldownDuration) {
		return Decision{DropReason: "cooldown"}
	}

	// Gate 5 — Rate cap.
	if !p.rateLimiter.Allow(snap.Limits.RateCap.PerMin, snap.Limits.RateCap.PerHour) {
		return Decision{DropReason: "rate_cap"}
	}

	// All gates passed — pick a random answer and substitute variables.
	matchedMatcher := findMatcher(matchers, result.MatcherName)
	answer := matchedMatcher.Answers[p.rng.IntN(len(matchedMatcher.Answers))]
	answer = substituteVars(answer, result.MatchedWord, msg.SenderPushName)

	// Record fire in cooldown and rate limiter.
	p.cooldowns.Record(result.MatcherName, msg.SenderJID)
	p.rateLimiter.Record()

	return Decision{
		Reply:       true,
		Answer:      answer,
		MatcherName: result.MatcherName,
		MatchedWord: result.MatchedWord,
	}
}

// findMatcher looks up a ResolvedMatcher by name.
func findMatcher(matchers []config.ResolvedMatcher, name string) *config.ResolvedMatcher {
	for i := range matchers {
		if matchers[i].Name == name {
			return &matchers[i]
		}
	}
	return nil
}

// substituteVars replaces {MATCHED_WORD} and {REPLIED_USER} in the answer template.
func substituteVars(answer, matchedWord, pushName string) string {
	answer = strings.ReplaceAll(answer, "{MATCHED_WORD}", matchedWord)
	answer = strings.ReplaceAll(answer, "{REPLIED_USER}", pushName)
	return answer
}

// Message is the pipeline's view of an inbound message. This mirrors domain.Message
// but is defined here to avoid a circular import. The adapter constructs this from
// domain.Message before calling Handle.
type Message struct {
	ID              string
	GroupJID        string
	SenderJID       string
	SenderPushName  string
	Text            string
	QuotedBody      string
	QuotedSenderJID string
	Timestamp       time.Time
	MentionedBot    bool // true when the bot's JID appears in ContextInfo.MentionedJID
}

// RateLimiter enforces per-minute and per-hour send rate caps using sliding windows.
type RateLimiter struct {
	mu        sync.Mutex
	minuteLog []time.Time
	hourLog   []time.Time
	clock     func() time.Time
}

// NewRateLimiter creates a RateLimiter. If clock is nil, time.Now is used.
func NewRateLimiter(clock func() time.Time) *RateLimiter {
	if clock == nil {
		clock = time.Now
	}
	return &RateLimiter{clock: clock}
}

// Allow checks whether a send is permitted under the current rate caps.
// It does NOT record the send — call Record() separately after a successful send.
func (r *RateLimiter) Allow(perMin, perHour int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.clock()

	// Prune expired entries.
	r.minuteLog = pruneOlderThan(r.minuteLog, now, time.Minute)
	r.hourLog = pruneOlderThan(r.hourLog, now, time.Hour)

	if perMin > 0 && len(r.minuteLog) >= perMin {
		return false
	}
	if perHour > 0 && len(r.hourLog) >= perHour {
		return false
	}
	return true
}

// Record records a send event in both the minute and hour windows.
func (r *RateLimiter) Record() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.clock()
	r.minuteLog = append(r.minuteLog, now)
	r.hourLog = append(r.hourLog, now)
}

// pruneOlderThan removes entries older than `window` from the front of a sorted slice.
func pruneOlderThan(log []time.Time, now time.Time, window time.Duration) []time.Time {
	cutoff := now.Add(-window)
	i := 0
	for i < len(log) && log[i].Before(cutoff) {
		i++
	}
	return log[i:]
}
