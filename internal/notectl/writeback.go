package notectl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"
)

// WriteBack writes a diary entry into the notectl Obsidian vault.
// Returns nil silently when notectl is not installed or not configured.
func WriteBack(date time.Time, body string) error {
	vaultPath, err := readVaultPath()
	if err != nil || vaultPath == "" {
		return nil // notectl not installed/configured → silent skip
	}

	folder := filepath.Join(vaultPath, "Diary")
	if err := os.MkdirAll(folder, 0755); err != nil {
		return fmt.Errorf("notectl writeback: %w", err)
	}

	filename := filepath.Join(folder, date.Format("2006-01-02")+".md")
	return os.WriteFile(filename, []byte(body), 0644)
}

func readVaultPath() (string, error) {
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, ".config", "notectl", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return "", nil // not installed
	}
	var cfg struct {
		VaultPath string `yaml:"vault_path"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "", nil
	}
	p := cfg.VaultPath
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(home, p[2:])
	}
	return p, nil
}
