package analysis

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/gordyrad/otel-sig-tracker/internal/store"
)

// Summarizer produces per-source summaries for SIG content using an LLM.
type Summarizer struct {
	llm   LLMClient
	store *store.Store
}

// NewSummarizer creates a new Summarizer.
func NewSummarizer(llm LLMClient, s *store.Store) *Summarizer {
	return &Summarizer{
		llm:   llm,
		store: s,
	}
}

// SummarizeMeetingNotes produces a summary of meeting notes for a SIG within a date range.
func (s *Summarizer) SummarizeMeetingNotes(ctx context.Context, sigID, sigName string, notes []*store.MeetingNote, start, end time.Time) (*SourceSummary, error) {
	if len(notes) == 0 {
		return nil, fmt.Errorf("no meeting notes to summarize for SIG %s", sigID)
	}

	// Build the content from all notes in the range.
	var contentParts []string
	for _, note := range notes {
		contentParts = append(contentParts, fmt.Sprintf("--- Meeting Date: %s ---\n%s",
			note.MeetingDate.Format("2006-01-02"), note.RawText))
	}
	content := strings.Join(contentParts, "\n\n")

	contentHash := hashContent(content)
	cacheKey := buildCacheKey(sigID, "notes", start, end, contentHash)

	// Check cache.
	cached, err := s.store.GetAnalysisCache(cacheKey)
	if err == nil && cached != nil {
		return &SourceSummary{
			SIGID:      sigID,
			SIGName:    sigName,
			SourceType: "notes",
			Summary:    cached.Result,
			Model:      cached.Model,
			TokensUsed: cached.TokensUsed,
		}, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("checking analysis cache: %w", err)
	}

	systemPrompt := fmt.Sprintf(
		"You are analyzing OpenTelemetry SIG meeting notes for the %s SIG.\n"+
			"Summarize the key discussions, decisions, and action items from the following\n"+
			"meeting notes dated between %s and %s.\n"+
			"Focus on: technical decisions, new features, breaking changes, deprecations,\n"+
			"integration changes, protocol/format changes, and anything affecting\n"+
			"telemetry pipelines or clients.",
		sigName,
		start.Format("2006-01-02"),
		end.Format("2006-01-02"),
	)

	promptHash := hashContent(systemPrompt)

	resp, err := s.llm.Complete(ctx, &CompletionRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   content,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM completion for meeting notes: %w", err)
	}

	// Cache the result.
	if cacheErr := s.store.PutAnalysisCache(&store.AnalysisCache{
		CacheKey:       cacheKey,
		SIGID:          sigID,
		SourceType:     "notes",
		DateRangeStart: start,
		DateRangeEnd:   end,
		PromptHash:     promptHash,
		Result:         resp.Content,
		Model:          resp.Model,
		TokensUsed:     resp.TokensUsed,
	}); cacheErr != nil {
		// Log but do not fail on cache write errors.
		_ = cacheErr
	}

	return &SourceSummary{
		SIGID:      sigID,
		SIGName:    sigName,
		SourceType: "notes",
		Summary:    resp.Content,
		Model:      resp.Model,
		TokensUsed: resp.TokensUsed,
	}, nil
}

// SummarizeVideoTranscripts produces a summary of video transcripts for a SIG within a date range.
func (s *Summarizer) SummarizeVideoTranscripts(ctx context.Context, sigID, sigName string, transcripts []*store.VideoTranscript, start, end time.Time) (*SourceSummary, error) {
	if len(transcripts) == 0 {
		return nil, fmt.Errorf("no video transcripts to summarize for SIG %s", sigID)
	}

	// Build the content from all transcripts in the range.
	var contentParts []string
	for _, t := range transcripts {
		contentParts = append(contentParts, fmt.Sprintf("--- Recording Date: %s (Duration: %d min) ---\n%s",
			t.RecordingDate.Format("2006-01-02"), t.DurationMinutes, t.Transcript))
	}
	content := strings.Join(contentParts, "\n\n")

	contentHash := hashContent(content)
	cacheKey := buildCacheKey(sigID, "video", start, end, contentHash)

	// Check cache.
	cached, err := s.store.GetAnalysisCache(cacheKey)
	if err == nil && cached != nil {
		return &SourceSummary{
			SIGID:      sigID,
			SIGName:    sigName,
			SourceType: "video",
			Summary:    cached.Result,
			Model:      cached.Model,
			TokensUsed: cached.TokensUsed,
		}, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("checking analysis cache: %w", err)
	}

	// Build a combined system prompt covering all transcripts in the range.
	systemPrompt := fmt.Sprintf(
		"You are analyzing transcripts of the %s SIG meetings.\n"+
			"Summarize the key technical discussions, noting any decisions made,\n"+
			"controversies, and planned work. Identify speakers and their positions\n"+
			"where possible.",
		sigName,
	)

	promptHash := hashContent(systemPrompt)

	resp, err := s.llm.Complete(ctx, &CompletionRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   content,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM completion for video transcripts: %w", err)
	}

	// Cache the result.
	if cacheErr := s.store.PutAnalysisCache(&store.AnalysisCache{
		CacheKey:       cacheKey,
		SIGID:          sigID,
		SourceType:     "video",
		DateRangeStart: start,
		DateRangeEnd:   end,
		PromptHash:     promptHash,
		Result:         resp.Content,
		Model:          resp.Model,
		TokensUsed:     resp.TokensUsed,
	}); cacheErr != nil {
		_ = cacheErr
	}

	return &SourceSummary{
		SIGID:      sigID,
		SIGName:    sigName,
		SourceType: "video",
		Summary:    resp.Content,
		Model:      resp.Model,
		TokensUsed: resp.TokensUsed,
	}, nil
}

