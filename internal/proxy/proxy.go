// Package proxy implements the local HTTP proxy: it redacts sensitive data
// from outgoing requests, forwards them to the configured upstream, and
// restores placeholders in the response before returning it to the caller.
package proxy

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"llmguard/internal/redact"
)

// Proxy forwards requests to a single upstream base URL, redacting request
// bodies and restoring response bodies along the way.
type Proxy struct {
	upstream *url.URL
	client   *http.Client
	redactor *redact.Redactor
	logger   *log.Logger
}

// New creates a Proxy that forwards to upstream (must include scheme and
// host, e.g. "https://api.anthropic.com"). logger may be nil to disable
// redaction logging.
func New(upstream string, redactor *redact.Redactor, logger *log.Logger) (*Proxy, error) {
	u, err := url.Parse(upstream)
	if err != nil {
		return nil, fmt.Errorf("parsing upstream URL %q: %w", upstream, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("upstream URL %q must include a scheme and host", upstream)
	}
	return &Proxy{
		upstream: u,
		client:   &http.Client{},
		redactor: redactor,
		logger:   logger,
	}, nil
}

// ServeHTTP implements http.Handler.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadGateway)
		return
	}
	r.Body.Close()

	redactedBody, categories := p.redactor.RedactForProxy(body)

	target := *p.upstream
	target.Path = singleJoiningSlash(p.upstream.Path, r.URL.Path)
	target.RawQuery = r.URL.RawQuery

	outReq, err := http.NewRequest(r.Method, target.String(), bytes.NewReader(redactedBody))
	if err != nil {
		http.Error(w, "failed to build upstream request", http.StatusBadGateway)
		return
	}
	outReq.Header = r.Header.Clone()
	outReq.Header.Del("Connection")
	// Let net/http negotiate and transparently decompress the response
	// itself. If we forward the client's Accept-Encoding verbatim, Go's
	// transport assumes *we* will handle decoding and leaves the body
	// gzip-compressed, which breaks placeholder restoration (it operates on
	// the raw bytes) for any compressed response.
	outReq.Header.Del("Accept-Encoding")
	outReq.Host = p.upstream.Host
	outReq.ContentLength = int64(len(redactedBody))
	outReq.Header.Set("Content-Length", strconv.Itoa(len(redactedBody)))

	resp, err := p.client.Do(outReq)
	if err != nil {
		http.Error(w, "upstream request failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		if k == "Content-Length" || k == "Transfer-Encoding" {
			continue
		}
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	// Restoration can change the body length, so let the server choose the
	// transfer encoding rather than trusting upstream's Content-Length.
	w.Header().Del("Content-Length")
	w.WriteHeader(resp.StatusCode)

	ct := resp.Header.Get("Content-Type")
	if strings.Contains(ct, "text/event-stream") || resp.Header.Get("Transfer-Encoding") == "chunked" {
		rw := NewRestoringWriter(w, p.redactor)
		_, _ = io.Copy(rw, resp.Body)
		_ = rw.Close()
	} else {
		respBody, _ := io.ReadAll(resp.Body)
		_, _ = w.Write(p.redactor.Restore(respBody))
		// Error response bodies are generic API error messages (no user
		// secrets), so logging them helps diagnose upstream rejections.
		if resp.StatusCode >= 400 {
			p.logErrorBody(resp.StatusCode, resp.Header, respBody)
		}
	}

	p.logRequest(r.URL.Path, resp.StatusCode, categories)
}

func (p *Proxy) logErrorBody(status int, header http.Header, body []byte) {
	if p.logger == nil {
		return
	}
	const maxLen = 2000
	b := body
	if len(b) > maxLen {
		b = b[:maxLen]
	}
	var rl []string
	for k, vv := range header {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "anthropic-ratelimit") || lk == "retry-after" || lk == "request-id" {
			rl = append(rl, k+"="+strings.Join(vv, ","))
		}
	}
	sort.Strings(rl)
	p.logger.Printf("upstream error status=%d %s body=%s", status, strings.Join(rl, " "), string(b))
}

func (p *Proxy) logRequest(path string, status int, categories []string) {
	if p.logger == nil {
		return
	}
	if len(categories) == 0 {
		p.logger.Printf("status=%d redacted=0 path=%s", status, path)
		return
	}
	p.logger.Printf("status=%d redacted=%d categories=%s path=%s", status, len(categories), strings.Join(uniqueSorted(categories), ","), path)
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

func uniqueSorted(items []string) []string {
	seen := make(map[string]bool, len(items))
	out := make([]string, 0, len(items))
	for _, it := range items {
		if !seen[it] {
			seen[it] = true
			out = append(out, it)
		}
	}
	sort.Strings(out)
	return out
}
