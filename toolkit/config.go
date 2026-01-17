package toolkit

import (
	"fmt"
	"sort"
	"strings"
)

// StrategyConfig represents an actionable strategy defined in a toolkit.
type StrategyConfig struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Keywords    []string `yaml:"keywords"` // trigger keywords for this strategy
	Steps       []string `yaml:"steps"`    // execution steps
}

// ToolkitConfig represents a toolkit loaded from a markdown file.
type ToolkitConfig struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Tools       []string         `yaml:"tools"`
	Keywords    []string         `yaml:"keywords"`
	Strategies  []StrategyConfig `yaml:"strategies"` // strategies this toolkit provides
	DependsOn   []string         `yaml:"depends_on"` // state keys this toolkit reads from
	OutputKey   string           `yaml:"output_key"` // state key this toolkit writes to (defaults to "final_response")
	Instruction string           // markdown body (not in YAML frontmatter)
}

// ToolkitRegistry holds all loaded toolkit configurations.
type ToolkitRegistry struct {
	Toolkits map[string]*ToolkitConfig
}

// NewToolkitRegistry creates a new empty toolkit registry.
func NewToolkitRegistry() *ToolkitRegistry {
	return &ToolkitRegistry{
		Toolkits: make(map[string]*ToolkitConfig),
	}
}

// Register adds a toolkit config to the registry.
func (r *ToolkitRegistry) Register(config *ToolkitConfig) {
	r.Toolkits[config.Name] = config
}

// Get retrieves a toolkit config by name.
func (r *ToolkitRegistry) Get(name string) (*ToolkitConfig, bool) {
	config, ok := r.Toolkits[name]
	return config, ok
}

// GetAvailableToolkitsPrompt generates a prompt string describing all available toolkits.
func (r *ToolkitRegistry) GetAvailableToolkitsPrompt() string {
	if len(r.Toolkits) == 0 {
		return "No toolkits available."
	}

	// Sort toolkit names for deterministic output
	names := r.Names()
	sort.Strings(names)

	var sb strings.Builder
	sb.WriteString("## Available Toolkits\n")

	for _, name := range names {
		config := r.Toolkits[name]
		sb.WriteString(fmt.Sprintf("- %q: %s (tools: %s)\n",
			name,
			config.Description,
			strings.Join(config.Tools, ", ")))
	}

	return sb.String()
}

// GetAllKeywordMappings returns a map of keywords to toolkit names.
func (r *ToolkitRegistry) GetAllKeywordMappings() map[string][]string {
	mappings := make(map[string][]string)

	for name, config := range r.Toolkits {
		for _, keyword := range config.Keywords {
			mappings[keyword] = append(mappings[keyword], name)
		}
	}

	return mappings
}

// Names returns all registered toolkit names.
func (r *ToolkitRegistry) Names() []string {
	names := make([]string, 0, len(r.Toolkits))
	for name := range r.Toolkits {
		names = append(names, name)
	}
	return names
}

// MatchedStrategy represents a strategy that matched a query, with its toolkit.
type MatchedStrategy struct {
	Strategy      StrategyConfig
	Toolkit       string
	MatchedCount  int // number of keywords that matched
	TotalKeywords int // total keywords for this strategy
}

// GetMatchingStrategies returns all strategies whose keywords match the query.
func (r *ToolkitRegistry) GetMatchingStrategies(query string) []MatchedStrategy {
	queryLower := strings.ToLower(query)
	var matches []MatchedStrategy

	for toolkitName, config := range r.Toolkits {
		for _, strategy := range config.Strategies {
			matchedCount := 0
			for _, keyword := range strategy.Keywords {
				if strings.Contains(queryLower, strings.ToLower(keyword)) {
					matchedCount++
				}
			}
			if matchedCount > 0 {
				matches = append(matches, MatchedStrategy{
					Strategy:      strategy,
					Toolkit:       toolkitName,
					MatchedCount:  matchedCount,
					TotalKeywords: len(strategy.Keywords),
				})
			}
		}
	}

	return matches
}

// GetAllStrategyKeywords returns all unique keywords from all strategies.
func (r *ToolkitRegistry) GetAllStrategyKeywords() []string {
	seen := make(map[string]bool)
	var keywords []string

	for _, config := range r.Toolkits {
		for _, strategy := range config.Strategies {
			for _, keyword := range strategy.Keywords {
				if !seen[keyword] {
					seen[keyword] = true
					keywords = append(keywords, keyword)
				}
			}
		}
	}

	return keywords
}
