package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"llmguard/internal/redact"
	"llmguard/internal/redact/detectors"
)

func newTestRedactor(t *testing.T) *redact.Redactor {
	t.Helper()
	d, err := detectors.NewRegexDetector([]string{"aws_access_key"}, nil)
	if err != nil {
		t.Fatalf("NewRegexDetector: %v", err)
	}
	return redact.New(redact.NewStore(), 0, redact.RedactorOptions{}, d)
}

// TestProxy_RedactsRequestAndRestoresResponse verifies that a secret in the
// request body is replaced with a placeholder before reaching the upstream,
// and that if the upstream echoes that placeholder back, the client receives
// the real secret again.
func TestProxy_RedactsRequestAndRestoresResponse(t *testing.T) {
	const secret = "AKIAIOSFODNN7EXAMPLE"

	var receivedBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)

		echo, _ := json.Marshal(receivedBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"echo":%s}`, echo)
	}))
	defer upstream.Close()

	redactor := newTestRedactor(t)
	p, err := New(upstream.URL, redactor, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	front := httptest.NewServer(p)
	defer front.Close()

	reqBody := fmt.Sprintf(`{"text":"my key is %s"}`, secret)
	resp, err := http.Post(front.URL+"/v1/chat", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if strings.Contains(receivedBody, secret) {
		t.Errorf("upstream received unredacted secret: %s", receivedBody)
	}
	if !strings.Contains(receivedBody, "⟦RG:") {
		t.Errorf("upstream body missing placeholder token: %s", receivedBody)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}
	if !strings.Contains(string(respBody), secret) {
		t.Errorf("client response missing restored secret: %s", respBody)
	}
	if strings.Contains(string(respBody), "⟦RG:") {
		t.Errorf("client response still contains placeholder: %s", respBody)
	}
}

// TestProxy_RestoresSSEStream verifies that a placeholder token streamed back
// from the upstream over text/event-stream, split across multiple flushed
// writes, is restored to the original secret in the client response.
func TestProxy_RestoresSSEStream(t *testing.T) {
	const secret = "AKIAIOSFODNN7EXAMPLE"
	placeholderRe := regexp.MustCompile(`⟦RG:[0-9a-f]{8}⟧`)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		placeholder := placeholderRe.FindString(string(body))
		if placeholder == "" {
			http.Error(w, "no placeholder in request", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("upstream ResponseWriter does not support flushing")
		}

		chunk := fmt.Sprintf("data: %s\n\n", placeholder)
		mid := len(chunk) / 2
		fmt.Fprint(w, chunk[:mid])
		flusher.Flush()
		fmt.Fprint(w, chunk[mid:])
		flusher.Flush()
	}))
	defer upstream.Close()

	redactor := newTestRedactor(t)
	p, err := New(upstream.URL, redactor, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	front := httptest.NewServer(p)
	defer front.Close()

	reqBody := fmt.Sprintf(`{"text":"my key is %s"}`, secret)
	resp, err := http.Post(front.URL+"/v1/chat", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response: %v", err)
	}

	want := fmt.Sprintf("data: %s\n\n", secret)
	if string(respBody) != want {
		t.Errorf("got %q, want %q", respBody, want)
	}
}
