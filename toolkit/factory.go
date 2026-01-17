package toolkit

import (
	"fmt"
	"strings"

	"github.com/connyay/adk-go-example/state"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
)

func BuildToolkitAgent(m model.LLM, config *ToolkitConfig, toolReg *ToolRegistry, mcpToolset tool.Toolset) (agent.Agent, error) {
	tools, err := toolReg.BuildTools(config.Tools)
	if err != nil {
		return nil, fmt.Errorf("failed to build tools for toolkit %q: %w", config.Name, err)
	}

	agentName := fmt.Sprintf("%sToolkit", capitalize(config.Name))
	instruction := buildInstructionWithState(config)

	outputKey := config.OutputKey
	if outputKey == "" {
		outputKey = state.FinalResponse.Key
	}

	var toolsets []tool.Toolset
	if mcpToolset != nil {
		toolsets = append(toolsets, mcpToolset)
	}

	return llmagent.New(llmagent.Config{
		Name:        agentName,
		Description: config.Description,
		Model:       m,
		Instruction: instruction,
		Tools:       tools,
		Toolsets:    toolsets,
		OutputKey:   outputKey,
	})
}

func buildInstructionWithState(config *ToolkitConfig) string {
	if len(config.DependsOn) == 0 {
		return config.Instruction
	}

	stateDoc := state.FormatStateAccess(config.DependsOn)
	return stateDoc + "\n" + config.Instruction
}

func BuildAllToolkitAgents(m model.LLM, tkReg *ToolkitRegistry, toolReg *ToolRegistry, mcpToolset tool.Toolset) (map[string]agent.Agent, error) {
	agents := make(map[string]agent.Agent)

	for name, config := range tkReg.Toolkits {
		a, err := BuildToolkitAgent(m, config, toolReg, mcpToolset)
		if err != nil {
			return nil, fmt.Errorf("failed to build toolkit agent %q: %w", name, err)
		}
		agents[name] = a
	}

	return agents, nil
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
