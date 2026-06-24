package redact

import (
	"encoding/json"
	"fmt"
	"strings"
)

// BenchFixtureChat20Msg returns a realistic 20-message chat JSON payload for benchmarks.
func BenchFixtureChat20Msg() []byte {
	msgs := make([]map[string]any, 20)
	for i := range msgs {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs[i] = map[string]any{
			"role":    role,
			"content": fmt.Sprintf("Message %d: please help me review this code snippet for correctness.", i),
		}
	}
	body, err := json.Marshal(map[string]any{
		"model":    "claude-sonnet-4-20250514",
		"messages": msgs,
	})
	if err != nil {
		panic(err)
	}
	return body
}

// benchFixtures holds realistic JSON payloads for redact package benchmarks.
var benchFixtures = struct {
	SmallChat   []byte
	Chat20Msg   []byte
	LargeSystem []byte
	NoMatch     []byte
	WithSecrets []byte
}{
	SmallChat:   benchMarshal(smallChatPayload(2)),
	Chat20Msg:   BenchFixtureChat20Msg(),
	LargeSystem: benchMarshal(largeSystemPayload()),
	NoMatch:     benchMarshal(noMatchPayload()),
	WithSecrets: benchMarshal(withSecretsPayload()),
}

func smallChatPayload(n int) map[string]any {
	msgs := make([]any, 0, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs = append(msgs, map[string]any{
			"role":    role,
			"content": fmt.Sprintf("Message %d: please help me review this code snippet for correctness.", i),
		})
	}
	return map[string]any{
		"model":    "claude-sonnet-4-20250514",
		"messages": msgs,
	}
}

func largeSystemPayload() map[string]any {
	var b strings.Builder
	b.WriteString("You are a helpful assistant. ")
	for i := 0; i < 200; i++ {
		b.WriteString("Follow these guidelines carefully. ")
	}
	return map[string]any{
		"model":  "claude-sonnet-4-20250514",
		"system": b.String(),
		"messages": []any{
			map[string]any{"role": "user", "content": "Summarize the guidelines."},
		},
	}
}

func noMatchPayload() map[string]any {
	return map[string]any{
		"model": "claude-sonnet-4-20250514",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello world"},
		},
	}
}

func withSecretsPayload() map[string]any {
	return map[string]any{
		"model": "claude-sonnet-4-20250514",
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "my key is AKIAIOSFODNN7EXAMPLE and email alice@example.com",
			},
		},
	}
}

func benchMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
