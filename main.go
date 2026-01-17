package main

import (
	"context"
	"log"
	"os"

	"github.com/connyay/adk-go-example/adkopenai"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/cmd/launcher"
	"google.golang.org/adk/cmd/launcher/full"
	"google.golang.org/adk/session"
)

func main() {
	ctx := context.Background()

	modelName := os.Getenv("OPENAI_MODEL")
	if modelName == "" {
		modelName = "gpt-4o-mini"
	}

	log.Printf("Using model: %s", modelName)

	m := adkopenai.NewModel(modelName, nil)

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
