package main

import (
	"testing"
)

func TestGetNextTask_NoPlan(t *testing.T) {
	task := getNextTask(nil)
	if task != nil {
		t.Errorf("Expected nil for nil plan, got %v", task)
	}
}

func TestGetNextTask_EmptyPlan(t *testing.T) {
	plan := &TaskPlan{
		ID:    "plan_1",
		Tasks: []PlanTask{},
	}
	task := getNextTask(plan)
	if task != nil {
		t.Errorf("Expected nil for empty plan, got %v", task)
	}
}

func TestGetNextTask_SinglePendingTask(t *testing.T) {
	plan := &TaskPlan{
		ID: "plan_1",
		Tasks: []PlanTask{
			{ID: "task_1", Description: "First task", Status: TaskStatusPending, DependsOn: []string{}},
		},
	}

	task := getNextTask(plan)
	if task == nil {
		t.Fatal("Expected a task, got nil")
	}
	if task.ID != "task_1" {
		t.Errorf("Expected task_1, got %s", task.ID)
	}
}

func TestGetNextTask_SkipsCompletedTask(t *testing.T) {
	plan := &TaskPlan{
		ID: "plan_1",
		Tasks: []PlanTask{
			{ID: "task_1", Description: "First task", Status: TaskStatusCompleted, DependsOn: []string{}},
			{ID: "task_2", Description: "Second task", Status: TaskStatusPending, DependsOn: []string{}},
		},
	}

	task := getNextTask(plan)
	if task == nil {
		t.Fatal("Expected a task, got nil")
	}
	if task.ID != "task_2" {
		t.Errorf("Expected task_2, got %s", task.ID)
	}
}

func TestGetNextTask_RespectsDepenency(t *testing.T) {
	plan := &TaskPlan{
		ID: "plan_1",
		Tasks: []PlanTask{
			{ID: "task_1", Description: "First task", Status: TaskStatusPending, DependsOn: []string{}},
			{ID: "task_2", Description: "Second task", Status: TaskStatusPending, DependsOn: []string{"task_1"}},
		},
	}

	task := getNextTask(plan)
	if task == nil {
		t.Fatal("Expected a task, got nil")
	}
	if task.ID != "task_1" {
		t.Errorf("Expected task_1 (task_2 depends on it), got %s", task.ID)
	}
}

func TestGetNextTask_DependencySatisfied(t *testing.T) {
	plan := &TaskPlan{
		ID: "plan_1",
		Tasks: []PlanTask{
			{ID: "task_1", Description: "First task", Status: TaskStatusCompleted, DependsOn: []string{}},
			{ID: "task_2", Description: "Second task", Status: TaskStatusPending, DependsOn: []string{"task_1"}},
		},
	}

	task := getNextTask(plan)
	if task == nil {
		t.Fatal("Expected a task, got nil")
	}
	if task.ID != "task_2" {
		t.Errorf("Expected task_2 (dependency satisfied), got %s", task.ID)
	}
}

func TestGetNextTask_BlockedByDependency(t *testing.T) {
	plan := &TaskPlan{
		ID: "plan_1",
		Tasks: []PlanTask{
			{ID: "task_1", Description: "First task", Status: TaskStatusInProgress, DependsOn: []string{}},
			{ID: "task_2", Description: "Second task", Status: TaskStatusPending, DependsOn: []string{"task_1"}},
		},
	}

	task := getNextTask(plan)
	// task_1 is in progress (not completed), so task_2 is blocked
	// And task_1 is not pending, so it shouldn't be returned either
	if task != nil {
		t.Errorf("Expected nil (task_2 blocked, task_1 in progress), got %s", task.ID)
	}
}

func TestResolveTaskParameters_NoPlaceholders(t *testing.T) {
	task := &PlanTask{
		ID: "task_1",
		Parameters: map[string]interface{}{
			"topic":  "cloud computing",
			"format": "detailed",
		},
	}
	plan := &TaskPlan{ID: "plan_1", Tasks: []PlanTask{*task}}

	resolved := resolveTaskParameters(task, plan, nil)

	if resolved["topic"] != "cloud computing" {
		t.Errorf("Expected 'cloud computing', got %v", resolved["topic"])
	}
	if resolved["format"] != "detailed" {
		t.Errorf("Expected 'detailed', got %v", resolved["format"])
	}
}

func TestResolveTaskParameters_WithTaskReference(t *testing.T) {
	plan := &TaskPlan{
		ID: "plan_1",
		Tasks: []PlanTask{
			{ID: "task_1", ResultRef: "entity_abc123", Status: TaskStatusCompleted},
			{
				ID: "task_2",
				Parameters: map[string]interface{}{
					"content_ref": "{{task_1.result}}",
				},
			},
		},
	}

	task := &plan.Tasks[1]
	resolved := resolveTaskParameters(task, plan, nil)

	if resolved["content_ref"] != "entity_abc123" {
		t.Errorf("Expected 'entity_abc123', got %v", resolved["content_ref"])
	}
}

