// Package whatsappadapter - inbound.go handles incoming WhatsApp events.
package whatsappadapter

import (
	"log/slog"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types/events"
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
// Phase 1 implementation: gates only — no matcher dispatch yet.
// Phase 2 will add matcher pipeline dispatch after the final gate.
func (a *Adapter) handleMessage(evt *events.Message) {
	// Single atomic load — hold snap for the full duration of this call (CONFIG-03).
	snap := a.cfg.Get()

	// Gate 0 — HistorySync timestamp filter (D-07, FIRST gate — cheapest elimination).
	// On first QR pair, whatsmeow replays weeks of group history. Drop all messages
	// predating bot startup so historical messages never reach the matcher pipeline.
	if evt.Info.Timestamp.Before(a.startTime) {
		return
	}

	// Gate 1 — group JID scope filter (SCOPE-01).
	// Only process messages from the configured group; drop everything else silently.
	if evt.Info.Chat.String() != snap.Scope.GroupJID {
		slog.Debug("message dropped: wrong group",
			"event", "scope_drop",
			"group_jid", evt.Info.Chat.String(),
			"expected", snap.Scope.GroupJID,
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

	// Phase 1 terminal action: log the received message.
	// Phase 2 will wire matcher dispatch and reply here.
	slog.Info("message received",
		"event", "message_received",
		"group_jid", evt.Info.Chat.String(),
		"sender_jid", evt.Info.Sender.ToNonAD().String(), // ToNonAD strips device suffix (T-03-05)
		"msg_id", evt.Info.ID,
	)
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
