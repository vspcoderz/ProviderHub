package agent

import (
    "fmt"
    "os"
    "path/filepath"
    "gopkg.in/yaml.v3"
)

const (
    agentMemoryFileName = "agent_memory.yaml"
    filePerms = 0600
    dirPerms = 0700
)

type AgentMemory struct {
    Memories map[string]string `yaml:"memories"`
}

func agentMemoryFilePath() (string, error) {
    home, err := os.UserHomeDir()
    if err != nil {
        return "", fmt.Errorf("home dir: %w", err)
    }
    return filepath.Join(home, ".config", "provider-hub", agentMemoryFileName), nil
}

func Load() (*AgentMemory, error) {
    p, err := agentMemoryFilePath()
    if err != nil {
        return nil, err
    }

    data, err := os.ReadFile(p)
    if err != nil {
        if os.IsNotExist(err) {
            return &AgentMemory{Memories: map[string]string{}}, nil
        }
        return nil, fmt.Errorf("read agent_memory: %w", err)
    }

    var am AgentMemory
    if err := yaml.Unmarshal(data, &am); err != nil {
        return nil, fmt.Errorf("parse agent_memory: %w", err)
    }
    if am.Memories == nil {
        am.Memories = map[string]string{}
    }
    return &am, nil
}

func Save(am *AgentMemory) error {
    p, err := agentMemoryFilePath()
    if err != nil {
        return err
    }

    dir := filepath.Dir(p)
    if err := os.MkdirAll(dir, dirPerms); err != nil {
        return fmt.Errorf("mkdir %s: %w", dir, err)
    }

    data, err := yaml.Marshal(am)
    if err != nil {
        return fmt.Errorf("marshal agent_memory: %w", err)
    }

    if err := os.WriteFile(p, data, filePerms); err != nil {
        return fmt.Errorf("write agent_memory: %w", err)
    }
    return nil
}

func Set(providerID, memory string) error {
    am, err := Load()
    if err != nil {
        return err
    }
    am.Memories[providerID] = memory
    return Save(am)
}

func Get(providerID string) (string, error) {
    am, err := Load()
    if err != nil {
        return "", err
    }
    return am.Memories[providerID], nil
}

func Remove(providerID string) error {
    am, err := Load()
    if err != nil {
        return err
    }
    delete(am.Memories, providerID)
    return Save(am)
}

func List() (map[string]string, error) {
    am, err := Load()
    if err != nil {
        return nil, err
    }
    return am.Memories, nil
}

func Mask(memory string) string {
    if len(memory) <= 8 {
        return "****"
    }
    return memory[:4] + "****" + memory[len(memory)-4:]
}
