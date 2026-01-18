package main

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"log"
	"strings"
	"time"

	"github.com/connyay/adk-go-example/mcp"
	"github.com/connyay/adk-go-example/state"
	"github.com/connyay/adk-go-example/toolkit"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/workflowagents/parallelagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

func buildPipeline(m model.LLM) (agent.Agent, error) {
	ctx := context.Background()
	toolReg := toolkit.NewToolRegistry()
	RegisterAllTools(toolReg)

	mcpToolset := mcp.NewDynamicMCPToolset("./mcp-servers")
	mcpToolset.StartPolling(ctx, 5*time.Second)

	toolkitAgentReg, err := toolkit.NewToolkitAgentRegistry("./toolkits", toolReg, m, mcpToolset)
	if err != nil {
		return nil, fmt.Errorf("failed to create toolkit agent registry: %w", err)
	}
	toolkitAgentReg.StartPolling(ctx, 5*time.Second)

	tkReg := toolkitAgentReg.Configs()
	log.Printf("[PIPELINE] Loaded %d toolkits: %v", len(tkReg.Toolkits), tkReg.Names())

	SetToolkitRegistry(tkReg)

	guardrails, err := buildGuardrailsAgent(m, "GuardrailsAgent", tkReg)
	if err != nil {
		return nil, err
	}

	docAgent, err := buildDocSearchAgent(m)
	if err != nil {
		return nil, err
	}

	strategyAgent, err := buildStrategySearchAgent(m, "StrategySearchAgent")
	if err != nil {
		return nil, err
	}

	// Separate instances for pivot intake (agents can only have one parent, names must be unique)
	pivotGuardrails, err := buildGuardrailsAgent(m, "PivotGuardrailsAgent", tkReg)
	if err != nil {
		return nil, err
	}

	pivotStrategyAgent, err := buildStrategySearchAgent(m, "PivotStrategySearchAgent")
	if err != nil {
		return nil, err
	}

	orchestrator, err := buildOrchestratorAgent(m, tkReg)
	if err != nil {
		return nil, err
	}

	synthesizer, err := buildSynthesizerAgent(m)
	if err != nil {
		return nil, err
	}

	greeter, err := buildGreeterAgent(m, tkReg, mcpToolset.ServerCount())
	if err != nil {
		return nil, err
	}

	parallelIntake, err := parallelagent.New(parallelagent.Config{
		AgentConfig: agent.Config{
			Name:        "ParallelIntake",
			Description: "Runs guardrails, doc search, and strategy search in parallel",
			SubAgents:   []agent.Agent{guardrails, docAgent, strategyAgent},
		},
	})
	if err != nil {
		return nil, err
	}

	// Pivot intake runs only guardrails + strategy (no doc search)
	pivotIntake, err := parallelagent.New(parallelagent.Config{
		AgentConfig: agent.Config{
			Name:        "PivotIntake",
			Description: "Runs guardrails and strategy search in parallel for pivot operations",
			SubAgents:   []agent.Agent{pivotGuardrails, pivotStrategyAgent},
		},
	})
	if err != nil {
		return nil, err
	}

	pivotOrchestrator, err := buildPivotOrchestrator(m, tkReg)
	if err != nil {
		return nil, err
	}

	subAgents := []agent.Agent{parallelIntake, pivotIntake, orchestrator, pivotOrchestrator, synthesizer, greeter}
	for _, tkAgent := range toolkitAgentReg.All() {
		subAgents = append(subAgents, tkAgent)
	}

	// classifier and entityExtractor are built per-invocation to include entity context
	pipeline, err := agent.New(agent.Config{
		Name:        "ResearchAssistant",
		Description: "A research assistant that searches docs and strategies, then summarizes or executes.",
		SubAgents:   subAgents,
		Run: func(ctx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return runPipeline(ctx, m, toolkitAgentReg, parallelIntake, pivotIntake, orchestrator, pivotOrchestrator, synthesizer, greeter)
		},
	})
	if err != nil {
		return nil, err
	}

	return pipeline, nil
}

