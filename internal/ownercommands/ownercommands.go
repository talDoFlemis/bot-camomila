// Package ownercommands implements the kill-switch command dispatch for bot owners.
// It is pure Go with no whatsmeow dependency — the adapter pre-authorizes calls
// before invoking Handle, keeping this package on the inner hexagonal ring.
package ownercommands

import "github.com/taldoflemis/bot-camomila/internal/killswitch"

// Handle dispatches an owner command to the kill switch.
// Recognized commands:
//   - "!pause"  → calls ks.Pause() and returns "paused"
//   - "!resume" → calls ks.Resume() and returns "resumed"
//
// Any other command (including empty string) returns "" without modifying the switch.
// The adapter must authorize the sender before calling Handle.
func Handle(cmd string, ks *killswitch.Switch) string {
	switch cmd {
	case "!pause":
		ks.Pause()
		return "paused"
	case "!resume":
		ks.Resume()
		return "resumed"
	default:
		return ""
	}
}
