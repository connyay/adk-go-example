package main

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/connyay/adk-go-example/state"
	"github.com/connyay/adk-go-example/toolkit"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// getTaskPlan retrieves the current task plan from session state.
func getTaskPlan(ctx agent.InvocationContext) *TaskPlan {
	sessionState := ctx.Session().State()
	if sessionState == nil {
		return nil
	}

	planVal, err := sessionState.Get(state.TaskPlan.Key)
	if err != nil {
		return nil
	}

	planStr, ok := planVal.(string)
	if !ok || planStr == "" {
		return nil
	}

	var plan TaskPlan
	if err := json.Unmarshal([]byte(planStr), &plan); err != nil {
		log.Printf("[TASKS] Failed to parse task plan: %v", err)
		return nil
	}

	return &plan
}

// hasActivePlan checks if there is an incomplete plan in session state.
func hasActivePlan(ctx agent.InvocationContext) bool {
	plan := getTaskPlan(ctx)
	if plan == nil {
		return false
	}

	// Check if plan has any pending or in-progress tasks
	for _, task := range plan.Tasks {
		if task.Status == TaskStatusPending || task.Status == TaskStatusInProgress {
			return true
		}
	}

	return false
}

// getNextTask returns the next pending task that has all dependencies satisfied.
func getNextTask(plan *TaskPlan) *PlanTask {
	if plan == nil || len(plan.Tasks) == 0 {
		return nil
	}

	// Build a set of completed task IDs
	completedTasks := make(map[string]bool)
	for i := range plan.Tasks {
		if plan.Tasks[i].Status == TaskStatusCompleted {
			completedTasks[plan.Tasks[i].ID] = true
		}
	}

	// Find first pending task with all dependencies satisfied
	for i := range plan.Tasks {
		task := &plan.Tasks[i]
		if task.Status != TaskStatusPending {
			continue
		}

		// Check if all dependencies are completed
		allDepsSatisfied := true
		for _, depID := range task.DependsOn {
			if !completedTasks[depID] {
				allDepsSatisfied = false
				break
			}
		}

		if allDepsSatisfied {
			return task
		}
	}

	return nil
}

// resolveTaskParameters replaces {{task_N.result}} placeholders with actual entity IDs.
func resolveTaskParameters(task *PlanTask, plan *TaskPlan, entityIndex *EntityIndex) map[string]interface{} {
	if task.Parameters == nil {
		return make(map[string]interface{})
	}

	// Build a map of task_id -> result_ref
	taskResults := make(map[string]string)
	for _, t := range plan.Tasks {
		if t.ResultRef != "" {
			taskResults[t.ID] = t.ResultRef
		}
	}

	// Resolve placeholders in parameters
	resolved := make(map[string]interface{})
	placeholderRegex := regexp.MustCompile(`\{\{(task_\d+)\.result\}\}`)

	for key, value := range task.Parameters {
		strVal, ok := value.(string)
		if !ok {
			resolved[key] = value
			continue
		}

		// Replace {{task_N.result}} with actual entity ID
		resolvedStr := placeholderRegex.ReplaceAllStringFunc(strVal, func(match string) string {
			// Extract task_N from {{task_N.result}}
			matches := placeholderRegex.FindStringSubmatch(match)
			if len(matches) < 2 {
				return match
			}
			taskID := matches[1]

			if entityID, ok := taskResults[taskID]; ok {
				return entityID
			}

			// Fallback to "latest" if task result not found
			log.Printf("[TASKS] Could not resolve %s, using 'latest'", match)
			return "latest"
		})

		// Also resolve {{entity_XXX}} references
		entityRegex := regexp.MustCompile(`\{\{(entity_[a-zA-Z0-9]+)\}\}`)
		resolvedStr = entityRegex.ReplaceAllStringFunc(resolvedStr, func(match string) string {
			matches := entityRegex.FindStringSubmatch(match)
			if len(matches) < 2 {
				return match
			}
			entityID := matches[1]

			// Verify entity exists
			if entityIndex != nil && entityIndex.Entities != nil {
				if _, exists := entityIndex.Entities[entityID]; exists {
					return entityID
				}
			}

			log.Printf("[TASKS] Entity %s not found in index", entityID)
			return match
		})

		resolved[key] = resolvedStr
	}

	return resolved
}