func runPipeline(
	ctx agent.InvocationContext,
	m model.LLM, toolkitAgentReg *toolkit.ToolkitAgentRegistry,
	parallelIntake, pivotIntake, orchestrator, pivotOrchestrator, synthesizer agent.Agent,
	greeter agent.Agent,
) iter.Seq2[*session.Event, error] {
	return func(yield func(*session.Event, error) bool) {
		userMessage := getLastUserMessage(ctx)
		if isGreeting(userMessage) && ctx.Session().Events().Len() <= 1 {
			log.Printf("[PIPELINE] Detected greeting, routing to Greeter")
			for event, err := range greeter.Run(ctx) {
				if !yield(event, err) {
					return
				}
			}
			return
		}

		// Get fresh toolkit registry for classifier (reflects hot-loaded changes)
		tkReg := toolkitAgentReg.Configs()
		classifier, err := buildIntakeClassifier(m, ctx, tkReg)
		if err != nil {
			log.Printf("[PIPELINE] Failed to build classifier: %v", err)
			return
		}

		log.Printf("[PIPELINE] Running intake classifier")
		var classificationOutput string
		var classifierErr error
		for event, err := range classifier.Run(ctx) {
			if err != nil {
				classifierErr = err
				if !yield(event, err) {
					return
				}
				continue
			}

			if event.Content != nil {
				for _, part := range event.Content.Parts {
					if part.Text != "" {
						classificationOutput = part.Text
					}
				}
			}

			if !yield(event, nil) {
				return
			}
		}

		// If classifier failed completely with no output, we can't proceed safely
		if classificationOutput == "" && classifierErr != nil {
			log.Printf("[PIPELINE] Classifier failed with no output: %v", classifierErr)
			return
		}

		var classification IntakeClassification
		if err := json.Unmarshal([]byte(extractJSON(classificationOutput)), &classification); err != nil {
			log.Printf("[PIPELINE] Failed to parse classification: %v, defaulting to new_query", err)
			classification = IntakeClassification{Classification: "new_query"}
		}

		log.Printf("[PIPELINE] Classification: %s (reason: %s)", classification.Classification, classification.Reasoning)
		if len(classification.ResolvedReferences) > 0 {
			log.Printf("[PIPELINE] Resolved references: %+v", classification.ResolvedReferences)
		}

		executedToolkit := false // track for post-processing entity extraction
		runFullPath := false

		switch classification.Classification {
		case "followup":
			// FAST PATH: Skip intake, go directly to active toolkit
			log.Printf("[PIPELINE] -> Fast Path (followup to current toolkit)")
			activeToolkitName := getActiveToolkitName(ctx)
			if activeToolkitName != "" {
				toolkitAgent, ok := toolkitAgentReg.Get(activeToolkitName)
				if !ok {
					log.Printf("[PIPELINE] Unknown active toolkit %q, falling back to full path", activeToolkitName)
					runFullPath = true
				} else {
					log.Printf("[PIPELINE] -> %s toolkit (continuing)", activeToolkitName)
					for event, err := range toolkitAgent.Run(ctx) {
						if !yield(event, err) {
							return
						}
					}
					executedToolkit = true
				}
			} else {
				// No active toolkit - this can happen if the prior request didn't complete
				// Fall back to full path to properly route the request
				log.Printf("[PIPELINE] No active toolkit for followup, falling back to full path")
				runFullPath = true
			}

		case "pivot":
			// PIVOT PATH: Run guardrails + strategy in parallel, then merge context
			log.Printf("[PIPELINE] -> Pivot Path (target toolkit: %s)", classification.TargetToolkit)

			log.Printf("[PIPELINE] Running pivot intake (guardrails + strategy)")
			var pivotGuardrailsOutput string
			for event, err := range pivotIntake.Run(ctx) {
				if err != nil {
					if !yield(event, err) {
						return
					}
					continue
				}

				if event.Author == "PivotGuardrailsAgent" && event.Content != nil {
					for _, part := range event.Content.Parts {
						if part.Text != "" {
							pivotGuardrailsOutput = part.Text
						}
					}
				}

				if !yield(event, nil) {
					return
				}
			}

			if blocked := checkGuardrailsFromOutput(pivotGuardrailsOutput, yield); blocked {
				log.Printf("[PIPELINE] Pivot blocked by guardrails")
				return
			}

			log.Printf("[PIPELINE] Running pivot orchestrator")
			var pivotDecisionOutput string
			for event, err := range pivotOrchestrator.Run(ctx) {
				if err != nil {
					if !yield(event, err) {
						return
					}
					continue
				}

				if event.Content != nil {
					for _, part := range event.Content.Parts {
						if part.Text != "" {
							pivotDecisionOutput = part.Text
						}
					}
				}

				if !yield(event, nil) {
					return
				}
			}

			var pivotDecision OrchestratorDecision
			if err := json.Unmarshal([]byte(extractJSON(pivotDecisionOutput)), &pivotDecision); err != nil {
				log.Printf("[PIPELINE] Failed to parse pivot decision: %v, using target from classifier", err)
				pivotDecision.Decision = "execute"
				pivotDecision.SelectedToolkit = classification.TargetToolkit
			}

			log.Printf("[PIPELINE] Pivot decision: %s (toolkit: %s)", pivotDecision.Decision, pivotDecision.SelectedToolkit)

			if pivotDecision.Decision == "blocked" {
				log.Printf("[PIPELINE] Pivot blocked by orchestrator: %s", pivotDecision.Reasoning)
				return
			}

			selectedToolkit := pivotDecision.SelectedToolkit
			if selectedToolkit == "" {
				selectedToolkit = classification.TargetToolkit
			}

			if selectedToolkit == "" {
				log.Printf("[PIPELINE] No valid target toolkit, running full path")
				runFullPath = true
			} else {
				tk, ok := toolkitAgentReg.Get(selectedToolkit)
				if !ok {
					log.Printf("[PIPELINE] Unknown toolkit %q, running full path", selectedToolkit)
					runFullPath = true
				} else {
					log.Printf("[PIPELINE] -> %s toolkit (pivot)", selectedToolkit)
					for event, err := range tk.Run(ctx) {
						if !yield(event, err) {
							return
						}
					}
					executedToolkit = true
				}
			}

		default: // "new_query"
			runFullPath = true
		}

		if runFullPath {
			// FULL PATH: parallel intake (guardrails + search) -> check guardrails -> orchestrator -> route
			log.Printf("[PIPELINE] -> Full Path (new query)")

			// Capture from event stream since state isn't committed yet
			log.Printf("[PIPELINE] Running parallel intake (guardrails + searches)")
			var guardrailsOutput string
			for event, err := range parallelIntake.Run(ctx) {
				if err != nil {
					if !yield(event, err) {
						return
					}
					continue
				}

				if event.Author == "GuardrailsAgent" && event.Content != nil {
					for _, part := range event.Content.Parts {
						if part.Text != "" {
							guardrailsOutput = part.Text
						}
					}
				}

				if !yield(event, nil) {
					return
				}
			}

			if blocked := checkGuardrailsFromOutput(guardrailsOutput, yield); blocked {
				log.Printf("[PIPELINE] Blocked by guardrails")
				return
			}

			log.Printf("[PIPELINE] Running orchestrator")
			var orchestratorOutput string
			for event, err := range orchestrator.Run(ctx) {
				if err != nil {
					if !yield(event, err) {
						return
					}
					continue
				}

				if event.Content != nil {
					for _, part := range event.Content.Parts {
						if part.Text != "" {
							orchestratorOutput = part.Text
						}
					}
				}

				if !yield(event, nil) {
					return
				}
			}

			var decision OrchestratorDecision
			if err := json.Unmarshal([]byte(extractJSON(orchestratorOutput)), &decision); err != nil {
				log.Printf("[PIPELINE] Failed to parse orchestrator decision: %v, defaulting to summarize", err)
				decision.Decision = "summarize"
			}

			log.Printf("[PIPELINE] Orchestrator decision: %s (toolkit: %s, chain: %v)", decision.Decision, decision.SelectedToolkit, decision.ToolkitSequence)

			switch decision.Decision {
			case "execute":
				selectedToolkit := decision.SelectedToolkit
				if selectedToolkit == "" {
					selectedToolkit = "analysis" // default
				}

				tk, ok := toolkitAgentReg.Get(selectedToolkit)
				if !ok {
					// Try analysis as fallback, but verify it exists
					tk, ok = toolkitAgentReg.Get("analysis")
					if !ok {
						log.Printf("[PIPELINE] Unknown toolkit %q and no analysis fallback, using synthesizer", selectedToolkit)
						for event, err := range synthesizer.Run(ctx) {
							if !yield(event, err) {
								return
							}
						}
						break
					}
					log.Printf("[PIPELINE] Unknown toolkit %q, falling back to analysis", selectedToolkit)
					selectedToolkit = "analysis"
				}

				log.Printf("[PIPELINE] -> %s toolkit", selectedToolkit)
				for event, err := range tk.Run(ctx) {
					if !yield(event, err) {
						return
					}
				}
				executedToolkit = true

			case "execute_chain":
				log.Printf("[PIPELINE] -> Chain execution: %v", decision.ToolkitSequence)

				for i, tkName := range decision.ToolkitSequence {
					tk, ok := toolkitAgentReg.Get(tkName)
					if !ok {
						log.Printf("[PIPELINE] Unknown toolkit %q in chain, skipping", tkName)
						continue
					}

					// Update orchestrator decision state with current toolkit's hint
					hint := decision.ExecutionHints[tkName]
					chainDecisionEvent := &session.Event{
						Author: "Orchestrator",
						Actions: session.EventActions{
							StateDelta: map[string]any{
								state.OrchestratorDecision.Key: fmt.Sprintf(`{"decision":"execute","selected_toolkit":"%s","execution_hint":"%s","reasoning":"chain step %d of %d"}`, tkName, hint, i+1, len(decision.ToolkitSequence)),
							},
						},
					}
					yield(chainDecisionEvent, nil)

					log.Printf("[PIPELINE] -> Chain step %d/%d: %s toolkit", i+1, len(decision.ToolkitSequence), tkName)
					for event, err := range tk.Run(ctx) {
						if !yield(event, err) {
							return
						}
					}

					// Run entity extraction after each toolkit (except the last)
					// so that the next toolkit can reference entities from this one
					if i < len(decision.ToolkitSequence)-1 {
						log.Printf("[PIPELINE] Running entity extraction between chain steps")
						runEntityExtraction(ctx, m, yield)
					}
				}
				executedToolkit = true

			default: // "summarize"
				log.Printf("[PIPELINE] -> Synthesizer")
				for event, err := range synthesizer.Run(ctx) {
					if !yield(event, err) {
						return
					}
				}
			}
		}

		// Post-execution: Run entity extraction if we executed a toolkit
		if executedToolkit {
			log.Printf("[PIPELINE] Running final entity extraction")
			runEntityExtraction(ctx, m, yield)
		}
	}
}

