package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/taldoflemis/bot-camomila/internal/config"
)

// TestAllowAdminCommandsZeroValue verifies that a config YAML that omits
// allow_admin_commands loads without error and produces
// ResolvedListener.AllowAdminCommands == false (zero-value safety per D-04).
func TestAllowAdminCommandsZeroValue(t *testing.T) {
	yaml := `
clusters:
  - name: calmdown
    answers: ["Calma lá"]
matchers:
  - name: sefaz
    levenshtein:
      words: ["SEFAZ", "sefaz12"]
      distance: 1
      cluster: calmdown
      cooldown_sec: 300
listeners:
  - group_jid: "120363428452727309@g.us"
    owner_jids: ["558591074044@s.whatsapp.net"]
    matchers: [sefaz]
db:
  path: "./test.sqlite"
`
	path := writeTemp(t, yaml)
	snap, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v; want nil", err)
	}
	if len(snap.Listeners) != 1 {
		t.Fatalf("expected 1 listener, got %d", len(snap.Listeners))
	}
	if snap.Listeners[0].AllowAdminCommands != false {
		t.Errorf("AllowAdminCommands = %v; want false (zero-value)", snap.Listeners[0].AllowAdminCommands)
	}
}

// TestAllowAdminCommandsTrue verifies that a config YAML with
// allow_admin_commands: true produces ResolvedListener.AllowAdminCommands == true.
func TestAllowAdminCommandsTrue(t *testing.T) {
	yaml := `
clusters:
  - name: calmdown
    answers: ["Calma lá"]
matchers:
  - name: sefaz
    levenshtein:
      words: ["SEFAZ", "sefaz12"]
      distance: 1
      cluster: calmdown
      cooldown_sec: 300
listeners:
  - group_jid: "120363428452727309@g.us"
    owner_jids: ["558591074044@s.whatsapp.net"]
    allow_admin_commands: true
    matchers: [sefaz]
db:
  path: "./test.sqlite"
`
	path := writeTemp(t, yaml)
	snap, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v; want nil", err)
	}
	if len(snap.Listeners) != 1 {
		t.Fatalf("expected 1 listener, got %d", len(snap.Listeners))
	}
	if snap.Listeners[0].AllowAdminCommands != true {
		t.Errorf("AllowAdminCommands = %v; want true", snap.Listeners[0].AllowAdminCommands)
	}
}

// writeTemp writes content to a temp file and returns its path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}
