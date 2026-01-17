package toolkit

import (
	"fmt"
	"strings"

	"github.com/connyay/adk-go-example/state"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
)

// BuildToolkitAgent builds a single toolkit agent from configuration.
func BuildToolkitAgent(m model.LLM, config *ToolkitConfig, toolReg *ToolRegistry) (agent.Agent, error) {
	tools, err := toolReg.BuildTools(config.Tools)
	if err != nil {
		return nil, fmt.Errorf("failed to build tools for toolkit %q: %w", config.Name, err)
	}

	agentName := fmt.Sprintf("%sToolkit", capitalize(config.Name))

	// Build instruction with state access documentation
	instruction := buildInstructionWithState(config)

	// Use output_key from config, default to final_response
	outputKey := config.OutputKey
	if outputKey == "" {
		outputKey = state.FinalResponse.Key
	}

	return llmagent.New(llmagent.Config{
		Name:        agentName,
		Description: config.Description,
		Model:       m,
		Instruction: instruction,
		Tools:       tools,
		OutputKey:   outputKey,
	})
}

// buildInstructionWithState prepends state access documentation to the instruction.
func buildInstructionWithState(config *ToolkitConfig) string {
	if len(config.DependsOn) == 0 {
		return config.Instruction
	}

	stateDoc := state.FormatStateAccess(config.DependsOn)
	return stateDoc + "\n" + config.Instruction
}

// BuildAllToolkitAgents builds all toolkit agents from a toolkit registry.
func BuildAllToolkitAgents(m model.LLM, tkReg *ToolkitRegistry, toolReg *ToolRegistry) (map[string]agent.Agent, error) {
	agents := make(map[string]agent.Agent)

	for name, config := range tkReg.Toolkits {
		a, err := BuildToolkitAgent(m, config, toolReg)
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