func TestResolveTaskParameters_MissingTaskReference(t *testing.T) {
	plan := &TaskPlan{
		ID: "plan_1",
		Tasks: []PlanTask{
			{ID: "task_1", Status: TaskStatusPending}, // No ResultRef
			{
				ID: "task_2",
				Parameters: map[string]interface{}{
					"content_ref": "{{task_1.result}}",
				},
			},
		},
	}

	task := &plan.Tasks[1]
	resolved := resolveTaskParameters(task, plan, nil)

	// Should fall back to "latest" when task result not found
	if resolved["content_ref"] != "latest" {
		t.Errorf("Expected 'latest' fallback, got %v", resolved["content_ref"])
	}
}

func TestResolveTaskParameters_WithEntityReference(t *testing.T) {
	entityIndex := &EntityIndex{
		Entities: map[string]*EntityReference{
			"entity_xyz": {ID: "entity_xyz", Label: "Test Entity"},
		},
	}

	task := &PlanTask{
		ID: "task_1",
		Parameters: map[string]interface{}{
			"source": "{{entity_xyz}}",
		},
	}
	plan := &TaskPlan{ID: "plan_1", Tasks: []PlanTask{*task}}

	resolved := resolveTaskParameters(task, plan, entityIndex)

	if resolved["source"] != "entity_xyz" {
		t.Errorf("Expected 'entity_xyz', got %v", resolved["source"])
	}
}

func TestResolveTaskParameters_NilParameters(t *testing.T) {
	task := &PlanTask{
		ID:         "task_1",
		Parameters: nil,
	}
	plan := &TaskPlan{ID: "plan_1", Tasks: []PlanTask{*task}}

	resolved := resolveTaskParameters(task, plan, nil)

	if resolved == nil {
		t.Error("Expected empty map, got nil")
	}
	if len(resolved) != 0 {
		t.Errorf("Expected empty map, got %v", resolved)
	}
}

func TestInitializePlan(t *testing.T) {
	plan := &TaskPlan{
		Summary: "Test plan",
		Tasks: []PlanTask{
			{ID: "task_1", Description: "First task"},
			{ID: "task_2", Description: "Second task"},
		},
	}

	initializePlan(plan, "original request")

	if plan.ID == "" {
		t.Error("Expected plan ID to be set")
	}
	if plan.OriginalRequest != "original request" {
		t.Errorf("Expected original request to be set, got %s", plan.OriginalRequest)
	}
	if plan.CreatedAt == "" {
		t.Error("Expected created_at to be set")
	}
	if plan.Status != TaskStatusPending {
		t.Errorf("Expected status pending, got %s", plan.Status)
	}
	if plan.CurrentTaskIdx != 0 {
		t.Errorf("Expected current_task_idx 0, got %d", plan.CurrentTaskIdx)
	}

	// Check task statuses
	for _, task := range plan.Tasks {
		if task.Status != TaskStatusPending {
			t.Errorf("Expected task %s status pending, got %s", task.ID, task.Status)
		}
	}
}

func TestInitializePlan_PreservesExistingID(t *testing.T) {
	plan := &TaskPlan{
		ID:      "plan_existing",
		Summary: "Test plan",
		Tasks:   []PlanTask{},
	}

	initializePlan(plan, "original request")

	if plan.ID != "plan_existing" {
		t.Errorf("Expected existing ID to be preserved, got %s", plan.ID)
	}
}

func TestBuildExecutionHint(t *testing.T) {
	task := &PlanTask{
		ID:          "task_1",
		Description: "Generate a report on cloud computing",
		Action:      "generate_report",
	}
	params := map[string]interface{}{
		"topic":  "cloud computing",
		"format": "detailed",
	}

	hint := buildExecutionHint(task, params)

	if hint == "" {
		t.Error("Expected non-empty hint")
	}
	// Should contain the description
	if !contains(hint, "Generate a report on cloud computing") {
		t.Errorf("Expected hint to contain description, got: %s", hint)
	}
	// Should contain the action
	if !contains(hint, "generate_report") {
		t.Errorf("Expected hint to contain action, got: %s", hint)
	}
}

func TestEscapeJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`simple`, `simple`},
		{`with "quotes"`, `with \"quotes\"`},
		{`with\backslash`, `with\\backslash`},
		{"with\nnewline", `with\nnewline`},
		{"with\ttab", `with\ttab`},
		{"with\rcarriage", `with\rcarriage`},
	}

	for _, tt := range tests {
		result := escapeJSON(tt.input)
		if result != tt.expected {
			t.Errorf("escapeJSON(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

func TestGeneratePlanID(t *testing.T) {
	id1 := generatePlanID()

	if id1 == "" {
		t.Error("Expected non-empty plan ID")
	}
	if !startsWith(id1, "plan_") {
		t.Errorf("Expected plan ID to start with 'plan_', got %s", id1)
	}

	// Generate multiple IDs and check they all have the correct prefix
	// (uniqueness is probabilistic with nanosecond timestamps)
	for i := 0; i < 10; i++ {
		id := generatePlanID()
		if !startsWith(id, "plan_") {
			t.Errorf("Expected plan ID to start with 'plan_', got %s", id)
		}
	}
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func startsWith(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
