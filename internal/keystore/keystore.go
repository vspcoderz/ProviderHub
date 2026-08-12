package keystore

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	keystoreFileName = "keys.yaml"
	filePerms        = 0600
	dirPerms         = 0700
)

// Keystore holds API keys keyed by provider ID.
type Keystore struct {
	Keys map[string]string `yaml:"keys"`
}

func keystoreFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".config", "provider-hub", keystoreFileName), nil
}

func Load() (*Keystore, error) {
	p, err := keystoreFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &Keystore{Keys: map[string]string{}}, nil
		}
		return nil, fmt.Errorf("read keys: %w", err)
	}

	var ks Keystore
	if err := yaml.Unmarshal(data, &ks); err != nil {
		return nil, fmt.Errorf("parse keys: %w", err)
	}
	if ks.Keys == nil {
		ks.Keys = map[string]string{}
	}
	return &ks, nil
}

func Save(ks *Keystore) error {
	p, err := keystoreFilePath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, dirPerms); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	data, err := yaml.Marshal(ks)
	if err != nil {
		return fmt.Errorf("marshal keys: %w", err)
	}

	if err := os.WriteFile(p, data, filePerms); err != nil {
		return fmt.Errorf("write keys: %w", err)
	}
	return nil
}

func Set(providerID, key string) error {
	ks, err := Load()
	if err != nil {
		return err
	}
	ks.Keys[providerID] = key
	return Save(ks)
}

func Get(providerID string) (string, error) {
	ks, err := Load()
	if err != nil {
		return "", err
	}
	return ks.Keys[providerID], nil
}

func Remove(providerID string) error {
	ks, err := Load()
	if err != nil {
		return err
	}
	delete(ks.Keys, providerID)
	return Save(ks)
}

func List() (map[string]string, error) {
	ks, err := Load()
	if err != nil {
		return nil, err
	}
	return ks.Keys, nil
}

func Mask(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}
