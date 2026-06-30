// Package install implements the interactive `llmguard install` flow.
package install

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"llmguard/internal/config"
)

// Agent identifies a supported AI client.
type Agent string

const (
	AgentOpenAI Agent = "openai"
	AgentClaude Agent = "claude"
	AgentCursor Agent = "cursor"
)

// Options configures a non-interactive or scripted install run.
type Options struct {
	Agents    []Agent
	Upstream  string // openai, anthropic, or full URL; empty = infer from agents
	SkipStart bool
	NoProfile bool
	Reader    io.Reader
	Writer    io.Writer
}

// Run performs the install flow: configure upstream, start the proxy, write
// shell exports, and print a ready summary.
func Run(opts Options) error {
	if opts.Reader == nil {
		opts.Reader = os.Stdin
	}
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}
	out := opts.Writer

	cfgPath, err := config.Path()
	if err != nil {
		return err
	}

	agents := opts.Agents
	if len(agents) == 0 {
		var err error
		agents, err = promptAgents(opts.Reader, out)
		if err != nil {
			return err
		}
	}

	upstream, err := resolveUpstream(agents, opts.Upstream, opts.Reader, out)
	if err != nil {
		return err
	}

	cfg := config.Default()
	if config.Exists(cfgPath) {
		existing, err := config.Load(cfgPath)
		if err != nil {
			return err
		}
		cfg = existing
	}
	cfg.Upstream = upstream

	if err := config.Save(cfgPath, cfg); err != nil {
		return err
	}

	if err := saveAgents(agents); err != nil {
		return err
	}

	if !opts.NoProfile {
		if err := writeShellProfile(cfg.Listen, agents); err != nil {
			fmt.Fprintf(out, "Warning: could not update shell profile: %v\n", err)
		}
	}

	if err := configureAgentSettings(cfg.Listen, agents); err != nil {
		fmt.Fprintf(out, "Warning: could not update agent settings: %v\n", err)
	}

	printAgentNotes(out, agents, cfg.Listen)

	if !opts.SkipStart {
		if err := runStartDetached(); err != nil {
			return err
		}
	}
	return nil
}

// EnvExports returns shell export statements for the configured agents and
// listen address. Used to apply settings in the current shell session.
func EnvExports(listen string, agents []Agent) []string {
	baseHTTP := "http://" + listen
	var lines []string
	if containsAgent(agents, AgentOpenAI) || containsAgent(agents, AgentCursor) {
		lines = append(lines, fmt.Sprintf("export OPENAI_BASE_URL=%q", baseHTTP+"/v1"))
	}
	if containsAgent(agents, AgentClaude) {
		lines = append(lines, fmt.Sprintf("export ANTHROPIC_BASE_URL=%q", baseHTTP))
	}
	return lines
}

// LoadSavedAgents reads the agent list written by a previous install run.
func LoadSavedAgents() ([]Agent, error) {
	path, err := agentsFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var agents []Agent
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		agents = append(agents, Agent(line))
	}
	return agents, nil
}

