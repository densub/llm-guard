package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// configureAgentSettings writes agent-specific config so clients pick up the
// proxy without relying on shell exports (which install scripts cannot apply
// to the parent shell when executed as ./install.sh).
func configureAgentSettings(listen string, agents []Agent) error {
	baseHTTP := "http://" + listen
	if containsAgent(agents, AgentClaude) {
		path, err := claudeSettingsPath()
		if err != nil {
			return err
		}
		if err := mergeJSONEnv(path, map[string]string{
			"ANTHROPIC_BASE_URL": baseHTTP,
		}); err != nil {
			return fmt.Errorf("claude settings: %w", err)
		}
	}
	return nil
}

func claudeSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// mergeJSONEnv merges key/value pairs into the "env" object of a JSON settings
// file, preserving all other top-level keys.
func mergeJSONEnv(path string, env map[string]string) error {
	settings := make(map[string]json.RawMessage)

	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	existing := map[string]string{}
	if raw, ok := settings["env"]; ok {
		_ = json.Unmarshal(raw, &existing)
	}
	for k, v := range env {
		existing[k] = v
	}
	envRaw, err := json.Marshal(existing)
	if err != nil {
		return err
	}
	settings["env"] = envRaw

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}
