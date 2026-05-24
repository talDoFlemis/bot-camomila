// Package domain contains transport-agnostic types for bot-camomila.
// No external dependencies — stdlib only.
package domain

import (
	"context"
	"time"
)

// InboundMessage is a transport-agnostic inbound group message.
type InboundMessage struct {
	ID              string
	GroupJID        string
	SenderJID       string
	SenderPushName  string
	Text            string
	QuotedBody      string
	QuotedSenderJID string
	Timestamp       time.Time
	MentionedBot    bool
	IsFromMe        bool
}

// OutboundReply is the pipeline's instruction to send a threaded reply.
type OutboundReply struct {
	InReplyTo    string // original message ID (for threading)
	ChatJID      string // where to send the reply
	SenderJID    string // original sender's JID (for ContextInfo.Participant)
	Answer       string // final text (variables already substituted)
	MatcherName  string // which matcher fired (empty for command acks)
	MatchedWord  string // the input token that matched (empty for command acks)
	IsCommandAck bool   // true = send immediately (no jitter)
}

// AdminChecker resolves whether a sender is a group admin.
// The transport adapter implements this; the pipeline calls it only when needed.
type AdminChecker interface {
	IsGroupAdmin(ctx context.Context, groupJID, senderJID string) (bool, error)
}
