// Package detectors provides pluggable sensitive-data detectors used by the
// redaction engine. The built-in RegexDetector covers common secret formats;
// additional detectors (e.g. a local LLM-based semantic detector) can
// implement the same Detector interface.
package detectors

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Match represents a single detected sensitive substring within a piece of text.
type Match struct {
	Category string
	Value    string
	Start    int
	End      int
}

// Detector finds sensitive substrings within a block of text.
type Detector interface {
	Name() string
	Detect(text string) []Match
}

// CustomPattern is a user-supplied regex pattern loaded from config.
type CustomPattern struct {
	Name    string `yaml:"name"`
	Pattern string `yaml:"pattern"`
}

// builtinPatterns maps a category name to the regex used to detect it.
// Patterns are intentionally conservative gitleaks-style signatures for
// common credential formats.
var builtinPatterns = map[string]string{
	"aws_access_key":             `AKIA[0-9A-Z]{16}`,
	"aws_secret_key":             `(?i)aws_secret_access_key\s*[=:]\s*['"]?[A-Za-z0-9/+=]{40}['"]?`,
	"gcp_api_key":                `AIza[0-9A-Za-z\-_]{35}`,
	"github_token":               `gh[pousr]_[A-Za-z0-9]{36,255}`,
	"gitlab_token":               `glpat-[A-Za-z0-9\-_]{20}`,
	"slack_token":                `xox[baprs]-[A-Za-z0-9-]{10,}`,
	"stripe_key":                 `sk_live_[0-9a-zA-Z]{24,}`,
	"openai_key":                 `sk-[A-Za-z0-9]{20,}`,
	"anthropic_key":              `sk-ant-[A-Za-z0-9_-]{20,}`,
	"private_key_block":          `-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z0-9 ]*PRIVATE KEY-----`,
	"jwt":                        `eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`,
	"generic_api_key_assignment": `(?i)(api[_-]?key|secret|token|password|passwd|pwd)\s*[=:]\s*['"]?[A-Za-z0-9_\-/+=]{8,}['"]?`,
	"email":                      `[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`,
	"ssn": `\b\d{3}[-\s]\d{2}[-\s]\d{4}\b`,
	"credit_card": `\b(?:4[0-9]{3}[-\s]?[0-9]{4}[-\s]?[0-9]{4}[-\s]?[0-9]{4}|5[1-5][0-9]{2}[-\s]?[0-9]{4}[-\s]?[0-9]{4}[-\s]?[0-9]{4}|3[47][0-9]{2}[-\s]?[0-9]{6}[-\s]?[0-9]{5}|6(?:011|5[0-9]{2})[-\s]?[0-9]{4}[-\s]?[0-9]{4}[-\s]?[0-9]{4})\b`,
	"phone_us":   `(?:\+?1[-.\s]?)?(?:\([2-9]\d{2}\)[-.\s]*|\b[2-9]\d{2}[-.\s]+)\d{3}[-.\s]+\d{4}\b`,
	"phone_intl": `\+[1-9]\d{6,14}\b`,
	"iban":       `\b[A-Z]{2}\d{2}[A-Z0-9]{11,30}\b`,
}