// updateTaskPlanState emits a state update event for the task plan.
func updateTaskPlanState(plan *TaskPlan, yield func(*session.Event, error) bool) {
	planJSON, err := json.Marshal(plan)
	if err != nil {
		log.Printf("[TASKS] Failed to marshal task plan: %v", err)
		return
	}

	event := &session.Event{
		Author: "TaskRunner",
		Actions: session.EventActions{
			StateDelta: map[string]any{
				state.TaskPlan.Key: string(planJSON),
			},
		},
	}

	yield(event, nil)
}

// emitTaskProgress emits a progress event for the UI.
func emitTaskProgress(plan *TaskPlan, currentTask *PlanTask, yield func(*session.Event, error) bool) {
	completedCount := 0
	for _, task := range plan.Tasks {
		if task.Status == TaskStatusCompleted {
			completedCount++
		}
	}

	progress := TaskProgress{
		PlanID:          plan.ID,
		TotalTasks:      len(plan.Tasks),
		CompletedTasks:  completedCount,
		CurrentTask:     currentTask.Description,
		PercentComplete: (completedCount * 100) / len(plan.Tasks),
	}

	progressJSON, err := json.Marshal(progress)
	if err != nil {
		log.Printf("[TASKS] Failed to marshal task progress: %v", err)
		return
	}

	// Emit progress as both state update and user-visible message
	progressEvent := &session.Event{
		Author: "TaskRunner",
		Actions: session.EventActions{
			StateDelta: map[string]any{
				state.TaskProgress.Key: string(progressJSON),
			},
		},
	}
	yield(progressEvent, nil)

	// Emit user-visible progress message
	progressMsg := fmt.Sprintf("[Task %d/%d] %s", completedCount+1, len(plan.Tasks), currentTask.Description)
	msgEvent := &session.Event{
		Author: "TaskRunner",
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{
				Role:  "model",
				Parts: []*genai.Part{{Text: progressMsg}},
			},
		},
	}
	yield(msgEvent, nil)
}

// runTaskLoop executes tasks in the plan sequentially, respecting dependencies.
func runTaskLoop(
	ctx agent.InvocationContext,
	m model.LLM,
	plan *TaskPlan,
	toolkitAgentReg *toolkit.ToolkitAgentRegistry,
	yield func(*session.Event, error) bool,
) {
	log.Printf("[TASKS] Starting task loop for plan %s with %d tasks", plan.ID, len(plan.Tasks))

	// Update plan status to in_progress
	plan.Status = TaskStatusInProgress
	updateTaskPlanState(plan, yield)

	for {
		// Get next executable task
		task := getNextTask(plan)
		if task == nil {
			log.Printf("[TASKS] No more executable tasks")
			break
		}

		log.Printf("[TASKS] Executing task %s: %s (toolkit: %s)", task.ID, task.Description, task.Toolkit)

		// Emit progress
		emitTaskProgress(plan, task, yield)

		// Mark task as in progress
		task.Status = TaskStatusInProgress
		task.StartedAt = time.Now().Format(time.RFC3339)
		updateTaskPlanState(plan, yield)

		// Get entity index for parameter resolution
		entityIndex := getEntityIndex(ctx)

		// Resolve parameters
		resolvedParams := resolveTaskParameters(task, plan, entityIndex)
		log.Printf("[TASKS] Resolved parameters: %v", resolvedParams)

		// Get toolkit agent
		toolkitAgent, ok := toolkitAgentReg.Get(task.Toolkit)
		if !ok {
			log.Printf("[TASKS] Unknown toolkit %q for task %s", task.Toolkit, task.ID)
			task.Status = TaskStatusFailed
			task.Error = fmt.Sprintf("unknown toolkit: %s", task.Toolkit)
			task.CompletedAt = time.Now().Format(time.RFC3339)
			updateTaskPlanState(plan, yield)
			continue
		}

		// Set orchestrator decision state for the toolkit
		execHint := buildExecutionHint(task, resolvedParams)
		decisionEvent := &session.Event{
			Author: "TaskRunner",
			Actions: session.EventActions{
				StateDelta: map[string]any{
					state.OrchestratorDecision.Key: fmt.Sprintf(`{"decision":"execute","selected_toolkit":"%s","execution_hint":"%s","reasoning":"task plan step %s"}`, task.Toolkit, escapeJSON(execHint), task.ID),
				},
			},
		}
		yield(decisionEvent, nil)

		// Execute toolkit agent
		log.Printf("[TASKS] Running %s toolkit for task %s", task.Toolkit, task.ID)
		for event, err := range toolkitAgent.Run(ctx) {
			if err != nil {
				log.Printf("[TASKS] Toolkit error: %v", err)
			}
			if !yield(event, err) {
				// Yield returned false, stop execution
				log.Printf("[TASKS] Task loop interrupted")
				return
			}
		}

		// Run entity extraction to capture result
		log.Printf("[TASKS] Running entity extraction for task %s", task.ID)
		resultRef := runEntityExtractionForTask(ctx, m, yield)

		// Update task with result
		task.Status = TaskStatusCompleted
		task.ResultRef = resultRef
		task.CompletedAt = time.Now().Format(time.RFC3339)

		// Find task in plan and update it
		for i := range plan.Tasks {
			if plan.Tasks[i].ID == task.ID {
				plan.Tasks[i] = *task
				break
			}
		}

		plan.CurrentTaskIdx++
		updateTaskPlanState(plan, yield)

		log.Printf("[TASKS] Completed task %s with result ref: %s", task.ID, resultRef)
	}

	// Check if all tasks completed
	allCompleted := true
	for _, task := range plan.Tasks {
		if task.Status != TaskStatusCompleted {
			allCompleted = false
			break
		}
	}

	if allCompleted {
		plan.Status = TaskStatusCompleted
		log.Printf("[TASKS] Plan %s completed successfully", plan.ID)

		// Emit completion message
		completionMsg := fmt.Sprintf("Plan completed: %s (%d tasks executed)", plan.Summary, len(plan.Tasks))
		completionEvent := &session.Event{
			Author: "TaskRunner",
			LLMResponse: model.LLMResponse{
				Content: &genai.Content{
					Role:  "model",
					Parts: []*genai.Part{{Text: completionMsg}},
				},
				TurnComplete: true,
			},
		}
		yield(completionEvent, nil)
	} else {
		plan.Status = TaskStatusFailed
		log.Printf("[TASKS] Plan %s did not complete all tasks", plan.ID)
	}

	updateTaskPlanState(plan, yield)
}

