// Package redact implements the redaction/restoration engine: it scans
// request bodies for sensitive substrings, replaces them with stable
// placeholder tokens, and later restores those placeholders in response
// bodies using an in-memory mapping store.
package redact

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"llmguard/internal/redact/detectors"
)

var placeholderRe = regexp.MustCompile(placeholderOpen + `([0-9a-f]{8})` + placeholderClose)

// ContextDetector is an optional interface a Detector can implement to
// receive a context with a deadline. The Redactor uses this to bound the
// total time spent in slow (e.g. LLM-backed) detectors across a single
// Redact call, regardless of how many string fields are scanned.
type ContextDetector interface {
	DetectWithContext(ctx context.Context, text string) []detectors.Match
}

// Redactor scans and rewrites request/response bodies using a set of
// detectors backed by a shared mapping Store.
type Redactor struct {
	detectors []detectors.Detector
	store     *Store
	llmBudget time.Duration
}

// New creates a Redactor backed by store, applying the given detectors in
// order. llmBudget bounds the total time (across an entire Redact call)
// available to detectors implementing ContextDetector; pass 0 if no such
// detectors are configured.
func New(store *Store, llmBudget time.Duration, dets ...detectors.Detector) *Redactor {
	return &Redactor{detectors: dets, store: store, llmBudget: llmBudget}
}

// Redact scans body for sensitive substrings and returns the rewritten body
// along with the list of categories that were matched (for logging). If body
// is valid JSON, every string value is scanned recursively; otherwise the raw
// bytes are scanned as text.
func (r *Redactor) Redact(body []byte) ([]byte, []string) {
	ctx := context.Background()
	if r.llmBudget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.llmBudget)
		defer cancel()
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()

	var data any
	if err := dec.Decode(&data); err != nil {
		redacted, cats := r.redactString(ctx, string(body), false)
		return []byte(redacted), cats
	}

	var categories []string
	walked := r.walk(ctx, data, &categories, false)

	out, err := json.Marshal(walked)
	if err != nil {
		return body, categories
	}
	return out, categories
}

// Restore replaces any placeholder tokens in data with the original values
// recorded during Redact. Unknown placeholders are left untouched. Safe to
// call repeatedly on successive chunks of a streamed response, provided each
// chunk contains complete placeholder tokens (see PlaceholderMaxLen).
func (r *Redactor) Restore(data []byte) []byte {
	return placeholderRe.ReplaceAllFunc(data, func(match []byte) []byte {
		sub := placeholderRe.FindSubmatch(match)
		if val, ok := r.store.Lookup(string(sub[1])); ok {
			return []byte(val)
		}
		return match
	})
}

// protocolKeys are JSON object keys whose values are API protocol/schema
// fields (model identifiers, tool names, type discriminators, IDs, ...)
// rather than free-form content. These never contain user-supplied text, and
// the upstream API requires them verbatim — e.g. rewriting "model" produces
// an unrecognized model (404), and rewriting "tools[].custom.name" with a
// placeholder token violates Anthropic's `^[a-zA-Z0-9_-]{1,128}$` pattern
// (400). They're skipped entirely (no redaction, no recursion).
var protocolKeys = map[string]bool{
	"model":         true,
	"name":          true,
	"type":          true,
	"role":          true,
	"id":            true,
	"tool_use_id":   true,
	"stop_reason":   true,
	"stop_sequence": true,
	"cache_control": true,
	"tools":         true,
	"tool_choice":   true,
}

// llmSkipKeys are fields whose content should be scanned by regex detectors
// but NOT by slow LLM-backed detectors. "system" is the AI provider's system
// prompt — typically static infrastructure text (tool descriptions, CLAUDE.md
// injections) rather than user-supplied content, so regex is sufficient and
// the LLM pass would add seconds of latency scanning thousands of tokens.
var llmSkipKeys = map[string]bool{
	"system": true,
}

// walk recursively visits every value in v. skipLLM suppresses
// ContextDetector (LLM-backed) detectors for the current subtree while still
// applying fast regex detectors — used for the "system" prompt field.
func (r *Redactor) walk(ctx context.Context, v any, categories *[]string, skipLLM bool) any {
	switch val := v.(type) {
	case string:
		redacted, cats := r.redactString(ctx, val, skipLLM)
		*categories = append(*categories, cats...)
		return redacted
	case map[string]any:
		for k, vv := range val {
			if protocolKeys[k] {
				continue
			}
			val[k] = r.walk(ctx, vv, categories, skipLLM || llmSkipKeys[k])
		}
		return val
	case []any:
		for i, vv := range val {
			val[i] = r.walk(ctx, vv, categories, skipLLM)
		}
		return val
	default:
		return v
	}
}

// redactString runs all detectors against s and replaces every detected
// substring with a placeholder token, returning the rewritten string and the
// categories that matched. Overlapping matches are resolved by preferring
// the earliest, longest match.
func (r *Redactor) redactString(ctx context.Context, s string, skipLLM bool) (string, []string) {
	var all []detectors.Match
	for _, det := range r.detectors {
		if cd, ok := det.(ContextDetector); ok {
			if skipLLM {
				continue
			}
			all = append(all, cd.DetectWithContext(ctx, s)...)
			continue
		}
		all = append(all, det.Detect(s)...)
	}
	if len(all) == 0 {
		return s, nil
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Start != all[j].Start {
			return all[i].Start < all[j].Start
		}
		return all[i].End > all[j].End
	})

	var b strings.Builder
	var categories []string
	last := 0
	for _, m := range all {
		if m.Start < last {
			continue // overlaps a previously-handled match
		}
		b.WriteString(s[last:m.Start])
		b.WriteString(r.store.PlaceholderFor(m.Value))
		categories = append(categories, m.Category)
		last = m.End
	}
	b.WriteString(s[last:])
	return b.String(), categories
}
