# ADK Go Example

A multi-agent research assistant built with [Google ADK (Agent Development Kit)](https://github.com/google/adk-go). Demonstrates dynamic toolkit loading, parallel agent execution, entity persistence, and intelligent message routing.

## Features

- **Dynamic Toolkit Loading**: Define toolkits in markdown files with YAML frontmatter - no code changes needed to add capabilities
- **Parallel Intake Pipeline**: Guardrails, strategy search, and doc search run concurrently
- **Three-Path Routing**: Messages are classified as `new_query`, `followup`, or `pivot` for optimized processing
- **Entity Persistence**: Results from previous turns are indexed and resolvable via natural language references
- **Strategy-Based Routing**: Keyword matching determines which toolkit handles a request

## Architecture

```
User Message
     │
     ▼
┌─────────────────┐
│ IntakeClassifier│ ─── Classifies: new_query / followup / pivot
└────────┬────────┘     Resolves entity references ("the report", "that")
         │
    ┌────┴────┬──────────────┐
    │         │              │
new_query   pivot       followup
    │         │              │
    ▼         ▼              ▼
┌────────┐ ┌────────┐   ┌────────┐
│Parallel│ │ Pivot  │   │  Fast  │
│ Intake │ │ Intake │   │  Path  │
└───┬────┘ └───┬────┘   └───┬────┘
    │          │            │
    ▼          ▼            │
┌────────┐ ┌────────┐       │
│  Orch  │ │ Pivot  │       │
│        │ │  Orch  │       │
└───┬────┘ └───┬────┘       │
    │          │            │
    └────┬─────┴────────────┘
         │
         ▼
   ┌──────────┐
   │ Toolkit  │ ─── analysis, export, ...
   └────┬─────┘
        │
        ▼
┌─────────────────┐
│ EntityExtractor │ ─── Indexes results for future reference
└─────────────────┘
```

## Processing Paths

### Full Path (new_query)

For new queries with no prior context, all intake agents run in parallel:

```
                    ┌──────────────────────────────────────────────────┐
                    │                 Parallel Intake                  │
                    │                                                  │
                    │  ┌───────────┐  ┌───────────────┐  ┌───────────┐ │
User ───────────────┼─▶│ Guardrails│  │ StrategySearch│  │ DocSearch │ │
Message             │  │           │  │               │  │           │ │
                    │  │     ▼     │  │       ▼       │  │     ▼     │ │
                    │  │  Safety   │  │  Strategies   │  │   Docs    │ │
                    │  │  Result   │  │   Matched     │  │  Results  │ │
                    │  └───────────┘  └───────────────┘  └───────────┘ │
                    │         │              │                │        │
                    │         └──────────────┼────────────────┘        │
                    │                        │ (all complete)          │
                    └────────────────────────┼─────────────────────────┘
                                             │
                                             ▼
                                    ┌─────────────────┐
                                    │  Orchestrator   │
                                    └────────┬────────┘
                                             │
                         ┌───────────────────┼───────────────────┐
                         ▼                   ▼                   ▼
                   ┌───────────┐        ┌──────────┐       ┌──────────────┐
                   │Synthesizer│        │ Toolkit  │       │ Multi-Toolkit│
                   │(info only)│        │ (single) │       │   (chain)    │
                   └───────────┘        └──────────┘       └──────────────┘
```

**Latency**: `max(guardrails, strategy, docs)` instead of `sum(guardrails + strategy + docs)`

### Pivot Path

When switching toolkits while referencing prior context (e.g., "export that to PDF"):

```
                    ┌────────────────────────────────────┐
                    │            Pivot Intake            │
                    │                                    │
                    │  ┌───────────┐  ┌───────────────┐  │
User ───────────────┼─▶│ Guardrails│  │ StrategySearch│  │
Message             │  │           │  │               │  │
                    │  │     ▼     │  │       ▼       │  │
                    │  │  Safety   │  │  Strategies   │  │
                    │  │  Result   │  │   Matched     │  │
                    │  └─────┬─────┘  └───────┬───────┘  │
                    │        │                │          │
                    │        └────────┬───────┘          │
                    │                 │ (both complete)  │
                    │                                    │
                    │    ⚡ No DocSearch - user already   │
                    │       knows what they want         │
                    └─────────────────┼──────────────────┘
                                      │
                                      ▼
                          ┌───────────────────┐
                          │ Pivot Orchestrator│
                          │                   │
                          │ • Reads resolved  │
                          │   entity refs     │
                          │ • Merges context  │
                          │   with strategy   │
                          └─────────┬─────────┘
                                    │
                                    ▼
                              ┌──────────┐
                              │ Toolkit  │
                              └──────────┘
```

**Latency**: `max(guardrails, strategy)` - faster than full path

### Fast Path (followup)

For follow-ups within the same toolkit (e.g., "make it longer"):

```
User ──────▶ Guardrails ──────▶ Active Toolkit
Message         │                    │
                │                    │
           (no intake)          (continues
                                 context)
```

**Latency**: `guardrails + toolkit` - fastest path

### Path Comparison

| Path  | When                         | Parallel Agents              | Latency |
| ----- | ---------------------------- | ---------------------------- | ------- |
| Full  | New topic, first message     | Guardrails + Strategy + Docs | ~3-5s   |
| Pivot | Switch toolkit, keep context | Guardrails + Strategy        | ~2-4s   |
| Fast  | Continue in same toolkit     | None                         | ~1-2s   |

## Project Structure

```
├── main.go              # Entry point, launcher configuration
├── pipeline.go          # Pipeline construction and routing logic
├── agents.go            # Agent builders (classifier, guardrails, orchestrator, etc.)
├── entity.go            # Entity persistence and resolution
├── tools.go             # Tool implementations and registry setup
├── types.go             # Shared type definitions
├── state/
│   └── keys.go          # Session state key constants
├── toolkit/
│   ├── config.go        # ToolkitConfig, StrategyConfig types
│   ├── registry.go      # Tool function registry
│   ├── loader.go        # Markdown/YAML toolkit loader
│   └── factory.go       # Agent builder from toolkit config
└── toolkits/
    ├── analysis.md      # Analysis toolkit definition
    └── export.md        # Export toolkit definition
```

## Toolkit Definition

Toolkits are defined in markdown files with YAML frontmatter:

```markdown
---
name: analysis
description: "Executes analysis actions: reports, comparisons, insight extraction"
tools:
  - generate_report
  - compare_sources
  - extract_insights
keywords:
  - report
  - compare
  - analyze
strategies:
  - name: generate_report
    description: Generate a structured report on a topic
    keywords:
      - report
      - generate
    steps:
      - Gather sources
      - Analyze content
      - Structure report
      - Generate output
depends_on:
  - orchestrator_decision
  - doc_results
output_key: final_response
---

You are an analysis toolkit that executes analytical actions.

Available tools:

- generate_report: Create a structured report on a topic
- compare_sources: Compare information from different sources
- extract_insights: Extract key insights and patterns
```

## Running

```bash
# Build
go build -o assistant .

# Web UI (default)
./assistant
# Open http://localhost:8080

# Custom port
./assistant web api webui -port 3000

# Console mode
./assistant console
```

## Environment Variables

| Variable         | Default       | Description    |
| ---------------- | ------------- | -------------- |
| `OPENAI_API_KEY` | (required)    | OpenAI API key |
| `OPENAI_MODEL`   | `gpt-4o-mini` | Model to use   |

## Example Queries

```
# Informational (synthesizer path)
"What is machine learning?"

# Analysis toolkit
"Generate a report on cloud computing"
"Compare machine learning frameworks"

# Pivot to export
"Export that to PDF"
"Create a presentation from the report"

# Follow-up (fast path)
"Make it more detailed"
"Add a section on security"
```

## Adding a New Toolkit

1. Create `toolkits/my_toolkit.md` with YAML frontmatter
2. Define tools, keywords, strategies, and dependencies
3. Register tool functions in `tools.go`:

   ```go
   func RegisterAllTools(reg *toolkit.ToolRegistry) {
       // ...
       mustRegister(reg, "my_tool", "Description", myToolHandler)
   }
   ```

4. Restart - the toolkit is automatically loaded and available

## State Keys

State is passed between agents via session state. Keys are defined in `state/keys.go`:

| Key                     | Description                   |
| ----------------------- | ----------------------------- |
| `intake_classification` | Message classification result |
| `guardrails_result`     | Safety check result           |
| `doc_results`           | Documentation search results  |
| `strategy_results`      | Matched strategies            |
| `orchestrator_decision` | Routing decision              |
| `entity_index`          | Persistent entity references  |
| `final_response`        | Output to user                |

## License

[The Unlicense](https://unlicense.org/)
