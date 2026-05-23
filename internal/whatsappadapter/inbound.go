// Package whatsappadapter - inbound.go handles incoming WhatsApp events.
package whatsappadapter

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/taldoflemis/bot-camomila/internal/config"
	"github.com/taldoflemis/bot-camomila/internal/pipeline"
)

// onEvent is the single event handler registered with the whatsmeow client via
// client.AddEventHandler. It type-switches on the event and dispatches accordingly.
//
// CRITICAL: Never call a.client.Disconnect() from inside this function — whatsmeow
// holds a dispatch lock while calling handlers; Disconnect() waits for that lock and
// would deadlock (RESEARCH.md Pitfall 6). On permanent-disconnect events (LoggedOut,
// StreamReplaced), call a.cancel() to signal the main goroutine to initiate shutdown.
func (a *Adapter) onEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Connected:
		slog.Info("whatsapp connected", "event", "connected")

	case *events.Disconnected:
		// Transient disconnect — whatsmeow auto-reconnects (SESSION-05).
		// Do NOT call a.cancel() here; that would cause an unnecessary full shutdown.
		slog.Warn("whatsapp disconnected; auto-reconnect in progress", "event", "disconnected")

	case *events.LoggedOut:
		// Permanent disconnect — session is invalidated (SESSION-04).
		slog.Error("whatsapp logged out",
			"event", "logged_out",
			"on_connect", v.OnConnect,
			"reason", v.Reason.String(),
		)
		a.cancel() // signal main goroutine to shut down; never call Disconnect() from here

	case *events.StreamReplaced:
		// Another client opened the same session — permanent disconnect (RESEARCH.md Pitfall 4).
		slog.Error("whatsapp stream replaced by another client; shutting down", "event", "stream_replaced")
		a.cancel() // same treatment as LoggedOut

	case *events.PairSuccess:
		slog.Info("whatsapp paired successfully",
			"event", "pair_success",
			"jid", v.ID.String(),
		)

	case *events.Message:
		a.handleMessage(v)
	}
}

// handleMessage applies the gate pipeline to a raw WhatsApp message event.
// It runs the adapter-level gates (history sync, scope, self-message, text-only),
// constructs a domain message, delegates to the Pipeline, and sends a threaded
// reply with jitter if the pipeline decides to fire.
func (a *Adapter) handleMessage(evt *events.Message) {
	// Single atomic load — hold snap for the full duration of this call (CONFIG-03).
	snap := a.cfg.Get()

	// Gate 0 — HistorySync timestamp filter (D-07, FIRST gate — cheapest elimination).
	// On first QR pair, whatsmeow replays weeks of group history. Drop all messages
	// predating bot startup so historical messages never reach the matcher pipeline.
	if evt.Info.Timestamp.Before(a.startTime) {
		return
	}

	// Gate 1 — listener lookup (SCOPE-01).
	// Only process messages from a configured group; drop everything else silently.
	listener := findListener(snap.Listeners, evt.Info.Chat.String())
	if listener == nil {
		slog.Debug("message dropped: group not configured",
			"event", "scope_drop",
			"group_jid", evt.Info.Chat.String(),
		)
		return
	}

	// Gate 2 — self-message filter (SCOPE-02).
	// Drop messages sent by the bot itself to prevent self-reply loops.
	if evt.Info.IsFromMe {
		slog.Debug("message dropped: from self",
			"event", "scope_drop",
			"reason", "is_from_me",
		)
		return
	}

	// Gate 3 — text-only filter (SCOPE-03).
	// Drop non-text message types (images, stickers, audio, etc.) before matching.
	text := extractText(evt.Message)
	if text == "" {
		slog.Debug("message dropped: non-text",
			"event", "scope_drop",
			"reason", "non_text",
		)
		return
	}

	// Construct pipeline.Message with all Phase 2 fields.
	quotedBody, quotedSender := extractQuotedText(evt.Message, a.botJID)
	msg := pipeline.Message{
		ID:              evt.Info.ID,
		GroupJID:        evt.Info.Chat.String(),
		SenderJID:       evt.Info.Sender.ToNonAD().String(),
		SenderPushName:  evt.Info.PushName,
		Text:            text,
		QuotedBody:      quotedBody,
		QuotedSenderJID: quotedSender,
		Timestamp:       evt.Info.Timestamp,
	}

	// Run the pipeline with this listener's matchers.
	decision := a.pipeline.Handle(msg, snap, listener.Matchers)

	// Log every dispatch decision (OBSERV-02).
	slog.Info("dispatch decision",
		"event", "dispatch",
		"msg_id", msg.ID,
		"sender_jid", msg.SenderJID,
		"matcher", decision.MatcherName,
		"matched_word", decision.MatchedWord,
		"reply", decision.Reply,
		"drop_reason", decision.DropReason,
	)

	if !decision.Reply {
		return
	}

	// Send reply in a goroutine with jitter (REPLY-04).
	// Do NOT block the event handler — whatsmeow holds a dispatch lock.
	go a.sendReply(evt, decision.Answer)
}

