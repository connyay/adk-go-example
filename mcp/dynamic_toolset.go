package mcp

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
)

// DynamicMCPToolset implements tool.Toolset and dynamically manages MCP server connections.
// It watches a directory for JSON config files and connects/disconnects servers as needed.
type DynamicMCPToolset struct {
	mu      sync.RWMutex
	dir     string
	servers map[string]*serverState
}

type serverState struct {
	config  *ServerConfig
	toolset tool.Toolset
	cancel  context.CancelFunc
	healthy bool
	lastErr error
}

func NewDynamicMCPToolset(dir string) *DynamicMCPToolset {
	return &DynamicMCPToolset{
		dir:     dir,
		servers: make(map[string]*serverState),
	}
}

func (d *DynamicMCPToolset) Name() string {
	return "dynamic_mcp_toolset"
}

func (d *DynamicMCPToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var allTools []tool.Tool
	for name, state := range d.servers {
		if !state.healthy {
			continue
		}

		tools, err := state.toolset.Tools(ctx)
		if err != nil {
			log.Printf("[MCP] Server %s returned error: %v", name, err)
			// Mark as unhealthy for future calls (need write lock)
			go d.markUnhealthy(name, err)
			continue
		}
		allTools = append(allTools, tools...)
	}

	return allTools, nil
}

// markUnhealthy is called asynchronously from Tools() when a server returns an error.
func (d *DynamicMCPToolset) markUnhealthy(name string, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if state, ok := d.servers[name]; ok {
		state.healthy = false
		state.lastErr = err
		log.Printf("[MCP] Server %s marked unhealthy: %v", name, err)
	}
}

func (d *DynamicMCPToolset) Reload(ctx context.Context) error {
	configs, err := LoadServerConfigs(d.dir)
	if err != nil {
		return fmt.Errorf("failed to load server configs: %w", err)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	found := make(map[string]bool)
	changes := 0

	for _, cfg := range configs {
		found[cfg.Name] = true

		existing, exists := d.servers[cfg.Name]
		if !exists {
			if err := d.connectServerLocked(ctx, cfg); err != nil {
				log.Printf("[MCP] Failed to connect server %s: %v", cfg.Name, err)
			} else {
				log.Printf("[MCP] Connected new server: %s", cfg.Name)
				changes++
			}
		} else if !configsEqual(existing.config, cfg) {
			log.Printf("[MCP] Config changed for server %s, reconnecting", cfg.Name)
			d.disconnectServerLocked(existing)
			if err := d.connectServerLocked(ctx, cfg); err != nil {
				log.Printf("[MCP] Failed to reconnect server %s: %v", cfg.Name, err)
			}
			changes++
		} else if !existing.healthy {
			// Config unchanged but server unhealthy - try to reconnect
			log.Printf("[MCP] Attempting to reconnect unhealthy server: %s", cfg.Name)
			d.disconnectServerLocked(existing)
			if err := d.connectServerLocked(ctx, cfg); err != nil {
				log.Printf("[MCP] Failed to reconnect server %s: %v", cfg.Name, err)
			} else {
				changes++
			}
		}
		// else: unchanged and healthy, keep existing
	}

	for name, state := range d.servers {
		if !found[name] {
			log.Printf("[MCP] Removing server: %s", name)
			d.disconnectServerLocked(state)
			delete(d.servers, name)
			changes++
		}
	}

	if changes > 0 {
		log.Printf("[MCP] Reload complete: %d changes, %d servers", changes, len(d.servers))
	}

	return nil
}

// connectServerLocked must be called with d.mu held.
func (d *DynamicMCPToolset) connectServerLocked(ctx context.Context, cfg *ServerConfig) error {
	serverCtx, cancel := context.WithCancel(ctx)

	env := os.Environ()
	for k, v := range cfg.Env {
		env = append(env, k+"="+v)
	}

	cmd := exec.CommandContext(serverCtx, cfg.Command, cfg.Args...)
	cmd.Env = env

	ts, err := mcptoolset.New(mcptoolset.Config{
		Transport: &mcp.CommandTransport{Command: cmd},
	})
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create MCP toolset: %w", err)
	}

	d.servers[cfg.Name] = &serverState{
		config:  cfg,
		toolset: ts,
		cancel:  cancel,
		healthy: true,
	}

	return nil
}

// disconnectServerLocked must be called with d.mu held.
func (d *DynamicMCPToolset) disconnectServerLocked(state *serverState) {
	if state.cancel != nil {
		state.cancel()
	}
}

func (d *DynamicMCPToolset) StartPolling(ctx context.Context, interval time.Duration) {
	if err := d.Reload(ctx); err != nil {
		log.Printf("[MCP] Initial load error: %v", err)
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		log.Printf("[MCP] Starting polling every %s", interval)

		for {
			select {
			case <-ctx.Done():
				log.Printf("[MCP] Polling stopped")
				return
			case <-ticker.C:
				if err := d.Reload(ctx); err != nil {
					log.Printf("[MCP] Reload error: %v", err)
				}
			}
		}
	}()
}

func (d *DynamicMCPToolset) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	for name, state := range d.servers {
		log.Printf("[MCP] Closing server: %s", name)
		d.disconnectServerLocked(state)
	}

	d.servers = make(map[string]*serverState)
	return nil
}

func (d *DynamicMCPToolset) ServerCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.servers)
}

func (d *DynamicMCPToolset) HealthyServerCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()

	count := 0
	for _, state := range d.servers {
		if state.healthy {
			count++
		}
	}
	return count
}
