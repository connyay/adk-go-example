package main

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/connyay/adk-go-example/adkanthropic"
	"github.com/connyay/adk-go-example/adkopenai"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
)

func main() {
	ctx := context.Background()

	m, err := createModel()
	if err != nil {
		log.Fatalf("Failed to create model: %v", err)
	}

	// Build the pipeline
	pipeline, err := buildPipeline(m)
	if err != nil {
		log.Fatalf("Failed to build pipeline: %v", err)
	}

	// Create agent loader
	agentLoader, err := agent.NewMultiLoader(pipeline)
	if err != nil {
		log.Fatalf("Failed to create agent loader: %v", err)
	}

	// Configure the launcher
	config := &launcher.Config{
		SessionService: session.InMemoryService(),
		AgentLoader:    agentLoader,
	}

	// Create the full launcher
	l := full.NewLauncher()

	// Default to web mode
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"web", "api", "webui"}
	}

	log.Printf("Starting with args: %v", args)
	log.Printf("")
	log.Printf("Usage:")
	log.Printf("  ./assistant                          # Web UI at http://localhost:8080")
	log.Printf("  ./assistant web api webui -port 3000 # Custom port")
	log.Printf("  ./assistant console                  # Interactive console mode")
	log.Printf("")
	log.Printf("Try these queries:")
	log.Printf("  - 'What is machine learning?'           -> Summarize (informational)")
	log.Printf("  - 'Generate a report on cloud computing' -> Execute (analysis toolkit)")
	log.Printf("  - 'Compare approaches to data storage'   -> Execute (analysis toolkit)")
	log.Printf("  - 'Export that to PDF'                   -> Pivot to export toolkit")
	log.Printf("")

	if err = l.Execute(ctx, config, args); err != nil {
		log.Fatalf("Launcher failed: %v", err)
	}
}

// createModel creates the appropriate LLM backend based on MODEL_BACKEND env var.
// Supported backends: "openai" (default), "anthropic"
func createModel() (model.LLM, error) {
	backend := strings.ToLower(os.Getenv("MODEL_BACKEND"))

	switch backend {
	case "anthropic":
		modelName := os.Getenv("ANTHROPIC_MODEL")
		if modelName == "" {
			modelName = "claude-sonnet-4-5-20250929"
		}
		log.Printf("Using Anthropic backend with model: %s", modelName)
		return adkanthropic.NewModel(anthropic.Model(modelName), nil)

	default:
		// Default to OpenAI
		modelName := os.Getenv("OPENAI_MODEL")
		if modelName == "" {
			modelName = "gpt-4o-mini"
		}
		log.Printf("Using OpenAI backend with model: %s", modelName)
		return adkopenai.NewModel(modelName, nil), nil
	}
}
