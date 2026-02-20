package analysis

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gordyrad/otel-sig-tracker/internal/store"
)

// mockLLMClient implements LLMClient for testing.
type mockLLMClient struct {
	response  string
	err       error
	callCount atomic.Int64
}

func (m *mockLLMClient) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	m.callCount.Add(1)
	if m.err != nil {
		return nil, m.err
	}
	return &CompletionResponse{
		Content:    m.response,
		Model:      "mock-model",
		TokensUsed: 100,
	}, nil
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// ---------------------------------------------------------------------------
// Summarizer tests
// ---------------------------------------------------------------------------

func TestSummarizeMeetingNotes(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{response: "Summary of collector meeting notes."}
	summarizer := NewSummarizer(mock, s)

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	notes := []*store.MeetingNote{
		{
			SIGID:       "collector",
			DocID:       "doc123",
			MeetingDate: time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
			RawText:     "Discussed OTLP/HTTP improvements and partial success responses.",
		},
		{
			SIGID:       "collector",
			DocID:       "doc123",
			MeetingDate: time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC),
			RawText:     "Reviewed pipeline fan-out architecture proposal.",
		},
	}

	result, err := summarizer.SummarizeMeetingNotes(context.Background(), "collector", "Collector", notes, start, end)
	if err != nil {
		t.Fatalf("SummarizeMeetingNotes failed: %v", err)
	}

	if result.SIGID != "collector" {
		t.Errorf("SIGID = %q, want %q", result.SIGID, "collector")
	}
	if result.SIGName != "Collector" {
		t.Errorf("SIGName = %q, want %q", result.SIGName, "Collector")
	}
	if result.SourceType != "notes" {
		t.Errorf("SourceType = %q, want %q", result.SourceType, "notes")
	}
	if result.Summary != "Summary of collector meeting notes." {
		t.Errorf("Summary = %q, want %q", result.Summary, "Summary of collector meeting notes.")
	}
	if result.Model != "mock-model" {
		t.Errorf("Model = %q, want %q", result.Model, "mock-model")
	}
	if result.TokensUsed != 100 {
		t.Errorf("TokensUsed = %d, want %d", result.TokensUsed, 100)
	}
	if mock.callCount.Load() != 1 {
		t.Errorf("LLM call count = %d, want 1", mock.callCount.Load())
	}
}

func TestSummarizeMeetingNotes_Caching(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{response: "Cached notes summary."}
	summarizer := NewSummarizer(mock, s)

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	notes := []*store.MeetingNote{
		{
			SIGID:       "collector",
			DocID:       "doc123",
			MeetingDate: time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
			RawText:     "Meeting notes for caching test.",
		},
	}

	// First call: should hit LLM.
	result1, err := summarizer.SummarizeMeetingNotes(context.Background(), "collector", "Collector", notes, start, end)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if mock.callCount.Load() != 1 {
		t.Fatalf("expected 1 LLM call after first request, got %d", mock.callCount.Load())
	}

	// Second call with same input: should return cached result without calling LLM.
	result2, err := summarizer.SummarizeMeetingNotes(context.Background(), "collector", "Collector", notes, start, end)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if mock.callCount.Load() != 1 {
		t.Errorf("expected 1 LLM call after second request (cached), got %d", mock.callCount.Load())
	}
	if result1.Summary != result2.Summary {
		t.Errorf("cached summary mismatch: %q vs %q", result1.Summary, result2.Summary)
	}
}

func TestSummarizeMeetingNotes_EmptyInput(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{response: "should not be called"}
	summarizer := NewSummarizer(mock, s)

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	_, err := summarizer.SummarizeMeetingNotes(context.Background(), "collector", "Collector", nil, start, end)
	if err == nil {
		t.Fatal("expected error for empty notes, got nil")
	}
	if mock.callCount.Load() != 0 {
		t.Errorf("LLM should not be called for empty input, got %d calls", mock.callCount.Load())
	}
}

func TestSummarizeMeetingNotes_LLMError(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{err: fmt.Errorf("LLM service unavailable")}
	summarizer := NewSummarizer(mock, s)

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	notes := []*store.MeetingNote{
		{
			SIGID:       "collector",
			DocID:       "doc1",
			MeetingDate: time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC),
			RawText:     "Some notes",
		},
	}

	_, err := summarizer.SummarizeMeetingNotes(context.Background(), "collector", "Collector", notes, start, end)
	if err == nil {
		t.Fatal("expected error when LLM fails, got nil")
	}
}

