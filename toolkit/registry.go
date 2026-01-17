package toolkit

import (
	"fmt"

	"google.golang.org/adk/tool"
)

// ToolRegistry holds all registered tools.
type ToolRegistry struct {
	tools map[string]tool.Tool
}

// NewToolRegistry creates a new empty tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]tool.Tool),
	}
}

// Register adds a pre-built tool to the registry.
func (r *ToolRegistry) Register(name string, t tool.Tool) {
	r.tools[name] = t
}

// Get retrieves a tool by name.
func (r *ToolRegistry) Get(name string) (tool.Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// BuildTools retrieves tools from a list of tool names.
func (r *ToolRegistry) BuildTools(names []string) ([]tool.Tool, error) {
	tools := make([]tool.Tool, 0, len(names))

	for _, name := range names {
		t, ok := r.tools[name]
		if !ok {
			return nil, fmt.Errorf("tool %q not found in registry", name)
		}
		tools = append(tools, t)
	}

	return tools, nil
}

// Names returns all registered tool names.
func (r *ToolRegistry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}
