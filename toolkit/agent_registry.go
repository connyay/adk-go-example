package toolkit

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
)

// ToolkitAgentRegistry provides lazy-loading and hot-reloading of toolkit agents.
// It watches a directory for markdown toolkit files and rebuilds agents when files change.
type ToolkitAgentRegistry struct {
	mu         sync.RWMutex
	dir        string
	toolReg    *ToolRegistry
	llm        model.LLM
	mcpToolset tool.Toolset

	// configs holds the current toolkit configurations, keyed by name
	configs map[string]*ToolkitConfig
	// agents holds the cached agent instances, keyed by name
	agents map[string]agent.Agent
	// hashes holds file content hashes to detect changes
	hashes map[string]string
}

func NewToolkitAgentRegistry(dir string, toolReg *ToolRegistry, llm model.LLM, mcpToolset tool.Toolset) (*ToolkitAgentRegistry, error) {
	r := &ToolkitAgentRegistry{
		dir:        dir,
		toolReg:    toolReg,
		llm:        llm,
		mcpToolset: mcpToolset,
		configs:    make(map[string]*ToolkitConfig),
		agents:     make(map[string]agent.Agent),
		hashes:     make(map[string]string),
	}

	if err := r.Reload(); err != nil {
		return nil, fmt.Errorf("initial toolkit load failed: %w", err)
	}

	return r, nil
}

func (r *ToolkitAgentRegistry) Get(name string) (agent.Agent, bool) {
	r.mu.RLock()
	ag, ok := r.agents[name]
	if ok {
		r.mu.RUnlock()
		return ag, true
	}

	config, hasConfig := r.configs[name]
	r.mu.RUnlock()

	if !hasConfig {
		return nil, false
	}

	// Need to build the agent - upgrade to write lock
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if ag, ok := r.agents[name]; ok {
		return ag, true
	}

	ag, err := BuildToolkitAgent(r.llm, config, r.toolReg, r.mcpToolset)
	if err != nil {
		log.Printf("[TOOLKIT-REGISTRY] Failed to build agent %q: %v", name, err)
		return nil, false
	}

	r.agents[name] = ag
	log.Printf("[TOOLKIT-REGISTRY] Built agent %q on demand", name)
	return ag, true
}

func (r *ToolkitAgentRegistry) All() []agent.Agent {
	r.mu.RLock()
	names := make([]string, 0, len(r.configs))
	for name := range r.configs {
		names = append(names, name)
	}
	r.mu.RUnlock()

	agents := make([]agent.Agent, 0, len(names))
	for _, name := range names {
		if ag, ok := r.Get(name); ok {
			agents = append(agents, ag)
		}
	}
	return agents
}

func (r *ToolkitAgentRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.configs))
	for name := range r.configs {
		names = append(names, name)
	}
	return names
}

func (r *ToolkitAgentRegistry) Configs() *ToolkitRegistry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	reg := NewToolkitRegistry()
	for _, config := range r.configs {
		reg.Register(config)
	}
	return reg
}

// SetMCPToolset clears the agent cache so agents are rebuilt with the new toolset.
func (r *ToolkitAgentRegistry) SetMCPToolset(ts tool.Toolset) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.mcpToolset = ts
	r.agents = make(map[string]agent.Agent)
	log.Printf("[TOOLKIT-REGISTRY] MCP toolset updated, agent cache cleared")
}

func (r *ToolkitAgentRegistry) Reload() error {
	pattern := filepath.Join(r.dir, "*.md")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to glob toolkit files: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	found := make(map[string]bool)
	changes := 0

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			log.Printf("[TOOLKIT-REGISTRY] Failed to read %s: %v", file, err)
			continue
		}

		hash := hashContent(content)
		baseName := filepath.Base(file)

		if oldHash, exists := r.hashes[baseName]; exists && oldHash == hash {
			found[getConfigName(file)] = true
			continue
		}

		config, err := loadToolkitFile(file)
		if err != nil {
			log.Printf("[TOOLKIT-REGISTRY] Failed to parse %s: %v", file, err)
			continue
		}

		found[config.Name] = true

		if _, exists := r.configs[config.Name]; exists {
			log.Printf("[TOOLKIT-REGISTRY] Toolkit %q changed, invalidating cache", config.Name)
		} else {
			log.Printf("[TOOLKIT-REGISTRY] New toolkit %q discovered", config.Name)
		}

		r.configs[config.Name] = config
		r.hashes[baseName] = hash
		delete(r.agents, config.Name) // Invalidate cached agent
		changes++
	}

	for name := range r.configs {
		if !found[name] {
			log.Printf("[TOOLKIT-REGISTRY] Toolkit %q removed", name)
			delete(r.configs, name)
			delete(r.agents, name)
			changes++
		}
	}

	if changes > 0 {
		log.Printf("[TOOLKIT-REGISTRY] Reload complete: %d changes, %d toolkits", changes, len(r.configs))
	}

	return nil
}

func (r *ToolkitAgentRegistry) StartPolling(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		log.Printf("[TOOLKIT-REGISTRY] Starting polling every %s", interval)

		for {
			select {
			case <-ctx.Done():
				log.Printf("[TOOLKIT-REGISTRY] Polling stopped")
				return
			case <-ticker.C:
				if err := r.Reload(); err != nil {
					log.Printf("[TOOLKIT-REGISTRY] Reload error: %v", err)
				}
			}
		}
	}()
}

func hashContent(content []byte) string {
	h := sha256.Sum256(content)
	return fmt.Sprintf("%x", h)
}

func getConfigName(file string) string {
	base := filepath.Base(file)
	return base[:len(base)-len(filepath.Ext(base))]
}