func TestSummarizeVideoTranscripts(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{response: "Summary of video transcripts."}
	summarizer := NewSummarizer(mock, s)

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	transcripts := []*store.VideoTranscript{
		{
			SIGID:           "collector",
			ZoomURL:         "https://zoom.us/rec/share/abc123",
			RecordingDate:   time.Date(2026, 2, 13, 9, 0, 0, 0, time.UTC),
			DurationMinutes: 54,
			Transcript:      "Speaker 1: Let's discuss the new OTLP partial success response...",
		},
	}

	result, err := summarizer.SummarizeVideoTranscripts(context.Background(), "collector", "Collector", transcripts, start, end)
	if err != nil {
		t.Fatalf("SummarizeVideoTranscripts failed: %v", err)
	}

	if result.SIGID != "collector" {
		t.Errorf("SIGID = %q, want %q", result.SIGID, "collector")
	}
	if result.SourceType != "video" {
		t.Errorf("SourceType = %q, want %q", result.SourceType, "video")
	}
	if result.Summary != "Summary of video transcripts." {
		t.Errorf("Summary = %q, want %q", result.Summary, "Summary of video transcripts.")
	}
}

func TestSummarizeVideoTranscripts_Caching(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{response: "Cached video summary."}
	summarizer := NewSummarizer(mock, s)

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	transcripts := []*store.VideoTranscript{
		{
			SIGID:           "collector",
			ZoomURL:         "https://zoom.us/rec/share/xyz789",
			RecordingDate:   time.Date(2026, 2, 13, 9, 0, 0, 0, time.UTC),
			DurationMinutes: 30,
			Transcript:      "Video transcript for caching test.",
		},
	}

	_, err := summarizer.SummarizeVideoTranscripts(context.Background(), "collector", "Collector", transcripts, start, end)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if mock.callCount.Load() != 1 {
		t.Fatalf("expected 1 LLM call, got %d", mock.callCount.Load())
	}

	_, err = summarizer.SummarizeVideoTranscripts(context.Background(), "collector", "Collector", transcripts, start, end)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if mock.callCount.Load() != 1 {
		t.Errorf("expected 1 LLM call after cached request, got %d", mock.callCount.Load())
	}
}

func TestSummarizeVideoTranscripts_EmptyInput(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{response: "should not be called"}
	summarizer := NewSummarizer(mock, s)

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	_, err := summarizer.SummarizeVideoTranscripts(context.Background(), "collector", "Collector", nil, start, end)
	if err == nil {
		t.Fatal("expected error for empty transcripts, got nil")
	}
}

func TestSummarizeSlackMessages(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{response: "Summary of Slack discussions."}
	summarizer := NewSummarizer(mock, s)

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	messages := []*store.SlackMessage{
		{
			SIGID:       "collector",
			ChannelID:   "C01N6P7KR6W",
			MessageTS:   "1739890000.000100",
			UserID:      "U01ABC",
			UserName:    "alice",
			Text:        "Has anyone looked at the new OTLP partial success response?",
			MessageDate: time.Date(2026, 2, 14, 10, 30, 0, 0, time.UTC),
		},
		{
			SIGID:       "collector",
			ChannelID:   "C01N6P7KR6W",
			MessageTS:   "1739890100.000200",
			ThreadTS:    "1739890000.000100",
			UserID:      "U01DEF",
			UserName:    "bob",
			Text:        "Yes, I reviewed the OTEP. Looks good.",
			MessageDate: time.Date(2026, 2, 14, 10, 35, 0, 0, time.UTC),
		},
	}

	result, err := summarizer.SummarizeSlackMessages(context.Background(), "collector", "Collector", messages, start, end)
	if err != nil {
		t.Fatalf("SummarizeSlackMessages failed: %v", err)
	}

	if result.SIGID != "collector" {
		t.Errorf("SIGID = %q, want %q", result.SIGID, "collector")
	}
	if result.SourceType != "slack" {
		t.Errorf("SourceType = %q, want %q", result.SourceType, "slack")
	}
	if result.Summary != "Summary of Slack discussions." {
		t.Errorf("Summary = %q, want %q", result.Summary, "Summary of Slack discussions.")
	}
}

