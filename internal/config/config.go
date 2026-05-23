// Package config defines configuration types for bot-camomila.
// This file contains type definitions only; no functions.
package config

import (
	"log/slog"
	"time"
)

// Config is the raw YAML-parsed configuration. Fields map directly to config.yaml sections.
type Config struct {
	AnswersClusters []Cluster        `yaml:"clusters"`
	Matchers        []MatcherConfig  `yaml:"matchers"`
	Listeners       []ListenerConfig `yaml:"listeners"`
	Limits          LimitsConfig     `yaml:"limits"`
	Log             LogConfig        `yaml:"log"`
	DB              DBConfig         `yaml:"db"`
}

// Cluster is a named pool of answer strings. Matchers reference clusters by name.
type Cluster struct {
	Name    string   `yaml:"name"`
	Answers []string `yaml:"answers"`
}

// MatcherConfig is one fuzzy-match rule as parsed from YAML.
// Exactly one of Levenshtein or Mention must be set.
type MatcherConfig struct {
	Name        string                   `yaml:"name"`
	Levenshtein *LevenshteinMatcherConfig `yaml:"levenshtein,omitempty"`
	Mention     *MentionMatcherConfig     `yaml:"mention,omitempty"`
}

// LevenshteinMatcherConfig holds fields for keyword fuzzy-matching.
type LevenshteinMatcherConfig struct {
	Words       []string `yaml:"words"`
	Distance    int      `yaml:"distance"`
	Cluster     string   `yaml:"cluster"`
	CooldownSec int      `yaml:"cooldown_sec"`
}

// MentionMatcherConfig holds fields for @mention-triggered matching.
type MentionMatcherConfig struct {
	Cluster     string `yaml:"cluster"`
	CooldownSec int    `yaml:"cooldown_sec"`
}

// ListenerConfig binds a WhatsApp group to a set of matchers.
type ListenerConfig struct {
	GroupJID  string   `yaml:"group_jid"`
	OwnerJIDs []string `yaml:"owner_jids"`
	Matchers  []string `yaml:"matchers"` // ordered list of MatcherConfig.Name references
}

// LimitsConfig holds behavioral rate and quiet-hours limits.
type LimitsConfig struct {
	QuietHours      QuietHoursConfig `yaml:"quiet_hours"`
	RateCap         RateCapConfig    `yaml:"rate_cap"`
	UserCooldownSec int              `yaml:"user_cooldown_sec"` // global per-user cooldown in seconds (default 900 = 15 min)
}

// QuietHoursConfig defines a time window during which the bot stays silent.
type QuietHoursConfig struct {
	Start    string `yaml:"start"`    // e.g. "22:00"
	End      string `yaml:"end"`      // e.g. "08:00"
	Timezone string `yaml:"timezone"` // IANA name e.g. "America/Sao_Paulo"
}

// RateCapConfig limits how many replies the bot can send per time window.
type RateCapConfig struct {
	PerMin  int `yaml:"per_min"`
	PerHour int `yaml:"per_hour"`
}

// LogConfig controls log output format and level.
type LogConfig struct {
	Format string `yaml:"format"` // "json" | "text" | "" (auto-detect via isatty)
	Level  string `yaml:"level"`  // "debug" | "info" | "warn" | "error" | "" (keep current)
}

// DBConfig holds the SQLite session database path.
type DBConfig struct {
	Path string `yaml:"path"` // e.g. "./session.sqlite"
}

// Snapshot is the immutable, resolved form of Config. Cluster and matcher references are
// fully resolved per listener. Callers must hold this pointer for the full duration of one
// message-handling call and must not call Get repeatedly within one call.
type Snapshot struct {
	Listeners            []ResolvedListener
	Limits               LimitsConfig
	Log                  LogConfig
	DB                   DBConfig
	Location             *time.Location // resolved from QuietHours.Timezone (nil if not configured)
	UserCooldownDuration time.Duration  // resolved from LimitsConfig.UserCooldownSec
	LogLevel             *slog.Level    // nil = keep current level; set when log.level is configured
}

// ResolvedListener is a listener with its matchers fully resolved.
type ResolvedListener struct {
	GroupJID  string
	OwnerJIDs []string
	Matchers  []ResolvedMatcher
}

// ResolvedMatcher is a matcher with its answer cluster already resolved.
type ResolvedMatcher struct {
	Name             string
	Kind             string        // "levenshtein" | "mention"
	Words            []string      // empty for mention matchers
	Distance         int           // 0 for mention matchers
	Answers          []string      // resolved from AnswersCluster at load time
	CooldownDuration time.Duration // resolved from MatcherConfig.CooldownSec
}