// runEntityExtraction runs the entity extractor and updates the entity index.
// This is called after toolkit execution to make results available for reference.
func runEntityExtraction(ctx agent.InvocationContext, m model.LLM, yield func(*session.Event, error) bool) {
	entityExtractor, err := buildEntityExtractor(m)
	if err != nil {
		log.Printf("[PIPELINE] Failed to build entity extractor: %v", err)
		return
	}

	var extractorOutput string
	for event, err := range entityExtractor.Run(ctx) {
		if err != nil {
			log.Printf("[PIPELINE] Entity extractor error: %v", err)
			continue
		}

		if event.Content != nil {
			for _, part := range event.Content.Parts {
				if part.Text != "" {
					extractorOutput = part.Text
				}
			}
		}

		// Don't yield extractor events to user (internal)
	}

	if extractorOutput != "" {
		var extracted ExtractedEntities
		if err := json.Unmarshal([]byte(extractJSON(extractorOutput)), &extracted); err != nil {
			log.Printf("[PIPELINE] Failed to parse extracted entities: %v", err)
		} else {
			updateEntityIndex(ctx, &extracted, yield)
		}
	}
}

// checkGuardrailsFromOutput returns true if blocked, false if passed.
func checkGuardrailsFromOutput(guardrailsOutput string, yield func(*session.Event, error) bool) bool {
	if guardrailsOutput == "" {
		log.Printf("[GUARDRAILS] No output captured, assuming passed")
		return false
	}

	var result GuardrailsResult
	if err := json.Unmarshal([]byte(extractJSON(guardrailsOutput)), &result); err != nil {
		log.Printf("[GUARDRAILS] Failed to parse result: %v, assuming passed", err)
		return false
	}

	log.Printf("[GUARDRAILS] Result: passed=%v category=%s risk=%s", result.Passed, result.Category, result.RiskLevel)

	if result.Passed {
		return false
	}

	log.Printf("[GUARDRAILS] BLOCKED: %s", result.Reason)

	blockMessage := buildBlockMessage(result)
	blockEvent := &session.Event{
		Author: "GuardrailsAgent",
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Role:  "model",
				Parts: []*genai.Part{{Text: blockMessage}},
			},
			TurnComplete: true,
		},
	}

	yield(blockEvent, nil)
	return true
}

