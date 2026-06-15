package llamacpp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"llmguard/internal/redact/detectors"
)

// Category is the detectors.Match category used for spans flagged by the
// LLM fallback detector.
const Category = "llm_sensitive"

// Detector is a detectors.Detector (and redact.ContextDetector) backed by a
// running llama-server. It is a best-effort, additive pass: any error
// (timeout, malformed response, server unreachable) results in no matches
// rather than failing the request, so regex-based redaction is unaffected.
type Detector struct {
	server  *Server
	client  *http.Client
	minLen  int
	maxLen  int
	timeout time.Duration
}

// NewDetector returns a Detector that queries server. Strings shorter than
// minLen or longer than maxLen (in bytes) are skipped without contacting the
// server. timeout bounds a single /completion call.
func NewDetector(server *Server, minLen, maxLen int, timeout time.Duration) *Detector {
	return &Detector{
		server:  server,
		client:  &http.Client{},
		minLen:  minLen,
		maxLen:  maxLen,
		timeout: timeout,
	}
}

// Name implements detectors.Detector.
func (d *Detector) Name() string { return "llm" }

// Detect implements detectors.Detector.
func (d *Detector) Detect(text string) []detectors.Match {
	return d.DetectWithContext(context.Background(), text)
}

// completionRequest is the body sent to llama-server's /completion endpoint.
type completionRequest struct {
	Prompt      string  `json:"prompt"`
	Grammar     string  `json:"grammar"`
	NPredict    int     `json:"n_predict"`
	Temperature float64 `json:"temperature"`
}

type completionResponse struct {
	Content string `json:"content"`
}

// DetectWithContext implements redact.ContextDetector. It asks the local
// llama-server to identify sensitive substrings of text, verifies each
// returned candidate actually occurs verbatim in text (a hallucination
// guard), and returns a Match for every verified occurrence.
func (d *Detector) DetectWithContext(ctx context.Context, text string) []detectors.Match {
	if len(text) < d.minLen || len(text) > d.maxLen {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	reqBody, err := json.Marshal(completionRequest{
		Prompt:      buildPrompt(text),
		Grammar:     jsonStringArrayGrammar,
		NPredict:    256,
		Temperature: 0,
	})
	if err != nil {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.server.BaseURL+"/completion", bytes.NewReader(reqBody))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var result completionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}

	var candidates []string
	if err := json.Unmarshal([]byte(result.Content), &candidates); err != nil {
		return nil
	}

	var matches []detectors.Match
	for _, c := range candidates {
		if c == "" {
			continue
		}
		for _, start := range allIndexes(text, c) {
			matches = append(matches, detectors.Match{
				Category: Category,
				Value:    c,
				Start:    start,
				End:      start + len(c),
			})
		}
	}
	return matches
}

// allIndexes returns the start byte offsets of every non-overlapping
// occurrence of substr in s.
func allIndexes(s, substr string) []int {
	var idxs []int
	offset := 0
	for {
		i := strings.Index(s[offset:], substr)
		if i < 0 {
			return idxs
		}
		idxs = append(idxs, offset+i)
		offset += i + len(substr)
	}
}
