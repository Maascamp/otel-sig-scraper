package analysis

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gordyrad/otel-sig-tracker/internal/store"
)

const datadogRelevanceKeywords = `## High Relevance Keywords
These topics have direct impact on Datadog's OpenTelemetry integration:
- OTLP, OTLP/HTTP, OTLP/gRPC
- trace context, W3C trace context, baggage
- sampling, tail sampling, head sampling
- Datadog exporter, vendor exporters
- semantic conventions (all: HTTP, DB, messaging, etc.)
- resource detection, resource attributes
- metrics SDK, delta vs cumulative temporality
- log bridge, log SDK
- collector pipeline, processor, receiver, exporter
- profiling signal, profile data model
- OpAMP, agent management
- context propagation
- instrumentation libraries
- configuration file format
- entities, resource lifecycle

## Medium Relevance Keywords
These topics are relevant but less directly impactful:
- SDK lifecycle, provider, tracer, meter, logger
- batch processing, export retry
- gRPC instrumentation, HTTP instrumentation
- Kubernetes operator, auto-instrumentation
- eBPF instrumentation
- Prometheus compatibility, remote write
`

// RelevanceScorer scores synthesized reports for Datadog relevance.
type RelevanceScorer struct {
	llm           LLMClient
	store         *store.Store
	customContext string
}

// NewRelevanceScorer creates a new RelevanceScorer.
// customContext is optional additional context that gets appended to the relevance prompt.
func NewRelevanceScorer(llm LLMClient, s *store.Store, customContext string) *RelevanceScorer {
	return &RelevanceScorer{
		llm:           llm,
		store:         s,
		customContext: customContext,
	}
}

// Score produces a Datadog relevance report from a synthesized SIG report.
func (r *RelevanceScorer) Score(ctx context.Context, sigID, sigName string, synthesis *SynthesizedReport, start, end time.Time) (*RelevanceReport, error) {
	if synthesis == nil {
		return nil, fmt.Errorf("no synthesis to score for SIG %s", sigID)
	}

	contentHash := hashContent(synthesis.Synthesis)
	cacheKey := buildCacheKey(sigID, "relevance", start, end, contentHash)

	// Check cache.
	cached, err := r.store.GetAnalysisCache(cacheKey)
	if err == nil && cached != nil {
		report := &RelevanceReport{
			SIGID:      sigID,
			SIGName:    sigName,
			Report:     cached.Result,
			Model:      cached.Model,
			TokensUsed: cached.TokensUsed,
		}
		report.HighItems, report.MediumItems, report.LowItems = parseRelevanceItems(cached.Result)
		return report, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("checking analysis cache: %w", err)
	}

	systemPrompt := buildRelevanceSystemPrompt(r.customContext)
	promptHash := hashContent(systemPrompt)

	userPrompt := fmt.Sprintf(
		"Produce a Datadog relevance report for the %s SIG based on the following synthesis "+
			"covering %s to %s:\n\n%s",
		sigName,
		start.Format("2006-01-02"),
		end.Format("2006-01-02"),
		synthesis.Synthesis,
	)

	resp, err := r.llm.Complete(ctx, &CompletionRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM completion for relevance scoring: %w", err)
	}

	// Cache the result.
	if cacheErr := r.store.PutAnalysisCache(&store.AnalysisCache{
		CacheKey:       cacheKey,
		SIGID:          sigID,
		SourceType:     "relevance",
		DateRangeStart: start,
		DateRangeEnd:   end,
		PromptHash:     promptHash,
		Result:         resp.Content,
		Model:          resp.Model,
		TokensUsed:     resp.TokensUsed,
	}); cacheErr != nil {
		_ = cacheErr
	}

	highItems, mediumItems, lowItems := parseRelevanceItems(resp.Content)

	return &RelevanceReport{
		SIGID:       sigID,
		SIGName:     sigName,
		Report:      resp.Content,
		HighItems:   highItems,
		MediumItems: mediumItems,
		LowItems:    lowItems,
		Model:       resp.Model,
		TokensUsed:  resp.TokensUsed,
	}, nil
}

