package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Backup copies a file before overwriting. Returns the backup path.
func Backup(path string) (string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", nil
	}

	backup := fmt.Sprintf("%s.bak.%s", path, time.Now().Format("20060102_150405"))
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if err := os.WriteFile(backup, data, 0o644); err != nil {
		return "", fmt.Errorf("write backup: %w", err)
	}
	return backup, nil
}

// EnsureDir creates the parent directory of a file if it doesn't exist.
func EnsureDir(path string) error {
	dir := filepath.Dir(path)
	return os.MkdirAll(dir, 0o755)
}
