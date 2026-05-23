package matcher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/taldoflemis/bot-camomila/internal/config"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "NFC input stays the same",
			input: "caf\u00e9", // U+00E9 (precomposed é)
			want:  "caf\u00e9",
		},
		{
			name:  "NFD input normalizes to NFC",
			input: "cafe\u0301", // e + U+0301 (combining accent)
			want:  "caf\u00e9",
		},
		{
			name:  "Uppercase converts to lowercase",
			input: "HELLO",
			want:  "hello",
		},
		{
			name:  "Mixed case and accent",
			input: "Caf\u00c9", // uppercase É precomposed
			want:  "caf\u00e9",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Normalize(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "Simple whitespace split",
			input: "hello world",
			want:  []string{"hello", "world"},
		},
		{
			name:  "Strips punctuation",
			input: "hello! world.",
			want:  []string{"hello", "world"},
		},
		{
			name:  "Multiple spaces and tabs",
			input: "  hello \t world  ",
			want:  []string{"hello", "world"},
		},
		{
			name:  "Empty string returns empty slice",
			input: "",
			want:  []string{},
		},
		{
			name:  "Emoji-only tokens are kept",
			input: "🔥 hello",
			want:  []string{"🔥", "hello"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Tokenize(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// helper builds a levenshtein ResolvedMatcher with sensible defaults.
func mkMatcher(name string, words []string, distance int) config.ResolvedMatcher {
	return config.ResolvedMatcher{
		Name:             name,
		Kind:             "levenshtein",
		Words:            words,
		Distance:         distance,
		Answers:          []string{"reply"},
		CooldownDuration: 5 * time.Minute,
	}
}

// helper builds a mention ResolvedMatcher.
func mkMentionMatcher(name string) config.ResolvedMatcher {
	return config.ResolvedMatcher{
		Name:             name,
		Kind:             "mention",
		Answers:          []string{"reply"},
		CooldownDuration: 5 * time.Minute,
	}
}

func TestMatch(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		mentionedBot bool
		matchers     []config.ResolvedMatcher
		want         *Result
	}{
		{
			name:     "Exact match distance 0",
			text:     "sefaz",
			matchers: []config.ResolvedMatcher{mkMatcher("tax", []string{"sefaz"}, 0)},
			want: &Result{
				MatcherName: "tax",
				MatchedWord: "sefaz",
				KeywordHit:  "sefaz",
				Distance:    0,
			},
		},
		{
			name:     "Fuzzy match distance 1 substitution",
			text:     "sefas",
			matchers: []config.ResolvedMatcher{mkMatcher("tax", []string{"SEFAZ"}, 1)},
			want: &Result{
				MatcherName: "tax",
				MatchedWord: "sefas",
				KeywordHit:  "SEFAZ",
				Distance:    1,
			},
		},
		{
			name:     "Fuzzy match distance 1 insertion",
			text:     "sefazz",
			matchers: []config.ResolvedMatcher{mkMatcher("tax", []string{"SEFAZ"}, 1)},
			want: &Result{
				MatcherName: "tax",
				MatchedWord: "sefazz",
				KeywordHit:  "SEFAZ",
				Distance:    1,
			},
		},
		{
			name:     "No match distance too far",
			text:     "abcde",
			matchers: []config.ResolvedMatcher{mkMatcher("tax", []string{"SEFAZ"}, 1)},
			want:     nil,
		},
		{
			name:     "Short word (≤4 runes) rejects fuzzy at distance 1",
			text:     "rk",
			matchers: []config.ResolvedMatcher{mkMatcher("state", []string{"RJ"}, 1)},
			want:     nil, // 2 runes → max effective distance 0; "rk" is 1 away from "rj"
		},
		{
			name:     "NFC NFD equivalence",
			text:     Normalize("cafe\u0301"), // NFD café → normalized to NFC "café" before Match
			matchers: []config.ResolvedMatcher{mkMatcher("drink", []string{"caf\u00e9"}, 0)},
			want: &Result{
				MatcherName: "drink",
				MatchedWord: "caf\u00e9", // after normalization, both NFC and NFD become "café"
				KeywordHit:  "caf\u00e9",
				Distance:    0,
			},
		},
		{
			name: "Multiple matchers returns first",
			text: "sefaz detran",
			matchers: []config.ResolvedMatcher{
				mkMatcher("tax", []string{"sefaz"}, 0),
				mkMatcher("traffic", []string{"detran"}, 0),
			},
			want: &Result{
				MatcherName: "tax",
				MatchedWord: "sefaz",
				KeywordHit:  "sefaz",
				Distance:    0,
			},
		},
		{
			name:     "No matchers configured",
			text:     "hello",
			matchers: nil,
			want:     nil,
		},
		{
			name:     "Empty text",
			text:     "",
			matchers: []config.ResolvedMatcher{mkMatcher("tax", []string{"sefaz"}, 0)},
			want:     nil,
		},
		{
			name:     "Distance 0 exact only no fuzzy",
			text:     "sefas",
			matchers: []config.ResolvedMatcher{mkMatcher("tax", []string{"sefaz"}, 0)},
			want:     nil,
		},
		{
			name:     "7-rune token capped at distance 1, rejects distance-2 keyword",
			text:     "abcdefg",
			matchers: []config.ResolvedMatcher{mkMatcher("short", []string{"abcdexy"}, 2)},
			want:     nil, // 7 runes → max effective distance 1; keyword is 2 away
		},
		{
			name:     "Distance 2 match with 9-rune token",
			text:     "abcdefghi",
			matchers: []config.ResolvedMatcher{mkMatcher("long", []string{"abcdefgxy"}, 2)},
			want: &Result{
				MatcherName: "long",
				MatchedWord: "abcdefghi",
				KeywordHit:  "abcdefgxy",
				Distance:    2,
			},
		},
		// Mention matcher tests.
		{
			name:         "Mention fires when bot mentioned",
			text:         "oi tudo bem",
			mentionedBot: true,
			matchers:     []config.ResolvedMatcher{mkMentionMatcher("greet")},
			want: &Result{
				MatcherName: "greet",
				MatchedWord: "@mention",
				KeywordHit:  "@mention",
				Distance:    0,
			},
		},
		{
			name:         "Mention does not fire when bot not mentioned",
			text:         "oi tudo bem",
			mentionedBot: false,
			matchers:     []config.ResolvedMatcher{mkMentionMatcher("greet")},
			want:         nil,
		},
		{
			name:         "Mention before levenshtein in order: mention wins",
			text:         "sefaz",
			mentionedBot: true,
			matchers: []config.ResolvedMatcher{
				mkMentionMatcher("greet"),
				mkMatcher("tax", []string{"sefaz"}, 0),
			},
			want: &Result{MatcherName: "greet", MatchedWord: "@mention", KeywordHit: "@mention"},
		},
		{
			name:         "Levenshtein before mention in order: levenshtein wins",
			text:         "sefaz",
			mentionedBot: true,
			matchers: []config.ResolvedMatcher{
				mkMatcher("tax", []string{"sefaz"}, 0),
				mkMentionMatcher("greet"),
			},
			want: &Result{MatcherName: "tax", MatchedWord: "sefaz", KeywordHit: "sefaz"},
		},
		{
			name:         "Mention skipped falls through to levenshtein",
			text:         "sefaz",
			mentionedBot: false,
			matchers: []config.ResolvedMatcher{
				mkMentionMatcher("greet"),
				mkMatcher("tax", []string{"sefaz"}, 0),
			},
			want: &Result{MatcherName: "tax", MatchedWord: "sefaz", KeywordHit: "sefaz"},
		},
		{
			name:         "Mention fires on empty text",
			text:         "",
			mentionedBot: true,
			matchers:     []config.ResolvedMatcher{mkMentionMatcher("greet")},
			want: &Result{
				MatcherName: "greet",
				MatchedWord: "@mention",
				KeywordHit:  "@mention",
				Distance:    0,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Match(tt.text, tt.mentionedBot, tt.matchers)
			if tt.want == nil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
				assert.Equal(t, tt.want.MatcherName, got.MatcherName)
				assert.Equal(t, tt.want.MatchedWord, got.MatchedWord)
				if tt.want.KeywordHit != "" {
					assert.Equal(t, tt.want.KeywordHit, got.KeywordHit)
				}
				assert.Equal(t, tt.want.Distance, got.Distance)
			}
		})
	}
}