// sendReply sends a threaded WhatsApp reply to the message that triggered the match.
// It sleeps for a random 2-8s jitter before sending to appear more human (REPLY-04).
// This must be called from a goroutine — never from the event handler directly.
func (a *Adapter) sendReply(evt *events.Message, answer string) {
	// Random 2-8s jitter (REPLY-04).
	jitter := time.Duration(2+rand.IntN(7)) * time.Second
	time.Sleep(jitter)

	// Build threaded reply (REPLY-01): ExtendedTextMessage with ContextInfo.
	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(answer),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:      proto.String(evt.Info.ID),
				Participant:   proto.String(evt.Info.Sender.String()),
				QuotedMessage: evt.Message,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := a.client.SendMessage(ctx, evt.Info.Chat, msg)
	if err != nil {
		slog.Error("failed to send reply",
			"event", "send_error",
			"msg_id", evt.Info.ID,
			"err", err,
		)
		return
	}

	slog.Info("reply sent",
		"event", "reply_sent",
		"msg_id", evt.Info.ID,
		"jitter_ms", jitter.Milliseconds(),
	)
}

// findListener returns the ResolvedListener for the given group JID, or nil if not configured.
func findListener(listeners []config.ResolvedListener, groupJID string) *config.ResolvedListener {
	for i := range listeners {
		if listeners[i].GroupJID == groupJID {
			return &listeners[i]
		}
	}
	return nil
}

// extractText returns the plain-text content of a proto message, or "" if the message
// is not a text message. Checks conversation first, then extended text (linked previews).
func extractText(m *waE2E.Message) string {
	if m == nil {
		return ""
	}
	if t := m.GetConversation(); t != "" {
		return t
	}
	if ext := m.GetExtendedTextMessage(); ext != nil {
		return ext.GetText()
	}
	return ""
}

// extractQuotedText returns the plain text and sender JID of a quoted message.
// If the quoted message's author is the bot itself (identified by botJID), it returns
// empty strings to prevent quote-chain loops (Pitfall 6).
func extractQuotedText(m *waE2E.Message, botJID string) (body string, senderJID string) {
	if m == nil {
		return "", ""
	}

	// Quoted messages come through ExtendedTextMessage.ContextInfo.
	ext := m.GetExtendedTextMessage()
	if ext == nil {
		return "", ""
	}
	ci := ext.GetContextInfo()
	if ci == nil || ci.QuotedMessage == nil {
		return "", ""
	}

	// Extract the quoted message's sender.
	participant := ci.GetParticipant()

	// Quote-chain prevention: if the quoted author is the bot itself, return empty
	// so the pipeline won't match against the bot's own previous replies.
	if participant == botJID {
		return "", ""
	}

	// Extract text from the quoted message.
	quotedText := extractText(ci.QuotedMessage)
	return quotedText, participant
}