// BuiltinCategories returns the names of all available built-in categories.
func BuiltinCategories() []string {
	cats := make([]string, 0, len(builtinPatterns))
	for c := range builtinPatterns {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	return cats
}

type namedPattern struct {
	category string
	re       *regexp.Regexp
	triggers []string
}

// builtinTriggers lists cheap literal substrings that must appear for a pattern
// to possibly match. Patterns with no triggers always run.
var builtinTriggers = map[string][]string{
	"aws_access_key":             {"AKIA"},
	"aws_secret_key":             {"aws_secret"},
	"gcp_api_key":                {"AIza"},
	"github_token":               {"ghp_", "gho_", "ghu_", "ghs_", "ghr_"},
	"gitlab_token":               {"glpat-"},
	"slack_token":                {"xox"},
	"stripe_key":                 {"sk_live_"},
	"openai_key":                 {"sk-"},
	"anthropic_key":              {"sk-ant-"},
	"private_key_block":          {"-----BEGIN"},
	"jwt":                        {"eyJ"},
	"generic_api_key_assignment": {"api", "key", "secret", "token", "password", "passwd", "pwd"},
	"email":                      {"@"},
	"ssn":                        {"-"},
	"credit_card":                {},
	"phone_us":                   {"(", "+1"},
	"phone_intl":                 {"+"},
	"iban":                       {},
}

// categories requiring post-regex validation before accepting a match.
var postValidators = map[string]func(string) bool{
	"credit_card": luhnValid,
	"ssn":         ssnValid,
}

// RegexDetector applies a configured set of built-in and custom regex
// patterns to a block of text.
type RegexDetector struct {
	patterns []namedPattern
}

// NewRegexDetector builds a RegexDetector from the requested built-in
// category names plus any custom patterns. Unknown category names or
// invalid custom patterns produce an error.
func NewRegexDetector(categories []string, custom []CustomPattern) (*RegexDetector, error) {
	d := &RegexDetector{}

	for _, cat := range categories {
		pat, ok := builtinPatterns[cat]
		if !ok {
			return nil, fmt.Errorf("unknown regex detector category %q", cat)
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("compiling builtin pattern %q: %w", cat, err)
		}
		d.patterns = append(d.patterns, namedPattern{
			category: cat,
			re:       re,
			triggers: builtinTriggers[cat],
		})
	}

	for _, c := range custom {
		re, err := regexp.Compile(c.Pattern)
		if err != nil {
			return nil, fmt.Errorf("compiling custom pattern %q: %w", c.Name, err)
		}
		d.patterns = append(d.patterns, namedPattern{
			category: c.Name,
			re:       re,
			triggers: extractTriggers(c.Pattern),
		})
	}

	return d, nil
}

// Name implements Detector.
func (d *RegexDetector) Name() string { return "regex" }

// Detect implements Detector. Overlapping matches across different patterns
// are all reported; the caller (Redactor) is responsible for resolving
// overlaps when substituting placeholders.
func (d *RegexDetector) Detect(text string) []Match {
	var matches []Match
	for _, p := range d.patterns {
		if !triggersMatch(text, p.triggers) {
			continue
		}
		validate := postValidators[p.category]
		for _, loc := range p.re.FindAllStringIndex(text, -1) {
			value := text[loc[0]:loc[1]]
			if validate != nil && !validate(value) {
				continue
			}
			matches = append(matches, Match{
				Category: p.category,
				Value:    value,
				Start:    loc[0],
				End:      loc[1],
			})
		}
	}
	return matches
}

func triggersMatch(text string, triggers []string) bool {
	if len(triggers) == 0 {
		return true
	}
	lower := strings.ToLower(text)
	for _, trig := range triggers {
		if strings.Contains(lower, strings.ToLower(trig)) {
			return true
		}
	}
	return false
}

// extractTriggers pulls a few literal runs from a regex pattern for fast-path
// filtering. Custom patterns without literals run unconditionally.
func extractTriggers(pattern string) []string {
	var literals []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() >= 3 {
			literals = append(literals, cur.String())
		}
		cur.Reset()
	}
	for _, r := range pattern {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-' || r == '_':
			cur.WriteRune(r)
		default:
			flush()
		}
	}
	flush()
	return literals
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// luhnValid reports whether s contains a credit-card number that passes the
// Luhn checksum (after stripping separators).
func luhnValid(s string) bool {
	digits := digitsOnly(s)
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	sum := 0
	alt := false
	for i := len(digits) - 1; i >= 0; i-- {
		n := int(digits[i] - '0')
		if alt {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		alt = !alt
	}
	return sum%10 == 0
}

// ssnValid rejects obviously invalid US Social Security numbers.
func ssnValid(s string) bool {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '-' || r == ' ' || r == '\t'
	})
	if len(parts) != 3 || len(parts[0]) != 3 || len(parts[1]) != 2 || len(parts[2]) != 4 {
		return false
	}
	area, group, serial := parts[0], parts[1], parts[2]
	if area == "000" || area == "666" || area[0] == '9' {
		return false
	}
	if group == "00" || serial == "0000" {
		return false
	}
	return true
}