func TestSummarizeSlackMessages_Caching(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{response: "Cached slack summary."}
	summarizer := NewSummarizer(mock, s)

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	messages := []*store.SlackMessage{
		{
			SIGID:       "collector",
			ChannelID:   "C01N6P7KR6W",
			MessageTS:   "1739890000.000300",
			UserID:      "U01GHI",
			UserName:    "charlie",
			Text:        "Slack message for caching test.",
			MessageDate: time.Date(2026, 2, 14, 11, 0, 0, 0, time.UTC),
		},
	}

	_, err := summarizer.SummarizeSlackMessages(context.Background(), "collector", "Collector", messages, start, end)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if mock.callCount.Load() != 1 {
		t.Fatalf("expected 1 LLM call, got %d", mock.callCount.Load())
	}

	_, err = summarizer.SummarizeSlackMessages(context.Background(), "collector", "Collector", messages, start, end)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if mock.callCount.Load() != 1 {
		t.Errorf("expected 1 LLM call after cached request, got %d", mock.callCount.Load())
	}
}

func TestSummarizeSlackMessages_EmptyInput(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{response: "should not be called"}
	summarizer := NewSummarizer(mock, s)

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	_, err := summarizer.SummarizeSlackMessages(context.Background(), "collector", "Collector", nil, start, end)
	if err == nil {
		t.Fatal("expected error for empty messages, got nil")
	}
}

// ---------------------------------------------------------------------------
// Synthesizer tests
// ---------------------------------------------------------------------------

func TestSynthesize(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{response: "Unified synthesis across all sources."}
	synthesizer := NewSynthesizer(mock, s)

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	summaries := []*SourceSummary{
		{
			SIGID:      "collector",
			SIGName:    "Collector",
			SourceType: "notes",
			Summary:    "Meeting notes discussed OTLP improvements.",
		},
		{
			SIGID:      "collector",
			SIGName:    "Collector",
			SourceType: "video",
			Summary:    "Video recording covered pipeline fan-out.",
		},
		{
			SIGID:      "collector",
			SIGName:    "Collector",
			SourceType: "slack",
			Summary:    "Slack thread about batch processor memory.",
		},
	}

	result, err := synthesizer.Synthesize(context.Background(), "collector", "Collector", summaries, start, end)
	if err != nil {
		t.Fatalf("Synthesize failed: %v", err)
	}

	if result.SIGID != "collector" {
		t.Errorf("SIGID = %q, want %q", result.SIGID, "collector")
	}
	if result.SIGName != "Collector" {
		t.Errorf("SIGName = %q, want %q", result.SIGName, "Collector")
	}
	if result.Synthesis != "Unified synthesis across all sources." {
		t.Errorf("Synthesis = %q, want %q", result.Synthesis, "Unified synthesis across all sources.")
	}
	if result.Model != "mock-model" {
		t.Errorf("Model = %q, want %q", result.Model, "mock-model")
	}
}

func TestSynthesize_Caching(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{response: "Cached synthesis."}
	synthesizer := NewSynthesizer(mock, s)

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	summaries := []*SourceSummary{
		{
			SIGID:      "collector",
			SIGName:    "Collector",
			SourceType: "notes",
			Summary:    "Notes summary for cache test.",
		},
	}

	_, err := synthesizer.Synthesize(context.Background(), "collector", "Collector", summaries, start, end)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if mock.callCount.Load() != 1 {
		t.Fatalf("expected 1 LLM call, got %d", mock.callCount.Load())
	}

	_, err = synthesizer.Synthesize(context.Background(), "collector", "Collector", summaries, start, end)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if mock.callCount.Load() != 1 {
		t.Errorf("expected 1 LLM call after cached request, got %d", mock.callCount.Load())
	}
}

func TestSynthesize_EmptyInput(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{response: "should not be called"}
	synthesizer := NewSynthesizer(mock, s)

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	_, err := synthesizer.Synthesize(context.Background(), "collector", "Collector", nil, start, end)
	if err == nil {
		t.Fatal("expected error for empty summaries, got nil")
	}
}

