// Package state defines constants for session state keys used across the pipeline.
// This provides a single source of truth for state key names, preventing
// stringly-typed errors when agents read/write state.
package state

// StateKey represents a session state key with its name and description.
type StateKey struct {
	Key         string
	Description string
}

// State keys used by the pipeline.
var (
	// IntakeClassification holds the classification result from the intake classifier.
	IntakeClassification = StateKey{
		Key:         "intake_classification",
		Description: "Classification of the user's message (new_query, followup, pivot)",
	}

	// GuardrailsResult holds the safety check result.
	GuardrailsResult = StateKey{
		Key:         "guardrails_result",
		Description: "Safety and policy compliance check result",
	}

	// DocResults holds documentation search results.
	DocResults = StateKey{
		Key:         "doc_results",
		Description: "Documentation search results from DocSearchAgent",
	}

	// StrategyResults holds strategy search results.
	StrategyResults = StateKey{
		Key:         "strategy_results",
		Description: "Matched strategies and capabilities from StrategySearchAgent",
	}

	// OrchestratorDecision holds the orchestrator's routing decision.
	OrchestratorDecision = StateKey{
		Key:         "orchestrator_decision",
		Description: "Orchestrator decision with selected toolkit and execution hints",
	}

	// FinalResponse holds the final response to show the user.
	FinalResponse = StateKey{
		Key:         "final_response",
		Description: "Final response content from toolkit or synthesizer",
	}

	// EntityIndex holds the entity reference index for the session.
	EntityIndex = StateKey{
		Key:         "entity_index",
		Description: "Index of referenceable entities from previous turns (reports, topics, etc.)",
	}

	// ExtractedEntities holds newly extracted entities from the current turn.
	ExtractedEntities = StateKey{
		Key:         "extracted_entities",
		Description: "Newly extracted entities from entity extractor",
	}
)

// AllKeys returns all defined state keys.
func AllKeys() []StateKey {
	return []StateKey{
		IntakeClassification,
		GuardrailsResult,
		DocResults,
		StrategyResults,
		OrchestratorDecision,
		FinalResponse,
		EntityIndex,
		ExtractedEntities,
	}
}

// KeysByName returns a map of key name to StateKey for lookups.
func KeysByName() map[string]StateKey {
	keys := AllKeys()
	m := make(map[string]StateKey, len(keys))
	for _, k := range keys {
		m[k.Key] = k
	}
	return m
}

// FormatStateAccess generates a documentation string for accessing specific state keys.
func FormatStateAccess(keys []string) string {
	if len(keys) == 0 {
		return ""
	}

	keyMap := KeysByName()
	var result string
	result = "You have access to the following state:\n"

	for _, keyName := range keys {
		if sk, ok := keyMap[keyName]; ok {
			result += "- " + sk.Key + ": " + sk.Description + "\n"
		} else {
			result += "- " + keyName + ": (unknown state key)\n"
		}
	}

	return result
}
