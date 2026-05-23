// Package domain contains transport-agnostic types for bot-camomila.
// No external dependencies — stdlib only.
package domain

import "time"

// Message is a transport-agnostic inbound group message. No whatsmeow types appear here.
// The whatsappadapter package is responsible for constructing Message values from
// whatsmeow event types.
type Message struct {
	// ID is the WhatsApp message ID.
	ID string
	// GroupJID is the group the message was sent to (string form of the JID).
	GroupJID string
	// SenderJID is the sender's JID in non-AD form (no device suffix).
	SenderJID string
	// Text is the plain text content of the message.
	Text string
	// Timestamp is when the message was sent.
	Timestamp time.Time
}
