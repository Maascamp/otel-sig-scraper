package analysis

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gordyrad/otel-sig-tracker/internal/store"
)

// Synthesizer merges per-source summaries into a unified cross-source report.
type Synthesizer struct {
	llm   LLMClient
	store *store.Store
}

// NewSynthesizer creates a new Synthesizer.
func NewSynthesizer(llm LLMClient, s *store.Store) *Synthesizer {
	return &Synthesizer{
		llm:   llm,
		store: s,
	}
}

// Synthesize produces a unified report from multiple per-source summaries for a SIG.
func (s *Synthesizer) Synthesize(ctx context.Context, sigID, sigName string, summaries []*SourceSummary, start, end time.Time) (*SynthesizedReport, error) {
	if len(summaries) == 0 {
		return nil, fmt.Errorf("no summaries to synthesize for SIG %s", sigID)
	}

	// Build the user prompt from all source summaries.
	var parts []string
	for _, summary := range summaries {
		parts = append(parts, fmt.Sprintf("=== Source: %s ===\n%s", summary.SourceType, summary.Summary))
	}
	content := strings.Join(parts, "\n\n")

	contentHash := hashContent(content)
	cacheKey := buildCacheKey(sigID, "synthesis", start, end, contentHash)

	// Check cache.
	cached, err := s.store.GetAnalysisCache(cacheKey)
	if err == nil && cached != nil {
		return &SynthesizedReport{
			SIGID:      sigID,
			SIGName:    sigName,
			Synthesis:  cached.Result,
			Model:      cached.Model,
			TokensUsed: cached.TokensUsed,
		}, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("checking analysis cache: %w", err)
	}

	systemPrompt := fmt.Sprintf(
		"Given the following summaries from meeting notes, video recordings,\n"+
			"and Slack discussions for the %s SIG, produce a unified report.\n"+
			"Deduplicate topics discussed across sources. Flag items where different\n"+
			"sources provide complementary information.",
		sigName,
	)

	promptHash := hashContent(systemPrompt)

	resp, err := s.llm.Complete(ctx, &CompletionRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   content,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM completion for synthesis: %w", err)
	}

	// Cache the result.
	if cacheErr := s.store.PutAnalysisCache(&store.AnalysisCache{
		CacheKey:       cacheKey,
		SIGID:          sigID,
		SourceType:     "synthesis",
		DateRangeStart: start,
		DateRangeEnd:   end,
		PromptHash:     promptHash,
		Result:         resp.Content,
		Model:          resp.Model,
		TokensUsed:     resp.TokensUsed,
	}); cacheErr != nil {
		_ = cacheErr
	}

	return &SynthesizedReport{
		SIGID:      sigID,
		SIGName:    sigName,
		Synthesis:  resp.Content,
		Model:      resp.Model,
		TokensUsed: resp.TokensUsed,
	}, nil
}