// SummarizeSlackMessages produces a summary of Slack messages for a SIG within a date range.
func (s *Summarizer) SummarizeSlackMessages(ctx context.Context, sigID, sigName string, messages []*store.SlackMessage, start, end time.Time) (*SourceSummary, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no slack messages to summarize for SIG %s", sigID)
	}

	// Determine the channel name from the first message (all messages belong to the same SIG).
	channelName := sigName

	// Build the content from all messages in the range.
	var contentParts []string
	for _, m := range messages {
		entry := fmt.Sprintf("[%s] %s: %s",
			m.MessageDate.Format("2006-01-02 15:04"), m.UserName, m.Text)
		if m.ThreadTS != "" && m.ThreadTS != m.MessageTS {
			entry = "  (thread reply) " + entry
		}
		contentParts = append(contentParts, entry)
	}
	content := strings.Join(contentParts, "\n")

	contentHash := hashContent(content)
	cacheKey := buildCacheKey(sigID, "slack", start, end, contentHash)

	// Check cache.
	cached, err := s.store.GetAnalysisCache(cacheKey)
	if err == nil && cached != nil {
		return &SourceSummary{
			SIGID:      sigID,
			SIGName:    sigName,
			SourceType: "slack",
			Summary:    cached.Result,
			Model:      cached.Model,
			TokensUsed: cached.TokensUsed,
		}, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("checking analysis cache: %w", err)
	}

	systemPrompt := fmt.Sprintf(
		"You are analyzing Slack discussions from the #%s channel\n"+
			"(%s SIG) between %s and %s.\n"+
			"Identify the most significant technical discussions, questions,\n"+
			"and announcements. Group by topic.",
		channelName,
		sigName,
		start.Format("2006-01-02"),
		end.Format("2006-01-02"),
	)

	promptHash := hashContent(systemPrompt)

	resp, err := s.llm.Complete(ctx, &CompletionRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   content,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM completion for slack messages: %w", err)
	}

	// Cache the result.
	if cacheErr := s.store.PutAnalysisCache(&store.AnalysisCache{
		CacheKey:       cacheKey,
		SIGID:          sigID,
		SourceType:     "slack",
		DateRangeStart: start,
		DateRangeEnd:   end,
		PromptHash:     promptHash,
		Result:         resp.Content,
		Model:          resp.Model,
		TokensUsed:     resp.TokensUsed,
	}); cacheErr != nil {
		_ = cacheErr
	}

	return &SourceSummary{
		SIGID:      sigID,
		SIGName:    sigName,
		SourceType: "slack",
		Summary:    resp.Content,
		Model:      resp.Model,
		TokensUsed: resp.TokensUsed,
	}, nil
}

// hashContent returns the hex-encoded SHA-256 hash of the given string.
func hashContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h)
}

// buildCacheKey constructs a deterministic cache key from the given components.
func buildCacheKey(sigID, sourceType string, start, end time.Time, contentHash string) string {
	raw := fmt.Sprintf("%s|%s|%s|%s|%s",
		sigID,
		sourceType,
		start.Format("2006-01-02"),
		end.Format("2006-01-02"),
		contentHash,
	)
	return hashContent(raw)
}
