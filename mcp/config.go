package mcp

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
)

type ServerConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

func LoadServerConfigs(dir string) ([]*ServerConfig, error) {
	pattern := filepath.Join(dir, "*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to glob config files: %w", err)
	}

	if len(files) == 0 {
		return nil, nil
	}

	configs := make([]*ServerConfig, 0, len(files))
	for _, file := range files {
		cfg, err := loadServerConfig(file)
		if err != nil {
			return nil, fmt.Errorf("failed to load config %s: %w", file, err)
		}
		configs = append(configs, cfg)
	}

	return configs, nil
}

func loadServerConfig(path string) (*ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var cfg ServerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	if cfg.Name == "" {
		// Use filename without extension as fallback
		base := filepath.Base(path)
		cfg.Name = base[:len(base)-len(filepath.Ext(base))]
	}

	if cfg.Command == "" {
		return nil, fmt.Errorf("command is required")
	}

	return &cfg, nil
}

func configsEqual(a, b *ServerConfig) bool {
	return a.Name == b.Name &&
		a.Command == b.Command &&
		slices.Equal(a.Args, b.Args) &&
		maps.Equal(a.Env, b.Env)
}