func TestSynthesize_LLMError(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{err: fmt.Errorf("synthesis LLM failure")}
	synthesizer := NewSynthesizer(mock, s)

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	summaries := []*SourceSummary{
		{SIGID: "collector", SIGName: "Collector", SourceType: "notes", Summary: "Some summary."},
	}

	_, err := synthesizer.Synthesize(context.Background(), "collector", "Collector", summaries, start, end)
	if err == nil {
		t.Fatal("expected error when LLM fails, got nil")
	}
}

// ---------------------------------------------------------------------------
// RelevanceScorer tests
// ---------------------------------------------------------------------------

const mockRelevanceResponse = `## Executive Summary
The Collector SIG discussed OTLP improvements.

## HIGH Relevance
- OTLP/HTTP Partial Success: New partial success response support directly affects Datadog OTLP ingest
- Semantic Convention Changes: Breaking changes to HTTP semantic conventions

## MEDIUM Relevance
- Pipeline Fan-out/Fan-in: Architectural change for fan-out patterns
- SDK Lifecycle Improvements: Better provider shutdown handling

## LOW Relevance
- Batch processor memory improvements
- Documentation updates for contributing guide`

func TestRelevanceScorer_Score(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{response: mockRelevanceResponse}
	scorer := NewRelevanceScorer(mock, s, "")

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	synthesis := &SynthesizedReport{
		SIGID:     "collector",
		SIGName:   "Collector",
		Synthesis: "The Collector SIG discussed OTLP improvements and pipeline changes.",
	}

	result, err := scorer.Score(context.Background(), "collector", "Collector", synthesis, start, end)
	if err != nil {
		t.Fatalf("Score failed: %v", err)
	}

	if result.SIGID != "collector" {
		t.Errorf("SIGID = %q, want %q", result.SIGID, "collector")
	}
	if result.SIGName != "Collector" {
		t.Errorf("SIGName = %q, want %q", result.SIGName, "Collector")
	}
	if result.Report != mockRelevanceResponse {
		t.Error("Report does not match expected mock response")
	}

	// Verify parsed items.
	if len(result.HighItems) != 2 {
		t.Errorf("HighItems count = %d, want 2", len(result.HighItems))
	}
	if len(result.MediumItems) != 2 {
		t.Errorf("MediumItems count = %d, want 2", len(result.MediumItems))
	}
	if len(result.LowItems) != 2 {
		t.Errorf("LowItems count = %d, want 2", len(result.LowItems))
	}
}

func TestRelevanceScorer_Caching(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{response: mockRelevanceResponse}
	scorer := NewRelevanceScorer(mock, s, "")

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	synthesis := &SynthesizedReport{
		SIGID:     "collector",
		SIGName:   "Collector",
		Synthesis: "Synthesis for relevance caching test.",
	}

	_, err := scorer.Score(context.Background(), "collector", "Collector", synthesis, start, end)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if mock.callCount.Load() != 1 {
		t.Fatalf("expected 1 LLM call, got %d", mock.callCount.Load())
	}

	result2, err := scorer.Score(context.Background(), "collector", "Collector", synthesis, start, end)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if mock.callCount.Load() != 1 {
		t.Errorf("expected 1 LLM call after cached request, got %d", mock.callCount.Load())
	}
	// Verify the cached result also has parsed items.
	if len(result2.HighItems) != 2 {
		t.Errorf("cached HighItems count = %d, want 2", len(result2.HighItems))
	}
}

func TestRelevanceScorer_NilSynthesis(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{response: "should not be called"}
	scorer := NewRelevanceScorer(mock, s, "")

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	_, err := scorer.Score(context.Background(), "collector", "Collector", nil, start, end)
	if err == nil {
		t.Fatal("expected error for nil synthesis, got nil")
	}
}

func TestRelevanceScorer_WithCustomContext(t *testing.T) {
	s := newTestStore(t)
	mock := &mockLLMClient{response: mockRelevanceResponse}
	customCtx := "We are especially interested in profiling signal and eBPF developments."
	scorer := NewRelevanceScorer(mock, s, customCtx)

	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	synthesis := &SynthesizedReport{
		SIGID:     "collector",
		SIGName:   "Collector",
		Synthesis: "Collector SIG discussed eBPF instrumentation and profiling.",
	}

	result, err := scorer.Score(context.Background(), "collector", "Collector", synthesis, start, end)
	if err != nil {
		t.Fatalf("Score with custom context failed: %v", err)
	}

	// The result should still be valid.
	if result.SIGID != "collector" {
		t.Errorf("SIGID = %q, want %q", result.SIGID, "collector")
	}
	if mock.callCount.Load() != 1 {
		t.Errorf("expected 1 LLM call, got %d", mock.callCount.Load())
	}
}

