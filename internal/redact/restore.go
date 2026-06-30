package redact

import (
	"bytes"
	"encoding/json"
)

// RestoreResponse replaces placeholder tokens in an upstream response body.
// JSON and SSE payloads are parsed so restored values are re-encoded with
// proper escaping; other bodies use byte-level restoration.
func (r *Redactor) RestoreResponse(body []byte, contentType string) []byte {
	if isSSEContentType(contentType) {
		return r.restoreSSE(body)
	}
	return r.restoreJSONOrRaw(body)
}

func isSSEContentType(contentType string) bool {
	return bytes.Contains([]byte(contentType), []byte("text/event-stream"))
}

func (r *Redactor) restoreJSONOrRaw(body []byte) []byte {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return r.Restore(body)
	}

	var data any
	if err := json.Unmarshal(trimmed, &data); err != nil {
		return r.Restore(body)
	}

	walked := r.walkRestoreStrings(data)
	out, err := json.Marshal(walked)
	if err != nil {
		return r.Restore(body)
	}
	return out
}

func (r *Redactor) walkRestoreStrings(v any) any {
	switch val := v.(type) {
	case string:
		return r.restoreString(val)
	case map[string]any:
		for k, vv := range val {
			val[k] = r.walkRestoreStrings(vv)
		}
		return val
	case []any:
		for i, vv := range val {
			val[i] = r.walkRestoreStrings(vv)
		}
		return val
	default:
		return v
	}
}

func (r *Redactor) restoreString(s string) string {
	return placeholderRe.ReplaceAllStringFunc(s, func(match string) string {
		openLen := len(placeholderOpen)
		closeLen := len(placeholderClose)
		if len(match) < openLen+closeLen {
			return match
		}
		hash := match[openLen : len(match)-closeLen]
		if val, ok := r.store.Lookup(hash); ok {
			return val
		}
		return match
	})
}

// RestoreSSEEvent restores placeholders inside a single SSE event block.
func (r *Redactor) RestoreSSEEvent(event []byte) []byte {
	lines := bytes.Split(event, []byte("\n"))
	for i, line := range lines {
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(line[5:])
		if len(payload) == 0 || (payload[0] != '{' && payload[0] != '[') {
			continue
		}
		lines[i] = append([]byte("data: "), r.restoreJSONOrRaw(payload)...)
	}
	return bytes.Join(lines, []byte("\n"))
}

func (r *Redactor) restoreSSE(body []byte) []byte {
	if !bytes.Contains(body, []byte("\n\n")) {
		return r.RestoreSSEEvent(body)
	}
	var out bytes.Buffer
	rest := body
	for {
		idx := bytes.Index(rest, []byte("\n\n"))
		if idx < 0 {
			out.Write(r.RestoreSSEEvent(rest))
			break
		}
		out.Write(r.RestoreSSEEvent(rest[:idx+2]))
		rest = rest[idx+2:]
	}
	return out.Bytes()
}