func saveAgents(agents []Agent) error {
	path, err := agentsFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	for _, a := range agents {
		b.WriteString(string(a))
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func agentsFilePath() (string, error) {
	stateDir, err := config.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(stateDir, "agents"), nil
}

func promptAgents(r io.Reader, w io.Writer) ([]Agent, error) {
	reader := bufio.NewReader(r)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Which AI agents do you use?")
	fmt.Fprintln(w, "  1) OpenAI / Codex CLI")
	fmt.Fprintln(w, "  2) Claude Code")
	fmt.Fprintln(w, "  3) Cursor IDE")
	fmt.Fprint(w, "Enter numbers separated by commas (e.g. 1,2): ")

	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("reading agent choice: %w", err)
	}

	choices := parseChoices(line)
	if len(choices) == 0 {
		return nil, fmt.Errorf("select at least one agent (1, 2, or 3)")
	}

	var agents []Agent
	for _, c := range choices {
		switch c {
		case 1:
			agents = append(agents, AgentOpenAI)
		case 2:
			agents = append(agents, AgentClaude)
		case 3:
			agents = append(agents, AgentCursor)
		default:
			return nil, fmt.Errorf("invalid choice %d (use 1, 2, or 3)", c)
		}
	}
	return agents, nil
}

func parseChoices(line string) []int {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	parts := strings.FieldsFunc(line, func(r rune) bool {
		return r == ',' || r == ' ' || r == '/'
	})
	var out []int
	seen := make(map[int]bool)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var n int
		switch p {
		case "1":
			n = 1
		case "2":
			n = 2
		case "3":
			n = 3
		default:
			continue
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

func resolveUpstream(agents []Agent, explicit string, r io.Reader, w io.Writer) (string, error) {
	if explicit != "" {
		return normalizeUpstream(explicit)
	}

	hasClaude := containsAgent(agents, AgentClaude)
	hasOpenAI := containsAgent(agents, AgentOpenAI) || containsAgent(agents, AgentCursor)

	switch {
	case hasClaude && !hasOpenAI:
		return "https://api.anthropic.com", nil
	case hasOpenAI && !hasClaude:
		return "https://api.openai.com", nil
	case hasClaude && hasOpenAI:
		reader := bufio.NewReader(r)
		fmt.Fprintln(w)
		fmt.Fprintln(w, "You selected agents that use different API providers.")
		fmt.Fprintln(w, "llm-guard proxies to one upstream at a time — which should it use?")
		fmt.Fprintln(w, "  1) OpenAI    (https://api.openai.com)")
		fmt.Fprintln(w, "  2) Anthropic (https://api.anthropic.com)")
		fmt.Fprint(w, "Choice [1-2]: ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("reading upstream choice: %w", err)
		}
		switch strings.TrimSpace(line) {
		case "1", "":
			return "https://api.openai.com", nil
		case "2":
			return "https://api.anthropic.com", nil
		default:
			return "", fmt.Errorf("invalid upstream choice")
		}
	default:
		return "https://api.openai.com", nil
	}
}

func normalizeUpstream(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "openai", "1":
		return "https://api.openai.com", nil
	case "anthropic", "claude", "2":
		return "https://api.anthropic.com", nil
	case "":
		return "", fmt.Errorf("empty upstream URL")
	default:
		if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
			return "", fmt.Errorf("upstream URL must start with http:// or https://")
		}
		return strings.TrimSpace(s), nil
	}
}

func containsAgent(agents []Agent, a Agent) bool {
	for _, x := range agents {
		if x == a {
			return true
		}
	}
	return false
}

func runStartDetached() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "restart", "--detach")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func printAgentNotes(w io.Writer, agents []Agent, listen string) {
	baseHTTP := "http://" + listen
	openAIURL := baseHTTP + "/v1"
	anthropicURL := baseHTTP

	const (
		reset = "\033[0m"
		bold  = "\033[1m"
		cyan  = "\033[36m"
		dim   = "\033[2m"
		green = "\033[32m"
	)

	useColor := isTerminal(w)
	c := func(code, s string) string {
		if !useColor {
			return s
		}
		return code + s + reset
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, c(bold, "  Agent setup saved:"))
	fmt.Fprintln(w)

	if containsAgent(agents, AgentClaude) {
		fmt.Fprintln(w, c(bold, "  Claude Code:"))
		if path, err := claudeSettingsPath(); err == nil {
			fmt.Fprintf(w, "    %s\n", c(green, "Configured "+path))
		}
		fmt.Fprintf(w, "    %s\n", c(cyan, fmt.Sprintf("ANTHROPIC_BASE_URL=%q", anthropicURL)))
		fmt.Fprintln(w, c(dim, "    Exit any running `claude` session and start a new one."))
		fmt.Fprintln(w)
	}
	if containsAgent(agents, AgentOpenAI) {
		fmt.Fprintf(w, "    %s\n", c(cyan, fmt.Sprintf("OPENAI_BASE_URL=%q", openAIURL)))
		fmt.Fprintln(w)
	}
	if containsAgent(agents, AgentCursor) {
		fmt.Fprintln(w, c(bold, "  Cursor IDE:"))
		fmt.Fprintln(w, "    Settings → Models → Override OpenAI Base URL")
		fmt.Fprintf(w, "    %s\n", c(cyan, openAIURL))
		fmt.Fprintln(w, c(dim, "    (Cursor may require a public tunnel for localhost — see README)"))
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, c(dim, "  Shell profile updated — new terminals pick up exports automatically."))
	fmt.Fprintln(w)
}

const (
	profileBegin = "# >>> llm-guard begin >>>"
	profileEnd   = "# <<< llm-guard end <<<"
)

func writeShellProfile(listen string, agents []Agent) error {
	profile, err := shellProfilePath()
	if err != nil {
		return err
	}

	baseHTTP := "http://" + listen
	openAIURL := baseHTTP + "/v1"
	anthropicURL := baseHTTP

	var lines []string
	lines = append(lines, profileBegin)
	if containsAgent(agents, AgentOpenAI) || containsAgent(agents, AgentCursor) {
		lines = append(lines, fmt.Sprintf("export OPENAI_BASE_URL=%q", openAIURL))
	}
	if containsAgent(agents, AgentClaude) {
		lines = append(lines, fmt.Sprintf("export ANTHROPIC_BASE_URL=%q", anthropicURL))
	}
	lines = append(lines, profileEnd)

	block := strings.Join(lines, "\n") + "\n"

	existing, _ := os.ReadFile(profile)
	content := string(existing)
	if idx := strings.Index(content, profileBegin); idx >= 0 {
		if end := strings.Index(content[idx:], profileEnd); end >= 0 {
			end += idx + len(profileEnd)
			for end < len(content) && content[end] == '\n' {
				end++
			}
			content = content[:idx] + block + content[end:]
		} else {
			content = strings.TrimRight(content, "\n") + "\n\n" + block
		}
	} else {
		if len(content) > 0 && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n" + block
	}

	if err := os.MkdirAll(filepath.Dir(profile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(profile, []byte(content), 0o644)
}

func shellProfilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	shell := os.Getenv("SHELL")
	if strings.HasSuffix(shell, "zsh") {
		return filepath.Join(home, ".zshrc"), nil
	}
	if strings.HasSuffix(shell, "bash") {
		if _, err := os.Stat(filepath.Join(home, ".bashrc")); err == nil {
			return filepath.Join(home, ".bashrc"), nil
		}
		return filepath.Join(home, ".bash_profile"), nil
	}
	return filepath.Join(home, ".profile"), nil
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