// ---------------------------------------------------------------------------
// parseRelevanceItems tests
// ---------------------------------------------------------------------------

func TestParseRelevanceItems(t *testing.T) {
	content := `## Executive Summary
Some executive summary text.

## HIGH Relevance
- OTLP/HTTP Partial Success: directly affects ingest
- Semantic Convention Breaking Changes: HTTP conventions renamed

## MEDIUM Relevance
- Pipeline Fan-out: new architecture
- SDK Lifecycle: improved shutdown

## LOW Relevance
- Batch processor memory improvements
- Docs updates
`

	high, medium, low := parseRelevanceItems(content)

	if len(high) != 2 {
		t.Errorf("high items = %d, want 2; items: %v", len(high), high)
	}
	if len(medium) != 2 {
		t.Errorf("medium items = %d, want 2; items: %v", len(medium), medium)
	}
	if len(low) != 2 {
		t.Errorf("low items = %d, want 2; items: %v", len(low), low)
	}

	// Verify specific items.
	if len(high) > 0 && high[0] != "OTLP/HTTP Partial Success: directly affects ingest" {
		t.Errorf("first high item = %q, unexpected", high[0])
	}
}

func TestParseRelevanceItems_BoldHeaders(t *testing.T) {
	content := `**HIGH Relevance**
- Item A
- Item B

**MEDIUM Relevance**
- Item C

**LOW Relevance**
- Item D
`
	high, medium, low := parseRelevanceItems(content)

	if len(high) != 2 {
		t.Errorf("high items = %d, want 2", len(high))
	}
	if len(medium) != 1 {
		t.Errorf("medium items = %d, want 1", len(medium))
	}
	if len(low) != 1 {
		t.Errorf("low items = %d, want 1", len(low))
	}
}

func TestParseRelevanceItems_AsteriskBullets(t *testing.T) {
	content := `## HIGH Relevance
* Item using asterisk bullet

## MEDIUM Relevance
* Another asterisk item

## LOW Relevance
* Low item with asterisk
`
	high, medium, low := parseRelevanceItems(content)

	if len(high) != 1 {
		t.Errorf("high items = %d, want 1", len(high))
	}
	if len(medium) != 1 {
		t.Errorf("medium items = %d, want 1", len(medium))
	}
	if len(low) != 1 {
		t.Errorf("low items = %d, want 1", len(low))
	}
}

func TestParseRelevanceItems_EmptyContent(t *testing.T) {
	high, medium, low := parseRelevanceItems("")

	if len(high) != 0 {
		t.Errorf("high items = %d, want 0", len(high))
	}
	if len(medium) != 0 {
		t.Errorf("medium items = %d, want 0", len(medium))
	}
	if len(low) != 0 {
		t.Errorf("low items = %d, want 0", len(low))
	}
}

func TestParseRelevanceItems_NoSections(t *testing.T) {
	content := `Just some random text without any relevance sections.
- This bullet should not be captured since there is no section header.
`
	high, medium, low := parseRelevanceItems(content)

	if len(high) != 0 {
		t.Errorf("high items = %d, want 0", len(high))
	}
	if len(medium) != 0 {
		t.Errorf("medium items = %d, want 0", len(medium))
	}
	if len(low) != 0 {
		t.Errorf("low items = %d, want 0", len(low))
	}
}

// ---------------------------------------------------------------------------
// buildRelevanceSystemPrompt tests
// ---------------------------------------------------------------------------

func TestBuildRelevanceSystemPrompt_NoCustomContext(t *testing.T) {
	prompt := buildRelevanceSystemPrompt("")

	// Should contain standard sections but no custom context header.
	if !containsStr(prompt, "intelligence report for Datadog") {
		t.Error("prompt should contain Datadog intelligence report instruction")
	}
	if !containsStr(prompt, "## HIGH Relevance") {
		t.Error("prompt should contain HIGH Relevance format instruction")
	}
	if containsStr(prompt, "Additional Context from User") {
		t.Error("prompt should not contain custom context section when empty")
	}
}

