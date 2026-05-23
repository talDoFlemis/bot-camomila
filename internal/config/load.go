package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
	"go.mau.fi/whatsmeow/types"
)

// Load reads and validates the YAML config at path, returning an immutable Snapshot.
// It uses strict decoding (unknown fields are rejected) and runs full load-time validation
// including JID parsing, timezone lookup, distance/word-length constraints, cluster resolution,
// and self-loop guard.
func Load(path string) (*Snapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	var cfg Config
	dec := yaml.NewDecoder(f, yaml.DisallowUnknownField())
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	return validate(cfg)
}

// validate applies all load-time validation checks and returns the resolved Snapshot.
// Checks are performed in a defined order:
//  1. group_jid — must parse as a group JID (server == "g.us")
//  2. owner_jids — each must parse as a valid JID
//  3. timezone — if non-empty, must resolve via time.LoadLocation (never time.Local)
//  4. cluster resolution — each matcher's cluster ref must resolve; no duplicates allowed
//  5. distance min-length — distance 1 → word ≥5 runes; distance 2 → word ≥8 runes
//  6. self-loop guard — no answer token may exactly match any matcher keyword (lowercased)
func validate(cfg Config) (*Snapshot, error) {
	// CHECK 1 — group_jid
	if cfg.Scope.GroupJID != "" {
		jid, err := types.ParseJID(cfg.Scope.GroupJID)
		if err != nil {
			return nil, fmt.Errorf("group_jid %q is invalid: %w", cfg.Scope.GroupJID, err)
		}
		if jid.Server != types.GroupServer {
			return nil, fmt.Errorf("group_jid must be a group JID (got server %q)", jid.Server)
		}
	}

	// CHECK 2 — owner_jids
	for i, raw := range cfg.Scope.OwnerJIDs {
		if _, err := types.ParseJID(raw); err != nil {
			return nil, fmt.Errorf("owner_jids[%d] %q is invalid: %w", i, raw, err)
		}
	}

	// CHECK 3 — timezone
	var loc *time.Location
	if cfg.Limits.QuietHours.Timezone != "" {
		var err error
		loc, err = time.LoadLocation(cfg.Limits.QuietHours.Timezone)
		if err != nil {
			return nil, fmt.Errorf("limits.quiet_hours.timezone %q is invalid: %w", cfg.Limits.QuietHours.Timezone, err)
		}
	}

	// CHECK 4 — cluster resolution (build map, detect duplicates)
	clusterMap := make(map[string][]string, len(cfg.AnswersClusters))
	for _, ac := range cfg.AnswersClusters {
		if _, exists := clusterMap[ac.Name]; exists {
			return nil, fmt.Errorf("answers_cluster name %q appears more than once (ambiguous reference)", ac.Name)
		}
		clusterMap[ac.Name] = ac.Answers
	}

	// CHECK 5 — distance min-length and CHECK 6 — self-loop guard (per matcher)
	resolved := make([]ResolvedMatcher, 0, len(cfg.Matchers))
	for _, m := range cfg.Matchers {
		// CHECK 5: enforce word-length minimums for each word
		for _, word := range m.Words {
			runeCount := utf8.RuneCountInString(word)
			switch m.Distance {
			case 1:
				if runeCount < 5 {
					return nil, fmt.Errorf("matcher %q: word %q has %d runes but distance 1 requires ≥5 runes", m.Name, word, runeCount)
				}
			case 2:
				if runeCount < 8 {
					return nil, fmt.Errorf("matcher %q: word %q has %d runes but distance 2 requires ≥8 runes", m.Name, word, runeCount)
				}
			}
		}

		// CHECK 4 (continued): resolve cluster reference
		answers, ok := clusterMap[m.Cluster]
		if !ok {
			return nil, fmt.Errorf("matcher %q references unknown cluster %q", m.Name, m.Cluster)
		}

		// CHECK 6 — self-loop guard
		// Build a lowercase set of matcher keywords.
		kwSet := make(map[string]struct{}, len(m.Words))
		for _, w := range m.Words {
			kwSet[strings.ToLower(w)] = struct{}{}
		}
		for _, answer := range answers {
			for _, token := range strings.Fields(answer) {
				lToken := strings.ToLower(token)
				if _, found := kwSet[lToken]; found {
					return nil, fmt.Errorf(
						"answer in cluster %q contains keyword %q from matcher %q (self-loop)",
						m.Cluster, token, m.Name,
					)
				}
			}
		}

		resolved = append(resolved, ResolvedMatcher{
			Name:             m.Name,
			Words:            m.Words,
			Distance:         m.Distance,
			Answers:          answers,
			CooldownDuration: resolveCooldown(m.CooldownSec, 300),
		})
	}

	// CHECK 7 — log level
	var logLevel *slog.Level
	if cfg.Log.Level != "" {
		lvl, err := parseLogLevel(cfg.Log.Level)
		if err != nil {
			return nil, err
		}
		logLevel = &lvl
	}

	return &Snapshot{
		Scope:                cfg.Scope,
		Limits:               cfg.Limits,
		Log:                  cfg.Log,
		DB:                   cfg.DB,
		Matchers:             resolved,
		Location:             loc,
		UserCooldownDuration: resolveCooldown(cfg.Limits.UserCooldownSec, 900),
		LogLevel:             logLevel,
	}, nil
}

// parseLogLevel converts a string to slog.Level. Returns error for unknown values.
func parseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("log.level %q is invalid (valid: debug, info, warn, error)", s)
	}
}

// resolveCooldown converts a seconds value to time.Duration, using defaultSec if value is 0.
func resolveCooldown(sec, defaultSec int) time.Duration {
	if sec <= 0 {
		sec = defaultSec
	}
	return time.Duration(sec) * time.Second
}
