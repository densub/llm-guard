// Package detectors provides pluggable sensitive-data detectors used by the
// redaction engine. The built-in RegexDetector covers common secret formats;
// additional detectors (e.g. a local LLM-based semantic detector) can
// implement the same Detector interface.
package detectors

import (
	"fmt"
	"regexp"
	"strings"
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
}

// BuiltinCategories returns the names of all available built-in categories.
func BuiltinCategories() []string {
	cats := make([]string, 0, len(builtinPatterns))
	for c := range builtinPatterns {
		cats = append(cats, c)
	}
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
		for _, loc := range p.re.FindAllStringIndex(text, -1) {
			matches = append(matches, Match{
				Category: p.category,
				Value:    text[loc[0]:loc[1]],
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
