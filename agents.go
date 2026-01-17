package main

import (
	"fmt"

	"github.com/connyay/adk-go-example/state"
	"github.com/connyay/adk-go-example/toolkit"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

func buildIntakeClassifier(m model.LLM, ctx agent.InvocationContext, tkReg *toolkit.ToolkitRegistry) (agent.Agent, error) {
	entityIndex := getEntityIndex(ctx)
	entityContext := formatEntityIndexForPrompt(entityIndex)
	toolkitsPrompt := tkReg.GetAvailableToolkitsPrompt()

	return llmagent.New(llmagent.Config{
		Name:        "IntakeClassifier",
		Description: "Classifies incoming messages to determine processing path",
		Model:       m,
		Instruction: fmt.Sprintf(`You are an intake classifier. Analyze the user's message in context of the conversation history.

## Entity Context
%s

%s

## Classification Rules

Classify the message into one of three categories:

1. "new_query" - Use when:
   - First message in conversation
   - Completely new topic with no connection to prior context
   - User explicitly starts fresh ("new question", "different topic")

2. "followup" - Use when:
   - User is continuing with the SAME toolkit/action type
   - Refining, confirming, or extending previous request
   - Examples: "yes", "use defaults", "make it longer", "add more detail", "ok do it"
   - Asking for more analysis when already in analysis toolkit

3. "pivot" - Use when:
   - User wants to SWITCH to a different toolkit
   - Examples of analysis -> export pivots:
     - "export that to PDF" (switching from analysis to export)
     - "create a presentation from that report" (analysis to export)
     - "download as CSV" (analysis to export)
   - Examples of export -> analysis pivots:
     - "now compare those results" (switching from export to analysis)
     - "extract insights from that" (export to analysis)
   - User references prior context but wants a fundamentally different action type

## Entity Resolution

If the user references something from a previous turn (e.g., "the report", "that topic", "those insights"):
1. Check the entity context above
2. Resolve the reference to the appropriate entity ID
3. Include in resolved_references

For pronouns like "it", "that", "this" - resolve to the default_referent if available.

## Output JSON:
{
  "classification": "new_query" | "followup" | "pivot",
  "topic": "main subject (can be 'continuation' for followups)",
  "intent": "what they want",
  "keywords": ["search", "terms"],
  "reasoning": "brief explanation",
  "target_toolkit": "analysis" | "export" (REQUIRED for pivots, empty for others),
  "resolved_references": [
    {"text": "the report", "entity_id": "entity_123", "confidence": 0.95}
  ],
  "unresolved_references": ["unknown thing"]
}

CRITICAL RULES:
- Short confirmations like "yes", "ok", "sure", "use defaults" are ALWAYS "followup"
- "export to PDF/CSV" or "create presentation" after analysis work = "pivot" (toolkit switch)
- "generate report" or "compare" after export work = "pivot" (toolkit switch)`, entityContext, toolkitsPrompt),
		OutputKey: state.IntakeClassification.Key,
	})
}

func buildGuardrailsAgent(m model.LLM, name string, tkReg *toolkit.ToolkitRegistry) (agent.Agent, error) {
	if name == "" {
		name = "GuardrailsAgent"
	}

	toolkitsPrompt := tkReg.GetAvailableToolkitsPrompt()

	return llmagent.New(llmagent.Config{
		Name:        name,
		Description: "Checks messages for safety and policy compliance",
		Model:       m,
		Instruction: fmt.Sprintf(`You are a safety and policy guardrails checker. Analyze the user's message for potential issues.

Check for:
1. **Harmful content**: Requests for dangerous, illegal, or harmful actions
2. **Out of scope**: Requests completely unrelated to our capabilities
3. **Ambiguous intent**: Unclear what the user wants

## Our Capabilities (all are IN SCOPE)

%s

Any request that uses these capabilities is IN SCOPE and should pass.
General learning/research questions are also IN SCOPE.

## Output JSON

{
  "passed": true | false,
  "reason": "explanation if not passed",
  "risk_level": "none" | "low" | "medium" | "high",
  "category": "safe" | "harmful" | "out_of_scope" | "ambiguous"
}

## Rules

- category "safe" → passed: true
- category "harmful" → passed: false
- category "out_of_scope" → passed: false
- category "ambiguous" → passed: true (give benefit of doubt)

## OUT OF SCOPE examples (passed: false)

- "What's the weather?" (unrelated to our toolkits)
- "Write me a poem" (unrelated)
- "Help me with my code" (unrelated)
- "Calculate 2+2" (unrelated)`, toolkitsPrompt),
		OutputKey: state.GuardrailsResult.Key,
	})
}

func buildDocSearchAgent(m model.LLM) (agent.Agent, error) {
	docTool, err := functiontool.New(
		functiontool.Config{
			Name:        "search_docs",
			Description: "Search documentation and knowledge base for information on a topic",
		},
		searchDocs,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create doc tool: %w", err)
	}

	return llmagent.New(llmagent.Config{
		Name:        "DocSearchAgent",
		Description: "Searches documentation for relevant information",
		Model:       m,
		Instruction: `You are a documentation search agent. Use the search_docs tool to find relevant documentation.

Based on the user's query, call search_docs with:
- query: the main topic or question
- categories: relevant categories (concepts, tutorials, reference, examples)
- max_results: 3-5 documents

After getting results, briefly summarize what documentation is available.`,
		Tools:     []tool.Tool{docTool},
		OutputKey: state.DocResults.Key,
	})
}

func buildStrategySearchAgent(m model.LLM, name string) (agent.Agent, error) {
	strategyTool, err := functiontool.New(
		functiontool.Config{
			Name:        "search_strategies",
			Description: "Search for actionable strategies and capabilities that can be executed",
		},
		searchStrategies,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create strategy tool: %w", err)
	}

	if name == "" {
		name = "StrategySearchAgent"
	}
	return llmagent.New(llmagent.Config{
		Name:        name,
		Description: "Searches for actionable strategies based on user intent",
		Model:       m,
		Instruction: `You are a strategy search agent. Use the search_strategies tool to find actionable capabilities.

Based on the user's query, call search_strategies with:
- query: what the user wants to accomplish
- domain: best matching domain (analysis, creation, transformation, general)

After getting results, summarize what strategies/capabilities are available to address the user's needs.
If no strategies match, indicate that this appears to be an informational query.`,
		Tools:     []tool.Tool{strategyTool},
		OutputKey: state.StrategyResults.Key,
	})
}

func buildOrchestratorAgent(m model.LLM, tkReg *toolkit.ToolkitRegistry) (agent.Agent, error) {
	toolkitsPrompt := tkReg.GetAvailableToolkitsPrompt()
	stateDoc := state.FormatStateAccess([]string{state.DocResults.Key, state.StrategyResults.Key})

	return llmagent.New(llmagent.Config{
		Name:        "Orchestrator",
		Description: "Decides whether to summarize information or execute a toolkit action",
		Model:       m,
		Instruction: fmt.Sprintf(`You are an orchestrator that decides the next step based on search results.

%s

Analyze both and decide:

1. If the user just wants INFORMATION (learn about something, understand a concept):
   → Decision: "summarize"
   → You will synthesize the docs and provide an informative answer

2. If the user wants to DO ONE thing with a SINGLE toolkit:
   → Decision: "execute"
   → Select the appropriate toolkit based on matched strategies

3. If the user wants MULTIPLE actions spanning DIFFERENT toolkits (e.g., "generate report AND export to PDF"):
   → Decision: "execute_chain"
   → Specify the toolkit sequence in order of execution
   → Provide hints for each toolkit
   → Results from earlier toolkits will be available to later ones via entity references

%s

Output a JSON object based on the decision type:

For "summarize":
{
  "decision": "summarize",
  "reasoning": "why this decision"
}

For "execute" (single toolkit):
{
  "decision": "execute",
  "reasoning": "why this decision",
  "selected_toolkit": "<toolkit_name>",
  "execution_hint": "guidance for the toolkit"
}

For "execute_chain" (multiple toolkits in sequence):
{
  "decision": "execute_chain",
  "reasoning": "why this decision",
  "toolkit_sequence": ["first_toolkit", "second_toolkit"],
  "execution_hints": {
    "first_toolkit": "what to do in first toolkit",
    "second_toolkit": "what to do with results from first toolkit"
  }
}

IMPORTANT:
- If strategies span multiple toolkits (e.g., analysis + export), use "execute_chain"
- Common chains: ["analysis", "export"] for "generate X and export to Y"
- If strategies were found, check the "toolkit" field to determine which toolkits are needed
- If no strategies matched, use "summarize"`, stateDoc, toolkitsPrompt),
		OutputKey: state.OrchestratorDecision.Key,
	})
}

func buildPivotOrchestrator(m model.LLM, tkReg *toolkit.ToolkitRegistry) (agent.Agent, error) {
	toolkitsPrompt := tkReg.GetAvailableToolkitsPrompt()
	stateDoc := state.FormatStateAccess([]string{
		state.GuardrailsResult.Key,
		state.StrategyResults.Key,
		state.IntakeClassification.Key,
		state.EntityIndex.Key,
	})

	return llmagent.New(llmagent.Config{
		Name:        "PivotOrchestrator",
		Description: "Merges carried context with new strategy for pivot operations",
		Model:       m,
		Instruction: fmt.Sprintf(`You are a pivot orchestrator. The user is switching to a different toolkit while referencing prior context.

%s

%s

Your job is to merge the carried context from the intake classification with the new strategy results.

## Context Merging Rules

1. **Resolved References**: The intake_classification contains resolved_references mapping user phrases to entity IDs.
   Use these to understand what the user is referring to (e.g., "the report" → entity_123).

2. **Entity Data**: Look up resolved entity IDs in the entity_index to get the actual data
   (report content, topic details, etc.).

3. **Strategy Selection**: Use strategy_results to determine what action to take.
   The target_toolkit from intake_classification suggests the toolkit, but verify with strategies.

4. **Execution Hint**: Create a detailed execution_hint that includes:
   - What action to perform (from strategy)
   - What content to act on (from resolved entities)
   - Any specific parameters mentioned by the user

## Output JSON

{
  "decision": "execute",
  "reasoning": "why this decision, what context was merged",
  "selected_toolkit": "<toolkit_name>",
  "execution_hint": "detailed guidance including resolved entity context",
  "merged_context": {
    "source_entities": ["entity_123"],
    "action": "what to do",
    "parameters": {}
  }
}

If guardrails failed, output:
{
  "decision": "blocked",
  "reasoning": "guardrails failure reason"
}

If no matching strategy found but target_toolkit is valid, still proceed:
{
  "decision": "execute",
  "reasoning": "no strategy match but user clearly wants toolkit X",
  "selected_toolkit": "<target_toolkit>",
  "execution_hint": "user wants to <action> with <entity context>"
}`, stateDoc, toolkitsPrompt),
		OutputKey: state.OrchestratorDecision.Key,
	})
}

func buildSynthesizerAgent(m model.LLM) (agent.Agent, error) {
	stateDoc := state.FormatStateAccess([]string{state.DocResults.Key, state.StrategyResults.Key})

	return llmagent.New(llmagent.Config{
		Name:        "Synthesizer",
		Description: "Synthesizes documentation into a helpful response",
		Model:       m,
		Instruction: fmt.Sprintf(`You are a synthesizer that creates helpful responses from documentation.

%s

Create a clear, informative response that:
1. Answers the user's question using the documentation
2. Highlights key concepts and important points
3. Suggests relevant tutorials or references if applicable
4. Mentions if there are actionable capabilities available (strategies) they might want to explore

Be concise but thorough. Format with clear sections if the topic is complex.`, stateDoc),
		OutputKey: state.FinalResponse.Key,
	})
}

func buildGreeterAgent(m model.LLM) (agent.Agent, error) {
	return llmagent.New(llmagent.Config{
		Name:        "Greeter",
		Description: "Handles greetings and unclear requests",
		Model:       m,
		Instruction: `You are a helpful research and analysis assistant. The user has sent a greeting or unclear message.

Respond warmly and explain what you can help with:

"Hello! I'm a research and analysis assistant. I can help you:

**Learn about topics:**
- 'What is quantum computing?'
- 'Explain machine learning basics'

**Create analysis and reports:**
- 'Generate a report on renewable energy trends'
- 'Compare different approaches to data storage'
- 'Extract key insights from AI research'

What would you like to explore?"

Keep it friendly and concise.`,
		OutputKey: state.FinalResponse.Key,
	})
}

func buildEntityExtractor(m model.LLM) (agent.Agent, error) {
	stateDoc := state.FormatStateAccess([]string{state.FinalResponse.Key, state.OrchestratorDecision.Key})

	return llmagent.New(llmagent.Config{
		Name:        "EntityExtractor",
		Description: "Extracts referenceable entities from execution results",
		Model:       m,
		Instruction: fmt.Sprintf(`You are an entity extractor. Analyze the execution results and extract referenceable entities.

%s

Extract entities that users might want to reference in future turns:

1. **Reports**: If a report was generated, extract it with:
   - label: descriptive name (e.g., "Cloud Computing Report")
   - type: "report"
   - aliases: ways to reference it ("the report", "cloud computing report", "that report")
   - data: {report_id, topic, sections}

2. **Topics**: Main topics that were researched/analyzed:
   - label: topic name (e.g., "cloud computing")
   - type: "topic"
   - aliases: variations ("cloud", "cloud computing", "that topic")

3. **Comparisons**: If sources were compared:
   - label: comparison description
   - type: "comparison"
   - aliases: ("the comparison", "that comparison")

4. **Insights**: If insights were extracted:
   - label: insight summary
   - type: "insights"
   - aliases: ("the insights", "those insights")

Output JSON:
{
  "new_entities": [
    {
      "id": "entity_XXX",
      "label": "descriptive name",
      "type": "report|topic|comparison|insights",
      "source_turn": 0,
      "source_toolkit": "analysis",
      "data": {},
      "aliases": ["alias1", "alias2"]
    }
  ],
  "default_referent": "entity_XXX"
}

The default_referent should be the most prominent entity (usually the main output).
Generate unique IDs like "entity_12345".
If no entities to extract, return {"new_entities": [], "default_referent": ""}`, stateDoc),
		OutputKey: state.ExtractedEntities.Key,
	})
}
