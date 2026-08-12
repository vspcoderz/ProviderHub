package skill

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	skillFileName = "skills.yaml"
	filePerms     = 0600
	dirPerms      = 0700
)

// SkillStore holds skill definitions keyed by name.
type SkillStore struct {
	Skills map[string]string `yaml:"skills"`
}

func skillFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", "provider-hub", skillFileName), nil
}

func Load() (*SkillStore, error) {
	p, err := skillFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &SkillStore{Skills: map[string]string{}}, nil
		}
		return nil, fmt.Errorf("read skills: %w", err)
	}

	var ss SkillStore
	if err := yaml.Unmarshal(data, &ss); err != nil {
		return nil, fmt.Errorf("parse skills: %w", err)
	}
	if ss.Skills == nil {
		ss.Skills = map[string]string{}
	}
	return &ss, nil
}

func Save(ss *SkillStore) error {
	p, err := skillFilePath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, dirPerms); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	data, err := yaml.Marshal(ss)
	if err != nil {
		return fmt.Errorf("marshal skills: %w", err)
	}

	if err := os.WriteFile(p, data, filePerms); err != nil {
		return fmt.Errorf("write skills: %w", err)
	}
	return nil
}

func Set(name, body string) error {
	ss, err := Load()
	if err != nil {
		return err
	}
	ss.Skills[name] = body
	return Save(ss)
}

func Get(name string) (string, error) {
	ss, err := Load()
	if err != nil {
		return "", err
	}
	return ss.Skills[name], nil
}

func Remove(name string) error {
	ss, err := Load()
	if err != nil {
		return err
	}
	delete(ss.Skills, name)
	return Save(ss)
}

func List() (map[string]string, error) {
	ss, err := Load()
	if err != nil {
		return nil, err
	}
	return ss.Skills, nil
}
