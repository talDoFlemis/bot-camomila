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

// minRuneLengthForDistance returns the minimum rune count a token must have to
// be eligible for matching at the given Levenshtein distance. This implements
// defense-in-depth for MATCH-05 (short-word rejection).
func minRuneLengthForDistance(distance int) int {
	switch distance {
	case 1:
		return 5
	case 2:
		return 8
	default:
		return 0 // distance 0 → no minimum
	}
}

// Match tokenizes the already-normalized text and checks each token against
// every configured matcher's keywords using Levenshtein distance. It returns
// the first match found (one reply per message) or nil if nothing matches.
func Match(text string, matchers []config.ResolvedMatcher) *Result {
	tokens := Tokenize(text)
	if len(tokens) == 0 {
		return nil
	}

	for i := range matchers {
		m := &matchers[i]

		// Pre-normalize keywords once per matcher (not per token).
		normKeywords := make([]string, len(m.Words))
		for j, w := range m.Words {
			normKeywords[j] = Normalize(w)
		}

		minLen := minRuneLengthForDistance(m.Distance)

		for _, token := range tokens {
			// Skip tokens that are too short for this matcher's distance.
			if len([]rune(token)) < minLen {
				continue
			}

			for j, kw := range normKeywords {
				dist := levenshtein.ComputeDistance(token, kw)
				if dist <= m.Distance {
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

	return nil
}