func buildBlockMessage(result GuardrailsResult) string {
	if result.Category == "out_of_scope" {
		return fmt.Sprintf("That's outside my capabilities as a research assistant. %s\n\nI can help you:\n- Research and learn about topics\n- Generate analysis reports\n- Compare information from different sources\n- Extract insights from data", result.Reason)
	}
	return fmt.Sprintf("I can't help with that request. %s", result.Reason)
}

func getActiveToolkitName(ctx agent.InvocationContext) string {
	sessionState := ctx.Session().State()
	if sessionState == nil {
		return ""
	}

	decisionVal, err := sessionState.Get(state.OrchestratorDecision.Key)
	if err != nil {
		return ""
	}

	decisionStr, ok := decisionVal.(string)
	if !ok || decisionStr == "" {
		return ""
	}

	var decision OrchestratorDecision
	if err := json.Unmarshal([]byte(decisionStr), &decision); err != nil {
		return ""
	}

	if decision.Decision != "execute" {
		return ""
	}

	if decision.SelectedToolkit == "" {
		return "analysis" // default
	}
	return decision.SelectedToolkit
}

func getLastUserMessage(ctx agent.InvocationContext) string {
	events := ctx.Session().Events()
	for i := events.Len() - 1; i >= 0; i-- {
		event := events.At(i)
		if event.Content != nil && event.Content.Role == "user" {
			for _, part := range event.Content.Parts {
				if part.Text != "" {
					return part.Text
				}
			}
		}
	}
	return ""
}

func isGreeting(msg string) bool {
	msg = strings.ToLower(strings.TrimSpace(msg))
	greetings := []string{"hi", "hello", "hey", "greetings", "howdy", "sup", "yo"}
	for _, g := range greetings {
		if msg == g || strings.HasPrefix(msg, g+" ") || strings.HasPrefix(msg, g+"!") || strings.HasPrefix(msg, g+",") {
			return true
		}
	}
	return false
}

// extractJSON extracts JSON from a string that may be wrapped in markdown code blocks.
// Handles formats like: ```json\n{...}\n``` or ```\n{...}\n``` or raw JSON.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)

	// Check for ```json or ``` prefix
	if strings.HasPrefix(s, "```") {
		// Find end of first line (after ```json or ```)
		firstNewline := strings.Index(s, "\n")
		if firstNewline != -1 {
			s = s[firstNewline+1:]
		}

		// Find closing ```
		if idx := strings.LastIndex(s, "```"); idx != -1 {
			s = s[:idx]
		}

		s = strings.TrimSpace(s)
	}

	return s
}
