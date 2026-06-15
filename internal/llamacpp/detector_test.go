package llamacpp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newTestDetector spins up a fake llama-server /completion endpoint that
// always returns content, and returns a Detector pointed at it plus a
// counter of how many requests it received.
func newTestDetector(t *testing.T, content string) (*Detector, *int) {
	t.Helper()
	calls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"content": content})
	}))
	t.Cleanup(ts.Close)

	d := NewDetector(&Server{BaseURL: ts.URL}, 8, 2000, 3*time.Second)
	return d, &calls
}

func TestDetector_HallucinationGuard(t *testing.T) {
	// "Acme Corp" is not present verbatim in the text and must be discarded.
	d, _ := newTestDetector(t, `["Project Bluefin", "Acme Corp"]`)

	text := "The codename for our launch is Project Bluefin."
	matches := d.Detect(text)

	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d: %+v", len(matches), matches)
	}
	m := matches[0]
	if m.Value != "Project Bluefin" {
		t.Errorf("unexpected match value %q", m.Value)
	}
	if text[m.Start:m.End] != "Project Bluefin" {
		t.Errorf("Start/End offsets don't match: %q", text[m.Start:m.End])
	}
	if m.Category != Category {
		t.Errorf("unexpected category %q", m.Category)
	}
}

func TestDetector_NoMatches(t *testing.T) {
	d, _ := newTestDetector(t, `[]`)
	matches := d.Detect("hello world, this is a longer piece of text for testing.")
	if matches != nil {
		t.Errorf("expected nil matches, got %+v", matches)
	}
}

func TestDetector_SkipsShortText(t *testing.T) {
	d, calls := newTestDetector(t, `["x"]`)

	matches := d.Detect("short")
	if matches != nil {
		t.Errorf("expected nil matches for short text, got %+v", matches)
	}
	if *calls != 0 {
		t.Errorf("expected no HTTP call for text shorter than minLen, got %d calls", *calls)
	}
}

func TestDetector_SkipsLongText(t *testing.T) {
	d, calls := newTestDetector(t, `["x"]`)
	d.maxLen = 10

	matches := d.Detect("this text is definitely longer than ten bytes")
	if matches != nil {
		t.Errorf("expected nil matches for long text, got %+v", matches)
	}
	if *calls != 0 {
		t.Errorf("expected no HTTP call for text longer than maxLen, got %d calls", *calls)
	}
}

func TestDetector_MultipleOccurrences(t *testing.T) {
	d, _ := newTestDetector(t, `["alice@example.com"]`)

	text := "contact alice@example.com or alice@example.com again"
	matches := d.Detect(text)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %+v", len(matches), matches)
	}
}

func TestDetector_ServerUnreachable(t *testing.T) {
	d := NewDetector(&Server{BaseURL: "http://127.0.0.1:1"}, 8, 2000, 500*time.Millisecond)

	matches := d.Detect("this is a long enough piece of text to query")
	if matches != nil {
		t.Errorf("expected nil matches when server is unreachable, got %+v", matches)
	}
}

func TestDetector_MalformedJSON(t *testing.T) {
	d, _ := newTestDetector(t, `not valid json`)

	matches := d.Detect("this is a long enough piece of text to query")
	if matches != nil {
		t.Errorf("expected nil matches for malformed content, got %+v", matches)
	}
}
