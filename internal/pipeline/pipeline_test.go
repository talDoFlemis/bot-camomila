package pipeline

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/taldoflemis/bot-camomila/internal/config"
	"github.com/taldoflemis/bot-camomila/internal/cooldown"
	"github.com/taldoflemis/bot-camomila/internal/killswitch"
)

// testMatchers returns the two standard test matchers.
func testMatchers() []config.ResolvedMatcher {
	return []config.ResolvedMatcher{
		{
			Name:             "tax",
			Words:            []string{"sefaz"},
			Distance:         1,
			Answers:          []string{"calma, vai dar certo!"},
			CooldownDuration: 5 * time.Minute,
		},
		{
			Name:             "traffic",
			Words:            []string{"detran"},
			Distance:         0,
			Answers:          []string{"respira fundo, {REPLIED_USER}"},
			CooldownDuration: 5 * time.Minute,
		},
	}
}

// testSnap returns a config.Snapshot with sensible defaults.
func testSnap() *config.Snapshot {
	return &config.Snapshot{
		Limits: config.LimitsConfig{
			QuietHours: config.QuietHoursConfig{
				Start:    "22:00",
				End:      "08:00",
				Timezone: "America/Sao_Paulo",
			},
			RateCap: config.RateCapConfig{
				PerMin:  3,
				PerHour: 20,
			},
		},
		Location:             mustLoadLocation("America/Sao_Paulo"),
		UserCooldownDuration: 15 * time.Minute,
	}
}

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// newTestPipeline creates a Pipeline with fake clock for testing.
func newTestPipeline(now *time.Time) (*Pipeline, *killswitch.Switch) {
	ks := killswitch.New()
	fakeClock := func() time.Time { return *now }
	cd := cooldown.NewTracker(fakeClock)
	rl := NewRateLimiter(fakeClock)
	pipe := New(ks, cd, rl, fakeClock)
	return pipe, ks
}

func TestHandle_KillSwitchDrops(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, ks := newTestPipeline(&now)
	ks.Pause()

	msg := Message{Text: "sefaz", SenderJID: "user@s.whatsapp.net"}
	snap := testSnap()
	snap.Location = nil // disable quiet hours for this test

	d := pipe.Handle(msg, snap, testMatchers())
	assert.False(t, d.Reply)
	assert.Equal(t, "kill_switch", d.DropReason)
}

func TestHandle_QuietHoursDrops(t *testing.T) {
	// 23:00 São Paulo time is within 22:00-08:00 quiet window.
	loc := mustLoadLocation("America/Sao_Paulo")
	now := time.Date(2026, 5, 23, 23, 0, 0, 0, loc)
	pipe, _ := newTestPipeline(&now)

	msg := Message{Text: "sefaz", SenderJID: "user@s.whatsapp.net"}
	snap := testSnap()

	d := pipe.Handle(msg, snap, testMatchers())
	assert.False(t, d.Reply)
	assert.Equal(t, "quiet_hours", d.DropReason)
}

func TestHandle_NoMatchDrops(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, _ := newTestPipeline(&now)

	msg := Message{Text: "hello world", SenderJID: "user@s.whatsapp.net"}
	snap := testSnap()
	snap.Location = nil

	d := pipe.Handle(msg, snap, testMatchers())
	assert.False(t, d.Reply)
	assert.Equal(t, "no_match", d.DropReason)
}

func TestHandle_MatchFires(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, _ := newTestPipeline(&now)

	msg := Message{Text: "sefaz", SenderJID: "user@s.whatsapp.net"}
	snap := testSnap()
	snap.Location = nil

	d := pipe.Handle(msg, snap, testMatchers())
	assert.True(t, d.Reply)
	assert.Equal(t, "tax", d.MatcherName)
	assert.NotEmpty(t, d.Answer)
}

func TestHandle_CooldownDrops(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, _ := newTestPipeline(&now)

	msg := Message{Text: "sefaz", SenderJID: "user@s.whatsapp.net"}
	snap := testSnap()
	snap.Location = nil

	// First call fires.
	d1 := pipe.Handle(msg, snap, testMatchers())
	assert.True(t, d1.Reply)

	// Second call (same matcher + user, same time) should be blocked by cooldown.
	d2 := pipe.Handle(msg, snap, testMatchers())
	assert.False(t, d2.Reply)
	assert.Equal(t, "cooldown", d2.DropReason)
}

