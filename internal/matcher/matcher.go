// Package matcher implements per-token fuzzy keyword matching using Levenshtein
// distance. It tokenizes input text on whitespace boundaries, normalizes tokens
// to NFC lowercase, and compares each token against configured keywords.
package matcher

import (
	"strings"
	"unicode"

	"github.com/agnivade/levenshtein"
	"golang.org/x/text/unicode/norm"

	"github.com/taldoflemis/bot-camomila/internal/config"
)

// Result describes a single match: which matcher fired, which input token
// triggered it, which configured keyword it matched against, and the computed
// Levenshtein distance.
type Result struct {
	MatcherName string // which matcher fired
	MatchedWord string // the input token that triggered the match
	KeywordHit  string // the configured keyword it matched against
	Distance    int    // actual Levenshtein distance
}

// Normalize applies NFC Unicode normalization and lowercasing to s.
// Callers should normalize message text once before passing it to Match.
func Normalize(s string) string {
	return strings.ToLower(norm.NFC.String(s))
}

// Tokenize splits s on whitespace, strips leading/trailing punctuation from
// each token, and drops any tokens that become empty after stripping.
func Tokenize(s string) []string {
	fields := strings.Fields(s)
	tokens := make([]string, 0, len(fields))
	for _, f := range fields {
		t := strings.TrimFunc(f, func(r rune) bool {
			return unicode.In(r, unicode.Punct)
		})
		if t != "" {
			tokens = append(tokens, t)
		}
	}
	return tokens
}

// maxDistanceForRuneLen returns the maximum Levenshtein distance allowed for a
// token of the given rune length: 0 for 1–4 runes, 1 for 5–8, 2 for 9+.
func maxDistanceForRuneLen(runeLen int) int {
	switch {
	case runeLen <= 4:
		return 0
	case runeLen <= 8:
		return 1
	default:
		return 2
	}
}

// Match iterates matchers in order and returns the first match.
// mentionedBot should be true when the bot's JID appears in the message's
// MentionedJID list — this activates "mention" kind matchers.
// For quoted-text passes, always pass mentionedBot=false.
func Match(text string, mentionedBot bool, matchers []config.ResolvedMatcher) *Result {
	tokens := Tokenize(text)

	for i := range matchers {
		m := &matchers[i]

		switch m.Kind {
		case "mention":
			if mentionedBot {
				return &Result{
					MatcherName: m.Name,
					MatchedWord: "@mention",
					KeywordHit:  "@mention",
					Distance:    0,
				}
			}
			continue

		default: // "levenshtein"
			if len(tokens) == 0 {
				continue
			}

			// Pre-normalize keywords once per matcher (not per token).
			normKeywords := make([]string, len(m.Words))
			for j, w := range m.Words {
				normKeywords[j] = Normalize(w)
			}

			for _, token := range tokens {
				runeLen := len([]rune(token))
				effectiveDist := m.Distance
				if cap := maxDistanceForRuneLen(runeLen); cap < effectiveDist {
					effectiveDist = cap
				}

				for j, kw := range normKeywords {
					dist := levenshtein.ComputeDistance(token, kw)
					if dist <= effectiveDist {
						return &Result{
							MatcherName: m.Name,
							MatchedWord: token,
							KeywordHit:  m.Words[j],
							Distance:    dist,
						}
					}
				}
			}
		}
	}

	return nil
}
