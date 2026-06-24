package llamacpp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func BenchmarkDetector_Detect(b *testing.B) {
	d, _ := newBenchDetector(b, `[]`)
	text := "this is a long enough piece of text to query for sensitive data"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Detect(text)
	}
}

func BenchmarkDetector_DetectBatch(b *testing.B) {
	d, _ := newBenchDetector(b, `[["Project Bluefin"],[]]`)
	texts := []string{
		"The codename for our launch is Project Bluefin.",
		"another long enough piece of text without secrets here",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.DetectBatchWithContext(context.Background(), texts)
	}
}

func newBenchDetector(b *testing.B, content string) (*Detector, *atomic.Int32) {
	b.Helper()
	var calls atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"content": content})
	}))
	b.Cleanup(ts.Close)
	return NewDetector(&Server{BaseURL: ts.URL}, 8, 2000, 3*time.Second), &calls
}

func TestDetector_DetectBatch(t *testing.T) {
	d, calls := newTestDetector(t, `[["Project Bluefin"],[]]`)

	texts := []string{
		"The codename for our launch is Project Bluefin.",
		"this is a long enough piece of text to query",
	}
	results := d.DetectBatchWithContext(context.Background(), texts)
	if len(results) != 2 {
		t.Fatalf("expected 2 result slices, got %d", len(results))
	}
	if len(results[0]) != 1 || results[0][0].Value != "Project Bluefin" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}
	if len(results[1]) != 0 {
		t.Fatalf("expected empty second result, got %+v", results[1])
	}
	if *calls != 1 {
		t.Errorf("expected 1 HTTP call for batch, got %d", *calls)
	}
}