// runEntityExtractionForTask runs entity extraction and returns the default referent ID.
func runEntityExtractionForTask(ctx agent.InvocationContext, m model.LLM, yield func(*session.Event, error) bool) string {
	entityExtractor, err := buildEntityExtractor(m)
	if err != nil {
		log.Printf("[TASKS] Failed to build entity extractor: %v", err)
		return ""
	}

	var extractorOutput string
	for event, err := range entityExtractor.Run(ctx) {
		if err != nil {
			log.Printf("[TASKS] Entity extractor error: %v", err)
			continue
		}

		if event.Content != nil {
			for _, part := range event.Content.Parts {
				if part.Text != "" {
					extractorOutput = part.Text
				}
			}
		}
		// Don't yield extractor events to user
	}

	if extractorOutput != "" {
		var extracted ExtractedEntities
		if err := json.Unmarshal([]byte(extractJSON(extractorOutput)), &extracted); err != nil {
			log.Printf("[TASKS] Failed to parse extracted entities: %v", err)
		} else {
			updateEntityIndex(ctx, &extracted, yield)
			return extracted.DefaultReferent
		}
	}

	return ""
}

// buildExecutionHint creates an execution hint from task parameters.
func buildExecutionHint(task *PlanTask, params map[string]interface{}) string {
	var parts []string
	parts = append(parts, task.Description)

	if task.Action != "" {
		parts = append(parts, fmt.Sprintf("Action: %s", task.Action))
	}

	// Add key parameters
	for key, value := range params {
		if strVal, ok := value.(string); ok && strVal != "" {
			parts = append(parts, fmt.Sprintf("%s: %s", key, strVal))
		}
	}

	return strings.Join(parts, "; ")
}

// escapeJSON escapes a string for use within a JSON string value.
func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// generatePlanID generates a unique plan ID.
func generatePlanID() string {
	return fmt.Sprintf("plan_%d", time.Now().UnixNano())
}

// initializePlan sets timestamps and IDs for a newly created plan.
func initializePlan(plan *TaskPlan, originalRequest string) {
	if plan.ID == "" {
		plan.ID = generatePlanID()
	}
	plan.OriginalRequest = originalRequest
	plan.CreatedAt = time.Now().Format(time.RFC3339)
	plan.Status = TaskStatusPending
	plan.CurrentTaskIdx = 0

	// Ensure all tasks have pending status
	for i := range plan.Tasks {
		if plan.Tasks[i].Status == "" {
			plan.Tasks[i].Status = TaskStatusPending
		}
	}
}