// buildRelevanceSystemPrompt constructs the full system prompt for relevance scoring.
func buildRelevanceSystemPrompt(customContext string) string {
	var sb strings.Builder

	sb.WriteString("You are producing a concise intelligence brief for Datadog engineering leaders.\n")
	sb.WriteString("Score each topic's relevance to Datadog (HIGH/MEDIUM/LOW) based on:\n")
	sb.WriteString("- Direct impact on Datadog's OTLP ingest pipeline\n")
	sb.WriteString("- Changes to trace/metric/log formats or semantic conventions\n")
	sb.WriteString("- New instrumentation that Datadog should support\n")
	sb.WriteString("- Collector changes affecting Datadog exporter\n")
	sb.WriteString("- Competitive landscape (features overlapping with Datadog products)\n")
	sb.WriteString("- SDK changes affecting Datadog's tracing libraries\n")
	sb.WriteString("- Changes to sampling, context propagation, or resource detection\n")
	sb.WriteString("- OpAMP or agent management developments\n")
	sb.WriteString("- Profiling signal developments\n\n")

	sb.WriteString("Use the following keyword reference for relevance classification:\n\n")
	sb.WriteString(datadogRelevanceKeywords)

	sb.WriteString("\n\nFormat your response with clear markdown sections:\n")
	sb.WriteString("#### HIGH Relevance\n")
	sb.WriteString("Each bullet: `- **Topic Name** — one-sentence what + why. Action clause if needed.`\n")
	sb.WriteString("If no items, write: `None this period.`\n\n")
	sb.WriteString("#### MEDIUM Relevance\n")
	sb.WriteString("Each bullet: `- **Topic Name** — one-sentence what + why.`\n")
	sb.WriteString("If no items, write: `None this period.`\n\n")
	sb.WriteString("#### LOW Relevance\n")
	sb.WriteString("Each bullet: `- **Topic Name** — one-sentence what + why.`\n")
	sb.WriteString("If no items, write: `None this period.`\n\n")

	sb.WriteString("Do NOT include any of the following in your response: ")
	sb.WriteString("\"Overall Assessment\", \"Analysis Summary\", \"Note\", \"Recommendation\", ")
	sb.WriteString("\"Executive Summary\", or prose paragraphs outside the bullet lists. ")
	sb.WriteString("Only output the three sections above with their bullet items.\n")

	if customContext != "" {
		sb.WriteString("\n\n## Additional Context from User\n")
		sb.WriteString(customContext)
	}

	return sb.String()
}

// parseRelevanceItems extracts HIGH, MEDIUM, and LOW items from the LLM output.
// It looks for markdown headers like "#### HIGH Relevance", "#### MEDIUM Relevance", "#### LOW Relevance"
// and collects bullet points under each section.
func parseRelevanceItems(content string) (high, medium, low []string) {
	lines := strings.Split(content, "\n")

	type section int
	const (
		sectionNone section = iota
		sectionHigh
		sectionMedium
		sectionLow
	)

	current := sectionNone

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		upper := strings.ToUpper(trimmed)

		// Detect section headers.
		if strings.Contains(upper, "HIGH") && (strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "**")) {
			current = sectionHigh
			continue
		}
		if strings.Contains(upper, "MEDIUM") && (strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "**")) {
			current = sectionMedium
			continue
		}
		if strings.Contains(upper, "LOW") && (strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "**")) {
			current = sectionLow
			continue
		}

		// Collect bullet items.
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			item := strings.TrimSpace(trimmed[2:])
			if item == "" {
				continue
			}
			switch current {
			case sectionHigh:
				high = append(high, item)
			case sectionMedium:
				medium = append(medium, item)
			case sectionLow:
				low = append(low, item)
			}
		}
	}

	return high, medium, low
}
