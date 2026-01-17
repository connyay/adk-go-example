package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/connyay/adk-go-example/state"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/session"
)

func getEntityIndex(ctx agent.InvocationContext) *EntityIndex {
	sessionState := ctx.Session().State()
	if sessionState == nil {
		return newEntityIndex()
	}

	indexVal, err := sessionState.Get(state.EntityIndex.Key)
	if err != nil {
		return newEntityIndex()
	}

	indexStr, ok := indexVal.(string)
	if !ok || indexStr == "" {
		return newEntityIndex()
	}

	var index EntityIndex
	if err := json.Unmarshal([]byte(indexStr), &index); err != nil {
		return newEntityIndex()
	}

	return &index
}

func newEntityIndex() *EntityIndex {
	return &EntityIndex{
		Entities: make(map[string]*EntityReference),
		Aliases:  make(map[string]string),
	}
}

// formatEntityIndexForPrompt creates a string representation for LLM prompts
func formatEntityIndexForPrompt(index *EntityIndex) string {
	if len(index.Entities) == 0 {
		return "No entities from previous turns."
	}

	var sb strings.Builder
	sb.WriteString("Known entities from previous turns:\n")
	for _, entity := range index.Entities {
		sb.WriteString(fmt.Sprintf("- [%s] %s (type: %s, aliases: %v)\n",
			entity.ID, entity.Label, entity.Type, entity.Aliases))
	}
	if index.DefaultReferent != "" {
		if entity, ok := index.Entities[index.DefaultReferent]; ok {
			sb.WriteString(fmt.Sprintf("\nDefault referent (for 'it', 'that', 'this'): %s (%s)\n",
				entity.Label, entity.ID))
		}
	}
	return sb.String()
}

func updateEntityIndex(ctx agent.InvocationContext, extracted *ExtractedEntities, yield func(*session.Event, error) bool) {
	if len(extracted.NewEntities) == 0 {
		log.Printf("[ENTITY] No new entities to store")
		return
	}

	index := getEntityIndex(ctx)
	index.TurnCount++

	for _, entity := range extracted.NewEntities {
		entity.SourceTurn = index.TurnCount
		entityCopy := entity // avoid loop variable capture
		index.Entities[entity.ID] = &entityCopy

		for _, alias := range entity.Aliases {
			index.Aliases[strings.ToLower(alias)] = entity.ID
		}

		log.Printf("[ENTITY] Stored: %s (%s) with aliases %v", entity.Label, entity.ID, entity.Aliases)
	}

	if extracted.DefaultReferent != "" {
		index.DefaultReferent = extracted.DefaultReferent
		log.Printf("[ENTITY] Default referent: %s", extracted.DefaultReferent)
	}

	indexJSON, err := json.Marshal(index)
	if err != nil {
		log.Printf("[ENTITY] Failed to serialize index: %v", err)
		return
	}

	stateEvent := &session.Event{
		Author: "EntityExtractor",
		Actions: session.EventActions{
			StateDelta: map[string]any{
				state.EntityIndex.Key: string(indexJSON),
			},
		},
	}

	yield(stateEvent, nil)
	log.Printf("[ENTITY] Updated entity index with %d entities", len(index.Entities))
}
