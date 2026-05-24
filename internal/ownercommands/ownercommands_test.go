package ownercommands_test

import (
	"testing"

	"github.com/taldoflemis/bot-camomila/internal/killswitch"
	"github.com/taldoflemis/bot-camomila/internal/ownercommands"
)

func TestHandle(t *testing.T) {
	tests := []struct {
		name           string
		cmd            string
		setup          func(ks *killswitch.Switch) // state before Handle is called
		wantReturn     string
		wantIsPaused   bool
		wantIsPausedOk bool // whether wantIsPaused should be checked
	}{
		{
			name:           "pause command pauses bot and returns paused",
			cmd:            "!pause",
			setup:          nil, // starts unpaused
			wantReturn:     "paused",
			wantIsPaused:   true,
			wantIsPausedOk: true,
		},
		{
			name: "resume command unpauses bot and returns resumed",
			cmd:  "!resume",
			setup: func(ks *killswitch.Switch) {
				ks.Pause() // start paused
			},
			wantReturn:     "resumed",
			wantIsPaused:   false,
			wantIsPausedOk: true,
		},
		{
			name:           "unknown command returns empty string and does not change state",
			cmd:            "!unknown",
			setup:          nil,
			wantReturn:     "",
			wantIsPaused:   false,
			wantIsPausedOk: true,
		},
		{
			name:           "empty string returns empty string and does not panic",
			cmd:            "",
			setup:          nil,
			wantReturn:     "",
			wantIsPaused:   false,
			wantIsPausedOk: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ks := killswitch.New()
			if tt.setup != nil {
				tt.setup(ks)
			}

			got := ownercommands.Handle(tt.cmd, ks)

			if got != tt.wantReturn {
				t.Errorf("Handle(%q) = %q; want %q", tt.cmd, got, tt.wantReturn)
			}
			if tt.wantIsPausedOk {
				if ks.IsPaused() != tt.wantIsPaused {
					t.Errorf("after Handle(%q): IsPaused() = %v; want %v", tt.cmd, ks.IsPaused(), tt.wantIsPaused)
				}
			}
		})
	}
}
