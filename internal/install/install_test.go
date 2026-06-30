package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseChoices(t *testing.T) {
	tests := []struct {
		in   string
		want []int
	}{
		{"1", []int{1}},
		{"1,2", []int{1, 2}},
		{"1, 2, 3", []int{1, 2, 3}},
		{"1/2", []int{1, 2}},
		{"1,1,2", []int{1, 2}},
		{"", nil},
		{"  ", nil},
	}
	for _, tt := range tests {
		got := parseChoices(tt.in)
		if len(got) != len(tt.want) {
			t.Fatalf("parseChoices(%q) = %v, want %v", tt.in, got, tt.want)
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Fatalf("parseChoices(%q) = %v, want %v", tt.in, got, tt.want)
			}
		}
	}
}

func TestNormalizeUpstream(t *testing.T) {
	got, err := normalizeUpstream("openai")
	if err != nil || got != "https://api.openai.com" {
		t.Fatalf("openai: got %q, err %v", got, err)
	}
	got, err = normalizeUpstream("anthropic")
	if err != nil || got != "https://api.anthropic.com" {
		t.Fatalf("anthropic: got %q, err %v", got, err)
	}
	got, err = normalizeUpstream("https://api.example.com")
	if err != nil || got != "https://api.example.com" {
		t.Fatalf("custom: got %q, err %v", got, err)
	}
}

func TestResolveUpstreamSingleAgent(t *testing.T) {
	up, err := resolveUpstream([]Agent{AgentClaude}, "", strings.NewReader(""))
	if err != nil || up != "https://api.anthropic.com" {
		t.Fatalf("claude: got %q, err %v", up, err)
	}
	up, err = resolveUpstream([]Agent{AgentOpenAI}, "", strings.NewReader(""))
	if err != nil || up != "https://api.openai.com" {
		t.Fatalf("openai: got %q, err %v", up, err)
	}
	up, err = resolveUpstream([]Agent{AgentCursor}, "", strings.NewReader(""))
	if err != nil || up != "https://api.openai.com" {
		t.Fatalf("cursor: got %q, err %v", up, err)
	}
}

func TestAgentsFromLabels(t *testing.T) {
	agents, err := agentsFromLabels([]string{labelClaude, labelOpenAI})
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 || agents[0] != AgentClaude || agents[1] != AgentOpenAI {
		t.Fatalf("unexpected agents: %v", agents)
	}
	_, err = agentsFromLabels(nil)
	if err == nil {
		t.Fatal("expected error for empty selection")
	}
}

func TestMergeClaudeSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"alwaysThinkingEnabled":true}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := mergeJSONEnv(path, map[string]string{"ANTHROPIC_BASE_URL": "http://127.0.0.1:8317"}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	var env map[string]string
	if err := json.Unmarshal(settings["env"], &env); err != nil {
		t.Fatal(err)
	}
	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:8317" {
		t.Fatalf("unexpected env: %v", env)
	}
	if _, ok := settings["alwaysThinkingEnabled"]; !ok {
		t.Fatalf("lost alwaysThinkingEnabled: %s", data)
	}
}

func TestConfigureAgentSettingsClaude(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := configureAgentSettings("127.0.0.1:8317", []Agent{AgentClaude}); err != nil {
		t.Fatal(err)
	}
	path, err := claudeSettingsPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ANTHROPIC_BASE_URL") {
		t.Fatalf("missing ANTHROPIC_BASE_URL: %s", data)
	}
}

func TestWriteShellProfileBlock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/zsh")

	if err := writeShellProfile("127.0.0.1:8317", []Agent{AgentOpenAI, AgentClaude}); err != nil {
		t.Fatal(err)
	}
	if err := writeShellProfile("127.0.0.1:8317", []Agent{AgentOpenAI, AgentClaude, AgentCursor}); err != nil {
		t.Fatal(err)
	}

	profile, err := shellProfilePath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, profileBegin) || !strings.Contains(content, profileEnd) {
		t.Fatalf("missing markers: %s", content)
	}
	if strings.Count(content, profileBegin) != 1 {
		t.Fatalf("expected one block, got:\n%s", content)
	}
	if !strings.Contains(content, `OPENAI_BASE_URL="http://127.0.0.1:8317/v1"`) {
		t.Fatalf("missing OPENAI_BASE_URL: %s", content)
	}
	if !strings.Contains(content, `ANTHROPIC_BASE_URL="http://127.0.0.1:8317"`) {
		t.Fatalf("missing ANTHROPIC_BASE_URL: %s", content)
	}
}