func TestHandle_RateCapDrops(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, _ := newTestPipeline(&now)

	snap := testSnap()
	snap.Location = nil
	snap.Limits.RateCap.PerMin = 2
	snap.Limits.RateCap.PerHour = 100
	snap.UserCooldownDuration = 0

	matchers := testMatchers()
	matchers[0].CooldownDuration = 0 // disable per-matcher cooldown so rate cap is the only gate

	// Fire perMin times.
	for i := 0; i < 2; i++ {
		msg := Message{Text: "sefaz", SenderJID: "user" + string(rune('A'+i)) + "@s.whatsapp.net"}
		d := pipe.Handle(msg, snap, matchers)
		assert.True(t, d.Reply, "fire %d should succeed", i)
	}

	// Next one should be rate-capped.
	msg := Message{Text: "sefaz", SenderJID: "userZ@s.whatsapp.net"}
	d := pipe.Handle(msg, snap, matchers)
	assert.False(t, d.Reply)
	assert.Equal(t, "rate_cap", d.DropReason)
}

func TestHandle_QuotedTextMatch(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, _ := newTestPipeline(&now)

	msg := Message{
		Text:            "olha isso aqui",      // no trigger in body
		QuotedBody:      "sefaz mandou avisar", // trigger in quote
		QuotedSenderJID: "other@s.whatsapp.net",
		SenderJID:       "user@s.whatsapp.net",
	}
	snap := testSnap()
	snap.Location = nil

	d := pipe.Handle(msg, snap, testMatchers())
	assert.True(t, d.Reply)
	assert.Equal(t, "tax", d.MatcherName)
}

func TestHandle_QuotedSelfSkipped(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, _ := newTestPipeline(&now)

	msg := Message{
		Text:            "olha isso aqui",
		QuotedBody:      "sefaz mandou avisar",
		QuotedSenderJID: "", // empty = bot's own quote (quote-chain prevention)
		SenderJID:       "user@s.whatsapp.net",
	}
	snap := testSnap()
	snap.Location = nil

	d := pipe.Handle(msg, snap, testMatchers())
	assert.False(t, d.Reply)
	assert.Equal(t, "no_match", d.DropReason)
}

func TestHandle_VariableSubstitution(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, _ := newTestPipeline(&now)

	msg := Message{
		Text:           "detran",
		SenderJID:      "user@s.whatsapp.net",
		SenderPushName: "Maria",
	}
	snap := testSnap()
	snap.Location = nil

	d := pipe.Handle(msg, snap, testMatchers())
	assert.True(t, d.Reply)
	assert.Equal(t, "traffic", d.MatcherName)
	assert.Contains(t, d.Answer, "Maria")
}

func TestHandle_GateOrder(t *testing.T) {
	// Kill switch should be checked BEFORE quiet hours.
	loc := mustLoadLocation("America/Sao_Paulo")
	now := time.Date(2026, 5, 23, 23, 0, 0, 0, loc)
	pipe, ks := newTestPipeline(&now)
	ks.Pause() // kill switch AND quiet hours both active

	msg := Message{Text: "sefaz", SenderJID: "user@s.whatsapp.net"}
	snap := testSnap()

	d := pipe.Handle(msg, snap, testMatchers())
	// Should be "kill_switch", not "quiet_hours" — proving gate order.
	assert.Equal(t, "kill_switch", d.DropReason)
}

func TestSubstituteVars(t *testing.T) {
	tests := []struct {
		name        string
		answer      string
		matchedWord string
		pushName    string
		want        string
	}{
		{
			name:        "Both variables",
			answer:      "calma {REPLIED_USER}, {MATCHED_WORD} não é o fim",
			matchedWord: "sefaz",
			pushName:    "João",
			want:        "calma João, sefaz não é o fim",
		},
		{
			name:        "No variables",
			answer:      "tudo vai ficar bem",
			matchedWord: "sefaz",
			pushName:    "Maria",
			want:        "tudo vai ficar bem",
		},
		{
			name:        "Empty push name",
			answer:      "calma {REPLIED_USER}",
			matchedWord: "sefaz",
			pushName:    "",
			want:        "calma ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := substituteVars(tt.answer, tt.matchedWord, tt.pushName)
			assert.Equal(t, tt.want, got)
		})
	}
}