func TestBuildRelevanceSystemPrompt_WithCustomContext(t *testing.T) {
	prompt := buildRelevanceSystemPrompt("Focus on profiling signal.")

	if !containsStr(prompt, "Additional Context from User") {
		t.Error("prompt should contain custom context section")
	}
	if !containsStr(prompt, "Focus on profiling signal.") {
		t.Error("prompt should contain the custom context content")
	}
}

// ---------------------------------------------------------------------------
// Context management tests
// ---------------------------------------------------------------------------

func TestLoadCustomContext_FileExists(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "context.txt")

	if err := os.WriteFile(filePath, []byte("My custom context content"), 0o644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	content, err := LoadCustomContext(filePath)
	if err != nil {
		t.Fatalf("LoadCustomContext failed: %v", err)
	}
	if content != "My custom context content" {
		t.Errorf("content = %q, want %q", content, "My custom context content")
	}
}

func TestLoadCustomContext_FileNotExists(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "nonexistent.txt")

	content, err := LoadCustomContext(filePath)
	if err != nil {
		t.Fatalf("LoadCustomContext should not error for missing file: %v", err)
	}
	if content != "" {
		t.Errorf("content should be empty for missing file, got %q", content)
	}
}

func TestSaveCustomContext(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "subdir", "context.txt")

	if err := SaveCustomContext(filePath, "Saved context content"); err != nil {
		t.Fatalf("SaveCustomContext failed: %v", err)
	}

	// Verify the file was written.
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading saved file: %v", err)
	}
	if string(data) != "Saved context content" {
		t.Errorf("saved content = %q, want %q", string(data), "Saved context content")
	}
}

func TestSaveAndLoadCustomContext_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "context.txt")

	original := "Round trip context with special chars: <>&\"\nSecond line."
	if err := SaveCustomContext(filePath, original); err != nil {
		t.Fatalf("SaveCustomContext failed: %v", err)
	}

	loaded, err := LoadCustomContext(filePath)
	if err != nil {
		t.Fatalf("LoadCustomContext failed: %v", err)
	}
	if loaded != original {
		t.Errorf("loaded = %q, want %q", loaded, original)
	}
}

func TestClearCustomContext_FileExists(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "context.txt")

	if err := os.WriteFile(filePath, []byte("content to clear"), 0o644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	if err := ClearCustomContext(filePath); err != nil {
		t.Fatalf("ClearCustomContext failed: %v", err)
	}

	// Verify the file is gone.
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("file should not exist after ClearCustomContext")
	}
}

func TestClearCustomContext_FileNotExists(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "nonexistent.txt")

	// Should not return an error for a missing file.
	if err := ClearCustomContext(filePath); err != nil {
		t.Fatalf("ClearCustomContext should not error for missing file: %v", err)
	}
}

// ---------------------------------------------------------------------------
// hashContent and buildCacheKey tests
// ---------------------------------------------------------------------------

func TestHashContent_Deterministic(t *testing.T) {
	h1 := hashContent("test content")
	h2 := hashContent("test content")
	if h1 != h2 {
		t.Errorf("hashContent is not deterministic: %q != %q", h1, h2)
	}

	h3 := hashContent("different content")
	if h1 == h3 {
		t.Error("hashContent should produce different hashes for different content")
	}
}

func TestBuildCacheKey_Deterministic(t *testing.T) {
	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	k1 := buildCacheKey("collector", "notes", start, end, "hash1")
	k2 := buildCacheKey("collector", "notes", start, end, "hash1")
	if k1 != k2 {
		t.Errorf("buildCacheKey is not deterministic: %q != %q", k1, k2)
	}

	// Different content hash should produce different cache key.
	k3 := buildCacheKey("collector", "notes", start, end, "hash2")
	if k1 == k3 {
		t.Error("buildCacheKey should produce different keys for different content hashes")
	}

	// Different source type should produce different cache key.
	k4 := buildCacheKey("collector", "video", start, end, "hash1")
	if k1 == k4 {
		t.Error("buildCacheKey should produce different keys for different source types")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
