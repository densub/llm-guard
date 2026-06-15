package proxy

import (
	"io"
	"net/http"

	"llmguard/internal/redact"
)

// RestoringWriter wraps an io.Writer (typically an http.ResponseWriter),
// restoring placeholder tokens as data streams through it while buffering
// the minimum number of bytes needed to avoid splitting a token across
// successive Write calls.
type RestoringWriter struct {
	w        io.Writer
	flusher  http.Flusher
	redactor *redact.Redactor
	buf      []byte
}

// NewRestoringWriter wraps w. If w implements http.Flusher, each restored
// chunk is flushed immediately so streaming responses remain responsive.
func NewRestoringWriter(w io.Writer, redactor *redact.Redactor) *RestoringWriter {
	f, _ := w.(http.Flusher)
	return &RestoringWriter{w: w, flusher: f, redactor: redactor}
}

// Write implements io.Writer.
func (rw *RestoringWriter) Write(p []byte) (int, error) {
	rw.buf = append(rw.buf, p...)

	cut := redact.SafeCutPoint(rw.buf)
	if cut > 0 {
		restored := rw.redactor.Restore(rw.buf[:cut])
		if _, err := rw.w.Write(restored); err != nil {
			return 0, err
		}
		rw.buf = append([]byte(nil), rw.buf[cut:]...)
		if rw.flusher != nil {
			rw.flusher.Flush()
		}
	}
	return len(p), nil
}

// Close restores and flushes any remaining buffered bytes. It must be called
// once after the source is fully copied.
func (rw *RestoringWriter) Close() error {
	if len(rw.buf) > 0 {
		restored := rw.redactor.Restore(rw.buf)
		if _, err := rw.w.Write(restored); err != nil {
			return err
		}
		rw.buf = nil
	}
	if rw.flusher != nil {
		rw.flusher.Flush()
	}
	return nil
}
