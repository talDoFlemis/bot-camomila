// Package whatsappadapter - inbound.go handles incoming WhatsApp events.
package whatsappadapter

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/taldoflemis/bot-camomila/internal/domain"
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
		slog.Error(
			"whatsapp logged out",
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
		slog.Info(
			"whatsapp paired successfully",
			"event", "pair_success",
			"jid", v.ID.String(),
		)

	case *events.Message:
		a.handleMessage(v)
	}
}

// handleMessage runs the transport-specific gates and forwards the message to the pipeline.
func (a *Adapter) handleMessage(evt *events.Message) {
	// Gate 0 — HistorySync timestamp filter (adapter-specific).
	if evt.Info.Timestamp.Before(a.startTime) {
		return
	}

	// Gate 1 — Text-only filter (adapter-specific).
	text := extractText(evt.Message)
	if text == "" {
		slog.Debug(
			"message dropped: non-text",
			"event", "scope_drop",
			"reason", "non_text",
		)
		return
	}

	// Store original event for reply threading (QuotedMessage preservation).
	a.pending.Store(evt.Info.ID, evt)

	quotedBody, quotedSender := extractQuotedText(evt.Message, a.botJID)
	a.inCh <- domain.InboundMessage{
		ID:              evt.Info.ID,
		GroupJID:        evt.Info.Chat.String(),
		SenderJID:       evt.Info.Sender.ToNonAD().String(),
		SenderPushName:  evt.Info.PushName,
		Text:            text,
		QuotedBody:      quotedBody,
		QuotedSenderJID: quotedSender,
		Timestamp:       evt.Info.Timestamp,
		MentionedBot:    isBotMentioned(evt.Message, a.botJID, a.botLID),
		IsFromMe:        evt.Info.IsFromMe,
	}
}

// ReplyLoop reads from outCh and sends WhatsApp replies.
func (a *Adapter) ReplyLoop(ctx context.Context) {
	reaper := time.NewTicker(30 * time.Second)
	defer reaper.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case reply, ok := <-a.outCh:
			if !ok {
				return
			}
			go a.sendThreadedReply(reply)
		case <-reaper.C:
			a.prunePending()
		}
	}
}

// prunePending deletes entries from the pending map that are older than 60 seconds.
func (a *Adapter) prunePending() {
	cutoff := time.Now().Add(-60 * time.Second)
	a.pending.Range(func(key, value interface{}) bool {
		evt, ok := value.(*events.Message)
		if ok && evt.Info.Timestamp.Before(cutoff) {
			a.pending.Delete(key)
		}
		return true
	})
}

// sendThreadedReply sends a threaded WhatsApp reply. Applies jitter for
// regular replies; sends immediately for command acks.
func (a *Adapter) sendThreadedReply(reply domain.OutboundReply) {
	if !reply.IsCommandAck {
		jitter := time.Duration(2+rand.IntN(7)) * time.Second
		time.Sleep(jitter)
	}

	// Look up original event for full reply threading (QuotedMessage preview).
	var quotedMsg *waE2E.Message
	var participant string
	if val, ok := a.pending.LoadAndDelete(reply.InReplyTo); ok {
		evt := val.(*events.Message)
		quotedMsg = textOnlyQuote(evt.Message)
		participant = evt.Info.Sender.ToNonAD().String()
	} else {
		slog.Warn("original event not found for reply", "msg_id", reply.InReplyTo)
		participant = reply.SenderJID // fallback
	}

	msg := &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(reply.Answer),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:      proto.String(reply.InReplyTo),
				Participant:   proto.String(participant),
				QuotedMessage: quotedMsg, // nil if not found — reply still threads
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	chatJID, err := types.ParseJID(reply.ChatJID)
	if err != nil {
		slog.Error(
			"failed to parse chat JID for reply",
			"event", "send_error",
			"chat_jid", reply.ChatJID,
			"err", err,
		)
		return
	}

	_, err = a.client.SendMessage(ctx, chatJID, msg)
	if err != nil {
		slog.Error(
			"failed to send reply",
			"event", "send_error",
			"msg_id", reply.InReplyTo,
			"err", err,
		)
		return
	}

	slog.Info(
		"reply sent",
		"event", "reply_sent",
		"msg_id", reply.InReplyTo,
		"is_command_ack", reply.IsCommandAck,
	)
}

// isBotMentioned returns true if any of the bot's known JIDs (phone-based @s.whatsapp.net
// or LID-based @lid) appears in the message's ContextInfo.MentionedJID list.
// Newer WhatsApp clients send mentions in @lid form; older ones use @s.whatsapp.net.
// Mentions only exist in ExtendedTextMessage — plain Conversation messages never carry them.
func isBotMentioned(m *waE2E.Message, botJID, botLID string) bool {
	if m == nil {
		return false
	}
	ext := m.GetExtendedTextMessage()
	if ext == nil {
		return false
	}
	ci := ext.GetContextInfo()
	if ci == nil {
		return false
	}
	for _, jid := range ci.GetMentionedJID() {
		if (botJID != "" && jid == botJID) || (botLID != "" && jid == botLID) {
			return true
		}
	}
	return false
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

// textOnlyQuote returns a minimal text-only Message proto suitable for use as a
// QuotedMessage. It strips all ContextInfo (MentionedJID, Participant, nested
// quotes, etc.) so the bot's reply never re-tags or re-notifies people who were
// referenced in the original message's context chain.
func textOnlyQuote(m *waE2E.Message) *waE2E.Message {
	if m == nil {
		return nil
	}
	text := extractText(m)
	if text == "" {
		return m // non-text message — pass through as-is
	}
	return &waE2E.Message{
		Conversation: proto.String(text),
	}
}
