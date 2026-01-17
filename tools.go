package main

import (
	"fmt"
	"log"
	"math/rand"
	"strings"

	"github.com/connyay/adk-go-example/toolkit"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// toolkitRegistry holds the loaded toolkit registry for use by searchStrategies.
var toolkitRegistry *toolkit.ToolkitRegistry

// SetToolkitRegistry sets the toolkit registry for keyword lookups.
func SetToolkitRegistry(reg *toolkit.ToolkitRegistry) {
	toolkitRegistry = reg
}

// RegisterAllTools registers all tool functions with the given registry.
func RegisterAllTools(reg *toolkit.ToolRegistry) {

	// Documentation/search tools
	mustRegister(reg, "search_docs", "Search documentation and knowledge base for information on a topic", searchDocs)
	mustRegister(reg, "search_strategies", "Search for actionable strategies and capabilities that can be executed", searchStrategies)

	// Analysis tools
	mustRegister(reg, "generate_report", "Generate a structured report on a topic", generateReport)
	mustRegister(reg, "compare_sources", "Compare and contrast information from multiple sources", compareSources)
	mustRegister(reg, "extract_insights", "Extract key insights from analyzed sources", extractInsights)

	// Export tools
	mustRegister(reg, "export_pdf", "Export content as a PDF document", exportPDF)
	mustRegister(reg, "export_csv", "Export data as a CSV file", exportCSV)
	mustRegister(reg, "create_presentation", "Create a presentation/slideshow from content", createPresentation)
}

func mustRegister[TArgs, TResults any](reg *toolkit.ToolRegistry, name, description string, handler functiontool.Func[TArgs, TResults]) {
	t, err := functiontool.New(functiontool.Config{Name: name, Description: description}, handler)
	if err != nil {
		panic(fmt.Sprintf("failed to create tool %q: %v", name, err))
	}
	reg.Register(name, t)
}

// limitSliceLen returns a limit value that is at most the length of the slice.
// If requested is <= 0, returns sliceLen.
func limitSliceLen(requested, sliceLen int) int {
	if requested <= 0 || requested > sliceLen {
		return sliceLen
	}
	return requested
}

func searchDocs(ctx tool.Context, input SearchDocsInput) (SearchDocsOutput, error) {
	log.Printf("[TOOL] searchDocs called: query=%q categories=%v max=%d",
		input.Query, input.Categories, input.MaxResults)

	docs := []Document{
		{
			Title:    fmt.Sprintf("Understanding %s: A Comprehensive Guide", input.Query),
			Category: "concepts",
			Snippet:  fmt.Sprintf("This guide covers the fundamentals of %s, including key principles and best practices...", input.Query),
			URL:      fmt.Sprintf("https://docs.example.com/concepts/%s", strings.ReplaceAll(strings.ToLower(input.Query), " ", "-")),
			Score:    0.92,
		},
		{
			Title:    fmt.Sprintf("Tutorial: Getting Started with %s", input.Query),
			Category: "tutorials",
			Snippet:  fmt.Sprintf("Learn how to work with %s step by step, from basics to advanced techniques...", input.Query),
			URL:      fmt.Sprintf("https://docs.example.com/tutorials/%s", strings.ReplaceAll(strings.ToLower(input.Query), " ", "-")),
			Score:    0.85,
		},
		{
			Title:    fmt.Sprintf("%s Reference Documentation", input.Query),
			Category: "reference",
			Snippet:  fmt.Sprintf("Complete API and reference documentation for %s...", input.Query),
			URL:      fmt.Sprintf("https://docs.example.com/reference/%s", strings.ReplaceAll(strings.ToLower(input.Query), " ", "-")),
			Score:    0.78,
		},
	}

	limit := limitSliceLen(input.MaxResults, len(docs))
	return SearchDocsOutput{
		Documents: docs[:limit],
		Total:     len(docs),
	}, nil
}

func searchStrategies(ctx tool.Context, input SearchStrategiesInput) (SearchStrategiesOutput, error) {
	log.Printf("[TOOL] searchStrategies called: query=%q domain=%s",
		input.Query, input.Domain)

	if toolkitRegistry == nil {
		log.Printf("[TOOL] searchStrategies: no toolkit registry, returning empty")
		return SearchStrategiesOutput{
			Strategies: []Strategy{},
			HasMatch:   false,
		}, nil
	}

	// Get matching strategies from the registry
	matches := toolkitRegistry.GetMatchingStrategies(input.Query)

	if len(matches) == 0 {
		return SearchStrategiesOutput{
			Strategies: []Strategy{},
			HasMatch:   false,
		}, nil
	}

	// Convert to output format with score based on keyword match ratio
	strategies := make([]Strategy, 0, len(matches))
	for _, match := range matches {
		score := float64(match.MatchedCount) / float64(match.TotalKeywords)
		strategies = append(strategies, Strategy{
			Name:        match.Strategy.Name,
			Description: match.Strategy.Description,
			Toolkit:     match.Toolkit,
			Score:       score,
			Steps:       match.Strategy.Steps,
		})
	}

	log.Printf("[TOOL] searchStrategies: found %d matching strategies", len(strategies))

	return SearchStrategiesOutput{
		Strategies: strategies,
		HasMatch:   true,
	}, nil
}

func generateReport(ctx tool.Context, input GenerateReportInput) (GenerateReportOutput, error) {
	log.Printf("[TOOL] generateReport called: topic=%q format=%s sources=%v",
		input.Topic, input.Format, input.Sources)

	sections := []string{
		fmt.Sprintf("Executive Summary: Overview of %s", input.Topic),
		fmt.Sprintf("Background: Context and history of %s", input.Topic),
		fmt.Sprintf("Key Findings: Main insights about %s", input.Topic),
		fmt.Sprintf("Analysis: Deep dive into %s", input.Topic),
		fmt.Sprintf("Recommendations: Next steps for %s", input.Topic),
	}

	limit := limitSliceLen(input.MaxSections, len(sections))
	return GenerateReportOutput{
		ReportID:  fmt.Sprintf("RPT-%d", rand.Intn(10000)),
		Title:     fmt.Sprintf("Analysis Report: %s", input.Topic),
		Sections:  sections[:limit],
		WordCount: rand.Intn(2000) + 500,
	}, nil
}

func compareSources(ctx tool.Context, input CompareSourcesInput) (CompareSourcesOutput, error) {
	log.Printf("[TOOL] compareSources called: topic=%q sources=%v",
		input.Topic, input.Sources)

	return CompareSourcesOutput{
		Comparison: fmt.Sprintf("Comparison of sources regarding %s shows both convergent and divergent perspectives.", input.Topic),
		Agreements: []string{
			fmt.Sprintf("All sources agree that %s is significant", input.Topic),
			"Common methodologies are referenced across sources",
			"Key terminology is consistent",
		},
		Differences: []string{
			"Sources differ on implementation approaches",
			"Timeline estimates vary between sources",
			"Some sources emphasize different aspects",
		},
	}, nil
}

func extractInsights(ctx tool.Context, input ExtractInsightsInput) (ExtractInsightsOutput, error) {
	log.Printf("[TOOL] extractInsights called: topic=%q maxPoints=%d",
		input.Topic, input.MaxPoints)

	insights := []string{
		fmt.Sprintf("Key insight 1: %s shows significant potential for growth", input.Topic),
		fmt.Sprintf("Key insight 2: Current approaches to %s have limitations", input.Topic),
		fmt.Sprintf("Key insight 3: Emerging trends in %s suggest new directions", input.Topic),
		fmt.Sprintf("Key insight 4: Best practices for %s are evolving", input.Topic),
	}

	limit := limitSliceLen(input.MaxPoints, len(insights))
	return ExtractInsightsOutput{
		Insights:   insights[:limit],
		Confidence: 0.82 + rand.Float64()*0.15,
		Sources:    rand.Intn(5) + 3,
	}, nil
}

func exportPDF(ctx tool.Context, input ExportPDFInput) (ExportPDFOutput, error) {
	log.Printf("[TOOL] exportPDF called: title=%q contentRef=%q includeTOC=%v",
		input.Title, input.ContentRef, input.IncludeTOC)

	filename := fmt.Sprintf("%s.pdf", strings.ReplaceAll(strings.ToLower(input.Title), " ", "_"))
	pageCount := rand.Intn(10) + 3

	return ExportPDFOutput{
		Filename:  filename,
		PageCount: pageCount,
		SizeKB:    pageCount * (rand.Intn(50) + 30),
		URL:       fmt.Sprintf("https://exports.example.com/pdf/%s", filename),
	}, nil
}

func exportCSV(ctx tool.Context, input ExportCSVInput) (ExportCSVOutput, error) {
	log.Printf("[TOOL] exportCSV called: dataRef=%q columns=%v",
		input.DataRef, input.Columns)

	filename := fmt.Sprintf("export_%d.csv", rand.Intn(10000))
	rowCount := rand.Intn(100) + 10

	return ExportCSVOutput{
		Filename: filename,
		RowCount: rowCount,
		SizeKB:   rowCount * 2,
		URL:      fmt.Sprintf("https://exports.example.com/csv/%s", filename),
	}, nil
}

func createPresentation(ctx tool.Context, input CreatePresentationInput) (CreatePresentationOutput, error) {
	log.Printf("[TOOL] createPresentation called: title=%q contentRef=%q maxSlides=%d style=%s",
		input.Title, input.ContentRef, input.MaxSlides, input.Style)

	slideCount := input.MaxSlides
	if slideCount <= 0 || slideCount > 20 {
		slideCount = 8
	}

	slides := make([]string, slideCount)
	slides[0] = fmt.Sprintf("Title: %s", input.Title)
	slides[1] = "Agenda / Overview"
	for i := 2; i < slideCount-1; i++ {
		slides[i] = fmt.Sprintf("Content Slide %d", i-1)
	}
	slides[slideCount-1] = "Summary & Questions"

	filename := fmt.Sprintf("%s.pptx", strings.ReplaceAll(strings.ToLower(input.Title), " ", "_"))

	return CreatePresentationOutput{
		Filename:   filename,
		SlideCount: slideCount,
		Slides:     slides,
		URL:        fmt.Sprintf("https://exports.example.com/pptx/%s", filename),
	}, nil
}
