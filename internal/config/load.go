package config

import (
	"fmt"
	"os"

	"github.com/fsnotify/fsnotify"
	"github.com/goccy/go-yaml"
)

// Load reads and validates the YAML config at path, returning an immutable Snapshot.
// This function will be fully implemented in Phase 1 Plan 02.
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

func validate(cfg Config) (*Snapshot, error) {
	// Validation logic will be added in Phase 1 Plan 02 (CONFIG-02).
	// For now, resolve clusters into a minimal snapshot.
	clusterMap := make(map[string][]string, len(cfg.AnswersClusters))
	for _, ac := range cfg.AnswersClusters {
		clusterMap[ac.Name] = ac.Answers
	}

	resolved := make([]ResolvedMatcher, 0, len(cfg.Matchers))
	for _, m := range cfg.Matchers {
		answers, ok := clusterMap[m.Cluster]
		if !ok {
			return nil, fmt.Errorf("matcher %q references unknown cluster %q", m.Name, m.Cluster)
		}
		resolved = append(resolved, ResolvedMatcher{
			Name:     m.Name,
			Words:    m.Words,
			Distance: m.Distance,
			Answers:  answers,
		})
	}

	return &Snapshot{
		Scope:    cfg.Scope,
		Limits:   cfg.Limits,
		Log:      cfg.Log,
		DB:       cfg.DB,
		Matchers: resolved,
	}, nil
}

// newWatcher creates an fsnotify.Watcher — referenced here to keep fsnotify in go.mod.
// Full watcher implementation lives in watcher.go (Phase 1 Plan 02).
func newWatcher() (*fsnotify.Watcher, error) {
	return fsnotify.NewWatcher()
}
