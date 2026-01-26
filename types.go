package main

type SearchDocsInput struct {
	Query      string   `json:"query" jsonschema:"The search query"`
	Categories []string `json:"categories" jsonschema:"Categories to search: concepts, tutorials, reference, examples"`
	MaxResults int      `json:"max_results" jsonschema:"Maximum number of results to return"`
}

type SearchDocsOutput struct {
	Documents []Document `json:"documents"`
	Total     int        `json:"total"`
}

type Document struct {
	Title    string  `json:"title"`
	Category string  `json:"category"`
	Snippet  string  `json:"snippet"`
	URL      string  `json:"url"`
	Score    float64 `json:"score"`
}

type SearchStrategiesInput struct {
	Query  string `json:"query" jsonschema:"What the user wants to accomplish"`
	Domain string `json:"domain" jsonschema:"Domain hint: analysis, creation, transformation, general"`
}

type SearchStrategiesOutput struct {
	Strategies []Strategy `json:"strategies"`
	HasMatch   bool       `json:"has_match"`
}

type Strategy struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Toolkit     string   `json:"toolkit"`
	Score       float64  `json:"score"` // based on keyword match ratio
	Steps       []string `json:"steps"`
}

type GenerateReportInput struct {
	Topic       string   `json:"topic" jsonschema:"The topic to report on"`
	Sources     []string `json:"sources" jsonschema:"Source types to include: docs, strategies, both"`
	Format      string   `json:"format" jsonschema:"Report format: summary, detailed, bullet_points"`
	MaxSections int      `json:"max_sections" jsonschema:"Maximum number of sections"`
}

type GenerateReportOutput struct {
	ReportID  string   `json:"report_id"`
	Title     string   `json:"title"`
	Sections  []string `json:"sections"`
	WordCount int      `json:"word_count"`
}

type CompareSourcesInput struct {
	Topic   string   `json:"topic" jsonschema:"The topic to compare"`
	Sources []string `json:"sources" jsonschema:"Which sources to compare"`
}

type CompareSourcesOutput struct {
	Comparison  string   `json:"comparison"`
	Agreements  []string `json:"agreements"`
	Differences []string `json:"differences"`
}

type ExtractInsightsInput struct {
	Topic     string `json:"topic" jsonschema:"The topic to extract insights from"`
	MaxPoints int    `json:"max_points" jsonschema:"Maximum number of insight points"`
}

type ExtractInsightsOutput struct {
	Insights   []string `json:"insights"`
	Confidence float64  `json:"confidence"`
	Sources    int      `json:"sources_analyzed"`
}

type ExportPDFInput struct {
	Title      string `json:"title" jsonschema:"Title for the PDF"`
	ContentRef string `json:"content_ref" jsonschema:"Reference to content to export (entity ID or 'latest')"`
	IncludeTOC bool   `json:"include_toc" jsonschema:"Whether to include table of contents"`
}

type ExportPDFOutput struct {
	Filename  string `json:"filename"`
	PageCount int    `json:"page_count"`
	SizeKB    int    `json:"size_kb"`
	URL       string `json:"url"`
}

type ExportCSVInput struct {
	DataRef        string   `json:"data_ref" jsonschema:"Reference to data to export"`
	Columns        []string `json:"columns" jsonschema:"Columns to include"`
	IncludeHeaders bool     `json:"include_headers" jsonschema:"Whether to include column headers"`
}

type ExportCSVOutput struct {
	Filename string `json:"filename"`
	RowCount int    `json:"row_count"`
	SizeKB   int    `json:"size_kb"`
	URL      string `json:"url"`
}

type CreatePresentationInput struct {
	Title      string `json:"title" jsonschema:"Presentation title"`
	ContentRef string `json:"content_ref" jsonschema:"Reference to content for slides"`
	MaxSlides  int    `json:"max_slides" jsonschema:"Maximum number of slides"`
	Style      string `json:"style" jsonschema:"Presentation style: professional, casual, minimal"`
}

type CreatePresentationOutput struct {
	Filename   string   `json:"filename"`
	SlideCount int      `json:"slide_count"`
	Slides     []string `json:"slides"`
	URL        string   `json:"url"`
}

