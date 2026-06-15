// Package config handles loading, saving, and defaulting llm-guard's
// configuration file (~/.config/llmguard/config.yaml).
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"llmguard/internal/redact/detectors"
)

// Config is the top-level configuration loaded from config.yaml.
type Config struct {
	Listen    string          `yaml:"listen"`
	Upstream  string          `yaml:"upstream"`
	LogFile   string          `yaml:"log_file"`
	Detectors DetectorsConfig `yaml:"detectors"`
}

// DetectorsConfig groups settings for each detector type.
type DetectorsConfig struct {
	Regex       RegexConfig       `yaml:"regex"`
	LLMFallback LLMFallbackConfig `yaml:"llm_fallback"`
}

// RegexConfig configures the built-in regex detector.
type RegexConfig struct {
	Enabled           bool                      `yaml:"enabled"`
	BuiltinCategories []string                  `yaml:"builtin_categories"`
	CustomPatterns    []detectors.CustomPattern `yaml:"custom_patterns"`
}

// LLMFallbackConfig configures the optional local-LLM semantic detector. When
// enabled, llm-guard spawns a local `llama-server` subprocess (downloaded via
// `llmguard models pull`) and uses it as an additional, best-effort detector
// for sensitive content that regex patterns miss. If the server binary or
// model is missing, or the server fails to start, llm-guard logs a warning
// and continues with regex-only detection.
type LLMFallbackConfig struct {
	Enabled bool `yaml:"enabled"`

	// ServerPath is the path to the llama-server binary, set by
	// `llmguard models pull`.
	ServerPath string `yaml:"server_path"`
	// ModelPath is the path to the GGUF model file, set by
	// `llmguard models pull`.
	ModelPath string `yaml:"model_path"`
	// Port is the local port llama-server listens on.
	Port int `yaml:"port"`

	// MinTextLen is the minimum string length (in bytes) considered for the
	// LLM pass; shorter strings are skipped.
	MinTextLen int `yaml:"min_text_len"`
	// MaxTextLen is the maximum string length (in bytes) considered for the
	// LLM pass; longer strings are skipped to bound latency.
	MaxTextLen int `yaml:"max_text_len"`

	// RequestTimeoutMS bounds a single call to llama-server's /completion
	// endpoint.
	RequestTimeoutMS int `yaml:"request_timeout_ms"`
	// OverallTimeoutMS bounds the total time spent across all LLM detector
	// calls for a single proxied request.
	OverallTimeoutMS int `yaml:"overall_timeout_ms"`

	// LlamacppRelease is the ggml-org/llama.cpp release tag `models pull`
	// downloads from, or "latest".
	LlamacppRelease string `yaml:"llamacpp_release"`
}

const (
	dirName       = "llmguard"
	fileName      = "config.yaml"
	logName       = "redactions.log"
	defaultListen = "127.0.0.1:8317"
)

// Default returns a Config populated with sensible defaults. Upstream is
// left blank and must be set by the user (via `llmguard init` or by editing
// the config file).
func Default() *Config {
	return &Config{
		Listen:   defaultListen,
		Upstream: "",
		LogFile:  defaultLogPath(),
		Detectors: DetectorsConfig{
			Regex: RegexConfig{
				Enabled:           true,
				BuiltinCategories: detectors.BuiltinCategories(),
				CustomPatterns:    []detectors.CustomPattern{},
			},
			LLMFallback: LLMFallbackConfig{
				Enabled:          false,
				Port:             8418,
				MinTextLen:       8,
				MaxTextLen:       2000,
				RequestTimeoutMS: 3000,
				OverallTimeoutMS: 4000,
				LlamacppRelease:  "latest",
			},
		},
	}
}

// Path returns the default config file path: ~/.config/llmguard/config.yaml.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".config", dirName, fileName), nil
}

// StateDir returns the directory used for llm-guard's persistent local
// state (logs, downloaded llama-server binaries, GGUF models):
// ~/.local/share/llmguard.
func StateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", dirName), nil
}

func defaultLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", logName)
	}
	return filepath.Join(home, ".local", "share", dirName, logName)
}

// Load reads and parses the config file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	return cfg, nil
}

// Save writes cfg to path as YAML, creating parent directories as needed.
func Save(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	return nil
}

// Exists reports whether a file exists at path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
