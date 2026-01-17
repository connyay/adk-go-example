package toolkit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadToolkits loads all toolkit configurations from markdown files in the given directory.
func LoadToolkits(dir string) (*ToolkitRegistry, error) {
	registry := NewToolkitRegistry()

	pattern := filepath.Join(dir, "*.md")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to glob toolkit files: %w", err)
	}

	if len(files) == 0 {
		return registry, nil
	}

	for _, file := range files {
		config, err := loadToolkitFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to load toolkit %s: %w", file, err)
		}
		registry.Register(config)
	}

	return registry, nil
}

// loadToolkitFile loads a single toolkit configuration from a markdown file.
func loadToolkitFile(path string) (*ToolkitConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	frontmatter, body, err := parseFrontmatter(string(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	var config ToolkitConfig
	if err := yaml.Unmarshal([]byte(frontmatter), &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	config.Instruction = strings.TrimSpace(body)

	if config.Name == "" {
		// Use filename without extension as fallback name
		base := filepath.Base(path)
		config.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}

	return &config, nil
}

// parseFrontmatter extracts YAML frontmatter from markdown content.
// Frontmatter is delimited by "---" at the start and end.
func parseFrontmatter(content string) (frontmatter, body string, err error) {
	const delimiter = "---"

	// Must start with ---
	if !strings.HasPrefix(content, delimiter) {
		return "", content, nil
	}

	// Find the closing ---
	rest := content[len(delimiter):]
	endIdx := strings.Index(rest, "\n"+delimiter)
	if endIdx == -1 {
		return "", content, nil
	}

	frontmatter = strings.TrimSpace(rest[:endIdx])
	body = strings.TrimPrefix(rest[endIdx+len("\n"+delimiter):], "\n")

	return frontmatter, body, nil
}