type IntakeClassification struct {
	Classification       string              `json:"classification"` // "new_query", "followup", "pivot"
	Topic                string              `json:"topic"`
	Intent               string              `json:"intent"` // what they want: learn, do, compare, etc.
	Keywords             []string            `json:"keywords"`
	Reasoning            string              `json:"reasoning"`
	TargetToolkit        string              `json:"target_toolkit"`        // for pivots: which toolkit to switch to
	ResolvedReferences   []ResolvedReference `json:"resolved_references"`   // references resolved to entities
	UnresolvedReferences []string            `json:"unresolved_references"` // references that couldn't be resolved
}

// ResolvedReference maps a text reference to an entity.
type ResolvedReference struct {
	Text       string  `json:"text"` // e.g., "the report", "that"
	EntityID   string  `json:"entity_id"`
	Confidence float64 `json:"confidence"` // 0.0-1.0
}

type OrchestratorDecision struct {
	Decision        string            `json:"decision"` // "summarize", "execute", or "execute_chain"
	Reasoning       string            `json:"reasoning"`
	SelectedToolkit string            `json:"selected_toolkit"` // if execute, which toolkit
	ExecutionHint   string            `json:"execution_hint"`   // if execute, hint for the toolkit
	ToolkitSequence []string          `json:"toolkit_sequence"` // if execute_chain, ordered list of toolkits
	ExecutionHints  map[string]string `json:"execution_hints"`  // if execute_chain, per-toolkit hints
}

// GuardrailsResult is the output from the guardrails check.
type GuardrailsResult struct {
	Passed    bool   `json:"passed"`
	Reason    string `json:"reason,omitempty"`
	RiskLevel string `json:"risk_level"` // "none", "low", "medium", "high"
	Category  string `json:"category"`   // "safe", "harmful", "out_of_scope", "ambiguous"
}

// EntityReference represents a referenceable entity from previous turns.
type EntityReference struct {
	ID            string                 `json:"id"`
	Label         string                 `json:"label"`
	Type          string                 `json:"type"` // "report", "topic", "comparison", "insights"
	SourceTurn    int                    `json:"source_turn"`
	SourceToolkit string                 `json:"source_toolkit"`
	Data          map[string]interface{} `json:"data"`
	Aliases       []string               `json:"aliases"`
}

// EntityIndex holds all entities and aliases for a session.
type EntityIndex struct {
	Entities        map[string]*EntityReference `json:"entities"`         // id -> entity
	Aliases         map[string]string           `json:"aliases"`          // alias -> entity id
	DefaultReferent string                      `json:"default_referent"` // most recent entity id
	TurnCount       int                         `json:"turn_count"`
}

// ExtractedEntities is the output from EntityExtractor.
type ExtractedEntities struct {
	NewEntities     []EntityReference `json:"new_entities"`
	DefaultReferent string            `json:"default_referent"`
}

// TaskStatus represents the current state of a task in a plan.
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusBlocked    TaskStatus = "blocked"
)

// ComplexityAssessment is the output from the ComplexityGate agent.
type ComplexityAssessment struct {
	IsComplex         bool     `json:"is_complex"`
	Reasoning         string   `json:"reasoning"`
	ComponentCount    int      `json:"component_count"`
	Indicators        []string `json:"indicators"`
	SuggestedApproach string   `json:"suggested_approach"` // "direct" | "planned"
}

// PlanTask represents a single task within a TaskPlan.
type PlanTask struct {
	ID          string                 `json:"id"`
	Description string                 `json:"description"`
	Toolkit     string                 `json:"toolkit"`
	Action      string                 `json:"action"`
	Parameters  map[string]interface{} `json:"parameters"`
	DependsOn   []string               `json:"depends_on"`
	Status      TaskStatus             `json:"status"`
	ResultRef   string                 `json:"result_ref"`
	Error       string                 `json:"error,omitempty"`
	StartedAt   string                 `json:"started_at,omitempty"`
	CompletedAt string                 `json:"completed_at,omitempty"`
}

// TaskPlan represents a decomposed plan for complex multi-step requests.
type TaskPlan struct {
	ID              string     `json:"id"`
	OriginalRequest string     `json:"original_request"`
	Summary         string     `json:"summary"`
	Tasks           []PlanTask `json:"tasks"`
	CurrentTaskIdx  int        `json:"current_task_idx"`
	Status          TaskStatus `json:"status"`
	CreatedAt       string     `json:"created_at"`
}

// TaskProgress represents the current progress through a task plan.
type TaskProgress struct {
	PlanID          string `json:"plan_id"`
	TotalTasks      int    `json:"total_tasks"`
	CompletedTasks  int    `json:"completed_tasks"`
	CurrentTask     string `json:"current_task"`
	PercentComplete int    `json:"percent_complete"`
}
