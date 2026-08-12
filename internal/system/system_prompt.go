package system

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
)

const (
	systemPromptsFileName = "system_prompts.yaml"
	filePerms             = 0600
	dirPerms              = 0700
)

type SystemPromptsStore struct {
	Prompts map[string]string `yaml:"prompts"`
}

func systemPromptsFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", "provider-hub", systemPromptsFileName), nil
}

func LoadSystemPrompts() (*SystemPromptsStore, error) {
	p, err := systemPromptsFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &SystemPromptsStore{Prompts: map[string]string{}}, nil
		}
		return nil, fmt.Errorf("read system_prompts: %w", err)
	}

	var sp SystemPromptsStore
	if err := yaml.Unmarshal(data, &sp); err != nil {
		return nil, fmt.Errorf("parse system_prompts: %w", err)
	}
	if sp.Prompts == nil {
		sp.Prompts = map[string]string{}
	}
	return &sp, nil
}

func SaveSystemPrompts(sp *SystemPromptsStore) error {
	p, err := systemPromptsFilePath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, dirPerms); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	data, err := yaml.Marshal(sp)
	if err != nil {
		return fmt.Errorf("marshal system_prompts: %w", err)
	}

	if err := os.WriteFile(p, data, 0o600); err != nil {
		return fmt.Errorf("write system_prompts: %w", err)
	}
	return nil
}

func SetSystemPrompt(providerID, prompt string) error {
	sp, err := LoadSystemPrompts()
	if err != nil {
		return err
	}
	sp.Prompts[providerID] = prompt
	return SaveSystemPrompts(sp)
}

func GetSystemPrompt(providerID string) (string, error) {
	sp, err := LoadSystemPrompts()
	if err != nil {
		return "", err
	}
	return sp.Prompts[providerID], nil
}

func RemoveSystemPrompt(providerID string) error {
	sp, err := LoadSystemPrompts()
	if err != nil {
		return err
	}
	delete(sp.Prompts, providerID)
	return SaveSystemPrompts(sp)
}

func ListSystemPrompts() (map[string]string, error) {
	sp, err := LoadSystemPrompts()
	if err != nil {
		return nil, err
	}
	return sp.Prompts, nil
}

func MaskSystemPrompt(prompt string) string {
	if len(prompt) <= 8 {
		return "****"
	}
	return prompt[:4] + "****" + prompt[len(prompt)-4:]
}
