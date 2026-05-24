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
// and listener/matcher cross-reference checks.
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
//  1. clusters — build map, detect duplicate names
//  2. matchers — word-length/distance constraints, cluster reference resolution
//  3. listeners — require ≥1, validate group_jid/owner_jids, resolve matcher references
//  4. timezone — if non-empty, must resolve via time.LoadLocation (never time.Local)
//  5. log level — if non-empty, must be a known slog level
func validate(cfg Config) (*Snapshot, error) {
	// CHECK 1 — cluster map (detect duplicates)
	clusterMap := make(map[string][]string, len(cfg.AnswersClusters))
	for _, ac := range cfg.AnswersClusters {
		if _, exists := clusterMap[ac.Name]; exists {
			return nil, fmt.Errorf("clusters name %q appears more than once (ambiguous reference)", ac.Name)
		}
		clusterMap[ac.Name] = ac.Answers
	}

	// CHECK 2 — resolve global matchers (tagged-union validation, word-length constraints, cluster refs)
	matcherNames := make(map[string]struct{}, len(cfg.Matchers))
	globalMatchers := make(map[string]ResolvedMatcher, len(cfg.Matchers))
	for _, m := range cfg.Matchers {
		if _, dup := matcherNames[m.Name]; dup {
			return nil, fmt.Errorf("matchers name %q appears more than once", m.Name)
		}
		matcherNames[m.Name] = struct{}{}

		// Exactly one branch must be set.
		if m.Levenshtein == nil && m.Mention == nil {
			return nil, fmt.Errorf("matcher %q: must have either a levenshtein or mention block", m.Name)
		}
		if m.Levenshtein != nil && m.Mention != nil {
			return nil, fmt.Errorf("matcher %q: levenshtein and mention are mutually exclusive", m.Name)
		}

		var (
			kind        string
			words       []string
			distance    int
			clusterName string
			cooldownSec int
		)

		switch {
		case m.Levenshtein != nil:
			lv := m.Levenshtein
			kind = "levenshtein"
			words = lv.Words
			distance = lv.Distance
			clusterName = lv.Cluster
			cooldownSec = lv.CooldownSec

			if len(words) == 0 {
				return nil, fmt.Errorf("matcher %q (levenshtein): words must not be empty", m.Name)
			}
			for _, word := range words {
				runeCount := utf8.RuneCountInString(word)
				switch distance {
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

		case m.Mention != nil:
			kind = "mention"
			clusterName = m.Mention.Cluster
			cooldownSec = m.Mention.CooldownSec
		}

		answers, ok := clusterMap[clusterName]
		if !ok {
			return nil, fmt.Errorf("matcher %q references unknown cluster %q", m.Name, clusterName)
		}

		globalMatchers[m.Name] = ResolvedMatcher{
			Name:             m.Name,
			Kind:             kind,
			Words:            words,
			Distance:         distance,
			Answers:          answers,
			CooldownDuration: resolveCooldown(cooldownSec, 300),
		}
	}

	// CHECK 3 — listeners
	if len(cfg.Listeners) == 0 {
		return nil, fmt.Errorf("listeners: at least one listener is required")
	}

	resolvedListeners := make([]ResolvedListener, 0, len(cfg.Listeners))
	for i, l := range cfg.Listeners {
		// group_jid must parse as a group JID
		jid, err := types.ParseJID(l.GroupJID)
		if err != nil {
			return nil, fmt.Errorf("listeners[%d].group_jid %q is invalid: %w", i, l.GroupJID, err)
		}
		if jid.Server != types.GroupServer {
			return nil, fmt.Errorf("listeners[%d].group_jid must be a group JID (got server %q)", i, jid.Server)
		}

		// at least one matcher reference required
		if len(l.Matchers) == 0 {
			return nil, fmt.Errorf("listeners[%d] (group %q): at least one matcher is required", i, l.GroupJID)
		}

		// resolve matcher references
		listenerMatchers := make([]ResolvedMatcher, 0, len(l.Matchers))
		for _, mName := range l.Matchers {
			rm, ok := globalMatchers[mName]
			if !ok {
				return nil, fmt.Errorf("listeners[%d] (group %q): references unknown matcher %q", i, l.GroupJID, mName)
			}
			listenerMatchers = append(listenerMatchers, rm)
		}

		resolvedListeners = append(resolvedListeners, ResolvedListener{
			GroupJID:           l.GroupJID,
			AllowAdminCommands: l.AllowAdminCommands,
			Matchers:           listenerMatchers,
		})
	}

	// CHECK 4 — timezone
	var loc *time.Location
	if cfg.Limits.QuietHours.Timezone != "" {
		var err error
		loc, err = time.LoadLocation(cfg.Limits.QuietHours.Timezone)
		if err != nil {
			return nil, fmt.Errorf("limits.quiet_hours.timezone %q is invalid: %w", cfg.Limits.QuietHours.Timezone, err)
		}
	}

	// CHECK 5 — log level
	var logLevel *slog.Level
	if cfg.Log.Level != "" {
		lvl, err := parseLogLevel(cfg.Log.Level)
		if err != nil {
			return nil, err
		}
		logLevel = &lvl
	}

	return &Snapshot{
		Listeners:            resolvedListeners,
		Limits:               cfg.Limits,
		Log:                  cfg.Log,
		DB:                   cfg.DB,
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
