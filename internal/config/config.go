// Package config defines configuration types for bot-camomila.
// This file contains type definitions only; no functions.
package config

// Config is the raw YAML-parsed configuration. Fields map directly to config.yaml sections.
type Config struct {
	AnswersClusters []AnswersCluster `yaml:"answers_cluster"`
	Matchers        []MatcherConfig  `yaml:"matchers"`
	Scope           ScopeConfig      `yaml:"scope"`
	Limits          LimitsConfig     `yaml:"limits"`
	Log             LogConfig        `yaml:"log"`
	DB              DBConfig         `yaml:"db"`
}

// AnswersCluster is a named pool of answer strings. Matchers reference clusters by name.
type AnswersCluster struct {
	Name    string   `yaml:"name"`
	Answers []string `yaml:"answers"`
}

// MatcherConfig is one fuzzy-match rule as parsed from YAML.
type MatcherConfig struct {
	Name     string   `yaml:"name"`
	Words    []string `yaml:"words"`
	Distance int      `yaml:"distance"`
	Cluster  string   `yaml:"cluster"` // references AnswersCluster.Name
}

// ScopeConfig restricts which group and which owners the bot responds to.
type ScopeConfig struct {
	GroupJID  string   `yaml:"group_jid"`
	OwnerJIDs []string `yaml:"owner_jids"`
}

// LimitsConfig holds behavioral rate and quiet-hours limits.
type LimitsConfig struct {
	QuietHours QuietHoursConfig `yaml:"quiet_hours"`
	RateCap    RateCapConfig    `yaml:"rate_cap"`
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

// LogConfig controls log output format.
type LogConfig struct {
	Format string `yaml:"format"` // "json" | "text" | "" (auto-detect via isatty)
}

// DBConfig holds the SQLite session database path.
type DBConfig struct {
	Path string `yaml:"path"` // e.g. "./session.sqlite"
}

// Snapshot is the immutable, resolved form of Config. Cluster references in
// MatcherConfig are resolved into answer slices. Callers must hold this pointer
// for the full duration of one message-handling call and must not call Get
// repeatedly within one call.
type Snapshot struct {
	Scope    ScopeConfig
	Limits   LimitsConfig
	Log      LogConfig
	DB       DBConfig
	Matchers []ResolvedMatcher
}

// ResolvedMatcher is a matcher with its answer cluster already resolved.
type ResolvedMatcher struct {
	Name     string
	Words    []string
	Distance int
	Answers  []string // resolved from AnswersCluster at load time
}
