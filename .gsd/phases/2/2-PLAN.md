---
phase: 2
plan: 2
wave: 1
depends_on: []
files_modified:
  - internal/matcher/matcher.go
  - internal/matcher/matcher_test.go
autonomous: true
user_setup: []

must_haves:
  truths:
    - "Matcher tokenizes input on whitespace/unicode boundaries"
    - "Matcher normalizes tokens to NFC lowercase before comparison"
    - "Matcher uses agnivade/levenshtein for distance computation"
    - "Matcher enforces min-word-length rules at match time (defense in depth)"
    - "Tests cover NFC/NFD equivalence, short-word rejection, exact match, fuzzy match, no-match"
  artifacts:
    - "internal/matcher/matcher.go exists with Match() function"
    - "internal/matcher/matcher_test.go exists with table-driven tests"
---

# Plan 2.2: Fuzzy Matcher Engine

<objective>
Build the core fuzzy matching engine as a standalone pure-Go package. It takes a normalized text and a list of matcher configs, returns which matchers fired and which token matched.

Purpose: This is the brains of the bot — the Levenshtein fuzzy keyword matcher. Pure function, no state, trivially testable.
Output: `internal/matcher/` package with `Match()` and comprehensive tests.
</objective>

<context>
Load for context:
- .gsd/SPEC.md (MATCH-01 through MATCH-05)
- .gsd/ARCHITECTURE.md
- .gsd/research/PITFALLS.md (Pitfall 10: Levenshtein false positives, Pitfall 11: Unicode normalization)
- internal/config/config.go (ResolvedMatcher type)
</context>

<tasks>

<task type="auto">
  <name>Implement the fuzzy matcher engine</name>
  <files>internal/matcher/matcher.go</files>
  <action>
    Create package `matcher` with these types and functions:

    1. `type Result struct`:
       - MatcherName string  // which matcher fired
       - MatchedWord string  // the input token that triggered the match
       - KeywordHit  string  // the configured keyword it matched against
       - Distance    int     // actual Levenshtein distance

    2. `func Normalize(s string) string`:
       - NFC-normalize using `golang.org/x/text/unicode/norm`
       - Lowercase using `strings.ToLower`
       - Returns the normalized string
       This is exported so the adapter can normalize once per message.

    3. `func Tokenize(s string) []string`:
       - Split on whitespace using `strings.Fields`
       - Strip leading/trailing punctuation from each token (runes in `unicode.Punct`)
       - Drop empty tokens
       - Returns the cleaned token slice

    4. `func Match(text string, matchers []config.ResolvedMatcher) *Result`:
       - Tokenize the already-normalized text
       - For each matcher, for each token, for each keyword in matcher.Words:
         - Normalize the keyword (NFC + lowercase) — do this once per keyword, not per token.
           Better: pre-normalize keywords at the top of Match() into a local slice.
         - Skip if token length < min-length for matcher.Distance (defense in depth, MATCH-05):
           distance 1 → token must be ≥5 runes; distance 2 → ≥8 runes
         - Compute `agnivade/levenshtein.ComputeDistance(token, keyword)`
         - If distance ≤ matcher.Distance → match found
       - Return the FIRST match found (stop on first hit — one reply per message)
       - Return nil if no match

    Import `golang.org/x/text/unicode/norm`. Run `go get golang.org/x/text@latest` if not already present.

    AVOID: Matching against the entire concatenated message body (must tokenize first per MATCH-03).
    AVOID: Using `strings.EqualFold` for distance-0 — use Levenshtein with distance 0 for uniformity.
    AVOID: Re-normalizing keywords on every call — normalize once at the top of Match().
  </action>
  <verify>go build ./internal/matcher/... succeeds</verify>
  <done>matcher.Match() tokenizes, normalizes, and fuzzy-matches per-token with Levenshtein</done>
</task>

<task type="auto">
  <name>Write comprehensive matcher tests</name>
  <files>internal/matcher/matcher_test.go</files>
  <action>
    Create table-driven tests using testify/assert:

    1. TestNormalize:
       - NFC input stays the same
       - NFD input (café with combining accent) normalizes to NFC
       - Uppercase converts to lowercase
       - Mixed case + accent

    2. TestTokenize:
       - Simple whitespace split
       - Strips punctuation: "hello!" → "hello"
       - Multiple spaces/tabs
       - Empty string → empty slice
       - Emoji-only tokens are kept (not stripped)

    3. TestMatch (table-driven, one sub-test per case):
       - Exact match (distance 0): word "sefaz" matches token "sefaz"
       - Fuzzy match (distance 1): word "SEFAZ" matches token "sefas" (1 substitution)
       - Fuzzy match (distance 1): word "SEFAZ" matches token "sefazz" (1 insertion)
       - No match: distance too far: word "SEFAZ" vs token "abcde" (distance > 1)
       - Short word rejection: word "RJ" with distance 1 → no match (< 5 runes)
       - NFC/NFD equivalence: keyword "café" matches NFD token "café" (NFC and NFD normalize to same)
       - Multiple matchers: returns the FIRST matching one
       - No matchers configured: returns nil
       - Empty text: returns nil
       - Distance 0 exact match only: "sefaz" matches "sefaz" but not "sefas"

    AVOID: Mocking the Levenshtein function — test real behavior.
    AVOID: Tests that only check for nil/non-nil without verifying which matcher fired.
  </action>
  <verify>go test -v ./internal/matcher/... — all tests pass</verify>
  <done>≥10 test cases covering exact, fuzzy, rejection, normalization, multi-matcher, edge cases</done>
</task>

</tasks>

<verification>
After all tasks, verify:
- [ ] `go test -v ./internal/matcher/...` — all tests pass
- [ ] `go vet ./internal/matcher/...` — no issues
- [ ] Short-word rejection is tested (distance 1 + 2-char word → no match)
- [ ] NFC/NFD normalization is tested
</verification>

<success_criteria>
- [ ] All tasks verified
- [ ] Matcher correctly fuzzy-matches per-token with Levenshtein
- [ ] ≥10 test cases pass
</success_criteria>
