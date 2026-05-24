package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/taldoflemis/bot-camomila/internal/config"
	"github.com/taldoflemis/bot-camomila/internal/cooldown"
	"github.com/taldoflemis/bot-camomila/internal/domain"
	"github.com/taldoflemis/bot-camomila/internal/killswitch"
)

// fakeAdminChecker implements domain.AdminChecker for testing
type fakeAdminChecker struct {
	isAdmin bool
	err     error
}

func (f *fakeAdminChecker) IsGroupAdmin(ctx context.Context, groupJID, senderJID string) (bool, error) {
	return f.isAdmin, f.err
}

// testMatchers returns the two standard test matchers.
func testMatchers() []config.ResolvedMatcher {
	return []config.ResolvedMatcher{
		{
			Name:             "tax",
			Kind:             "levenshtein",
			Words:            []string{"sefaz"},
			Distance:         1,
			Answers:          []string{"calma, vai dar certo!"},
			CooldownDuration: 5 * time.Minute,
		},
		{
			Name:             "traffic",
			Kind:             "levenshtein",
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
		Listeners: []config.ResolvedListener{
			{
				GroupJID:           "group1@g.us",
				AllowAdminCommands: true,
				Matchers:           testMatchers(),
			},
		},
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
func newTestPipeline(now *time.Time) (*Pipeline, *killswitch.Switch, *config.Store, *fakeAdminChecker) {
	ks := killswitch.New()
	fakeClock := func() time.Time { return *now }
	cd := cooldown.NewTracker(fakeClock)
	rl := NewRateLimiter(fakeClock)
	cfg := config.NewStore(testSnap())
	ac := &fakeAdminChecker{}
	pipe := New(cfg, ac, ks, cd, rl, fakeClock)
	return pipe, ks, cfg, ac
}

func TestHandle_KillSwitchDrops(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, ks, _, _ := newTestPipeline(&now)
	ks.Pause()

	msg := domain.InboundMessage{Text: "sefaz", SenderJID: "user@s.whatsapp.net"}
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
	pipe, _, _, _ := newTestPipeline(&now)

	msg := domain.InboundMessage{Text: "sefaz", SenderJID: "user@s.whatsapp.net"}
	snap := testSnap()

	d := pipe.Handle(msg, snap, testMatchers())
	assert.False(t, d.Reply)
	assert.Equal(t, "quiet_hours", d.DropReason)
}

func TestHandle_NoMatchDrops(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, _, _, _ := newTestPipeline(&now)

	msg := domain.InboundMessage{Text: "hello world", SenderJID: "user@s.whatsapp.net"}
	snap := testSnap()
	snap.Location = nil

	d := pipe.Handle(msg, snap, testMatchers())
	assert.False(t, d.Reply)
	assert.Equal(t, "no_match", d.DropReason)
}

func TestHandle_MatchFires(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, _, _, _ := newTestPipeline(&now)

	msg := domain.InboundMessage{Text: "sefaz", SenderJID: "user@s.whatsapp.net"}
	snap := testSnap()
	snap.Location = nil

	d := pipe.Handle(msg, snap, testMatchers())
	assert.True(t, d.Reply)
	assert.Equal(t, "tax", d.MatcherName)
	assert.NotEmpty(t, d.Answer)
}

func TestHandle_CooldownDrops(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, _, _, _ := newTestPipeline(&now)

	msg := domain.InboundMessage{Text: "sefaz", SenderJID: "user@s.whatsapp.net"}
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
	pipe, _, _, _ := newTestPipeline(&now)

	snap := testSnap()
	snap.Location = nil
	snap.Limits.RateCap.PerMin = 2
	snap.Limits.RateCap.PerHour = 100
	snap.UserCooldownDuration = 0

	matchers := testMatchers()
	matchers[0].CooldownDuration = 0 // disable per-matcher cooldown so rate cap is the only gate

	// Fire perMin times.
	for i := 0; i < 2; i++ {
		msg := domain.InboundMessage{Text: "sefaz", SenderJID: "user" + string(rune('A'+i)) + "@s.whatsapp.net"}
		d := pipe.Handle(msg, snap, matchers)
		assert.True(t, d.Reply, "fire %d should succeed", i)
	}

	// Next one should be rate-capped.
	msg := domain.InboundMessage{Text: "sefaz", SenderJID: "userZ@s.whatsapp.net"}
	d := pipe.Handle(msg, snap, matchers)
	assert.False(t, d.Reply)
	assert.Equal(t, "rate_cap", d.DropReason)
}

func TestHandle_QuotedTextMatch(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, _, _, _ := newTestPipeline(&now)

	msg := domain.InboundMessage{
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
	pipe, _, _, _ := newTestPipeline(&now)

	msg := domain.InboundMessage{
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
	pipe, _, _, _ := newTestPipeline(&now)

	msg := domain.InboundMessage{
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

func TestHandle_MentionFires(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, _, _, _ := newTestPipeline(&now)

	msg := domain.InboundMessage{
		Text:         "oi bot",
		SenderJID:    "user@s.whatsapp.net",
		MentionedBot: true,
	}
	snap := testSnap()
	snap.Location = nil

	mentionMatcher := config.ResolvedMatcher{
		Name:             "greet",
		Kind:             "mention",
		Answers:          []string{"olá!"},
		CooldownDuration: 5 * time.Minute,
	}
	matchers := append([]config.ResolvedMatcher{mentionMatcher}, testMatchers()...)

	d := pipe.Handle(msg, snap, matchers)
	assert.True(t, d.Reply)
	assert.Equal(t, "greet", d.MatcherName)
	assert.Equal(t, "@mention", d.MatchedWord)
}

func TestHandle_GateOrder(t *testing.T) {
	// Kill switch should be checked BEFORE quiet hours.
	loc := mustLoadLocation("America/Sao_Paulo")
	now := time.Date(2026, 5, 23, 23, 0, 0, 0, loc)
	pipe, ks, _, _ := newTestPipeline(&now)
	ks.Pause() // kill switch AND quiet hours both active

	msg := domain.InboundMessage{Text: "sefaz", SenderJID: "user@s.whatsapp.net"}
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

// Command path tests
func TestHandleCommand_PauseFromOwner(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, ks, cfg, _ := newTestPipeline(&now)
	snap := cfg.Get()

	msg := domain.InboundMessage{
		Text:      "!pause",
		GroupJID:  "group1@g.us",
		SenderJID: "owner@s.whatsapp.net",
		IsFromMe:  true,
	}

	reply, handled := pipe.handleCommand(context.Background(), msg, &snap.Listeners[0])
	assert.True(t, handled)
	assert.NotNil(t, reply)
	assert.True(t, reply.IsCommandAck)
	assert.Equal(t, "paused", reply.Answer)
	assert.True(t, ks.IsPaused())
}

func TestHandleCommand_ResumeWhilePaused(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, ks, cfg, _ := newTestPipeline(&now)
	ks.Pause()
	snap := cfg.Get()

	msg := domain.InboundMessage{
		Text:      "!resume",
		GroupJID:  "group1@g.us",
		SenderJID: "owner@s.whatsapp.net",
		IsFromMe:  true,
	}

	reply, handled := pipe.handleCommand(context.Background(), msg, &snap.Listeners[0])
	assert.True(t, handled)
	assert.NotNil(t, reply)
	assert.Equal(t, "resumed", reply.Answer)
	assert.False(t, ks.IsPaused())
}

func TestHandleCommand_DeniedNonOwner(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, ks, cfg, _ := newTestPipeline(&now)
	snap := cfg.Get()

	msg := domain.InboundMessage{
		Text:      "!pause",
		GroupJID:  "group1@g.us",
		SenderJID: "stranger@s.whatsapp.net",
	}

	reply, handled := pipe.handleCommand(context.Background(), msg, &snap.Listeners[0])
	assert.True(t, handled) // It IS a command...
	assert.Nil(t, reply)    // ...but denied, so no reply
	assert.False(t, ks.IsPaused())
}

func TestHandleCommand_AdminFallback(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, ks, cfg, ac := newTestPipeline(&now)
	ac.isAdmin = true // mock admin check returns true
	snap := cfg.Get()

	msg := domain.InboundMessage{
		Text:      "!pause",
		GroupJID:  "group1@g.us",
		SenderJID: "admin@s.whatsapp.net",
	}

	reply, handled := pipe.handleCommand(context.Background(), msg, &snap.Listeners[0])
	assert.True(t, handled)
	assert.NotNil(t, reply)
	assert.Equal(t, "paused", reply.Answer)
	assert.True(t, ks.IsPaused())
}

// Integration tests for Run()
func TestRun_ForwardsMatchToOutCh(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, _, _, _ := newTestPipeline(&now)

	in := make(chan domain.InboundMessage, 1)
	out := make(chan domain.OutboundReply, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go pipe.Run(ctx, in, out)

	in <- domain.InboundMessage{
		ID:        "msg1",
		GroupJID:  "group1@g.us",
		Text:      "sefaz",
		SenderJID: "user@s.whatsapp.net",
	}

	select {
	case reply := <-out:
		assert.Equal(t, "msg1", reply.InReplyTo)
		assert.Equal(t, "tax", reply.MatcherName)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for reply")
	}
}

func TestRun_DropsNonConfiguredGroup(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, _, _, _ := newTestPipeline(&now)

	in := make(chan domain.InboundMessage, 1)
	out := make(chan domain.OutboundReply, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go pipe.Run(ctx, in, out)

	in <- domain.InboundMessage{
		ID:        "msg1",
		GroupJID:  "unknown@g.us",
		Text:      "sefaz",
		SenderJID: "user@s.whatsapp.net",
	}

	select {
	case <-out:
		t.Fatal("expected no reply for unknown group")
	case <-time.After(100 * time.Millisecond):
		// pass
	}
}

func TestRun_StopsOnContextCancel(t *testing.T) {
	now := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	pipe, _, _, _ := newTestPipeline(&now)

	in := make(chan domain.InboundMessage, 1)
	out := make(chan domain.OutboundReply, 1)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		pipe.Run(ctx, in, out)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// pass
	case <-time.After(1 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}
