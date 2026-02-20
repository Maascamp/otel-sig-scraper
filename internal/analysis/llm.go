package analysis

import "context"

// LLMClient is the interface for LLM providers.
type LLMClient interface {
	// Complete sends a prompt to the LLM and returns the response.
	Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
}

// CompletionRequest represents a request to the LLM.
type CompletionRequest struct {
	SystemPrompt string
	UserPrompt   string
	MaxTokens    int
	Temperature  float64
}

// CompletionResponse represents a response from the LLM.
type CompletionResponse struct {
	Content    string
	Model      string
	TokensUsed int
}

// SourceSummary holds a per-source summary for a SIG.
type SourceSummary struct {
	SIGID      string
	SIGName    string
	SourceType string // "notes", "video", "slack"
	Summary    string
	Model      string
	TokensUsed int
}

// SynthesizedReport holds the cross-source synthesized report.
type SynthesizedReport struct {
	SIGID      string
	SIGName    string
	Synthesis  string
	Model      string
	TokensUsed int
}

// RelevanceReport holds the Datadog relevance-scored report.
type RelevanceReport struct {
	SIGID          string
	SIGName        string
	Report         string
	HighItems      []string
	MediumItems    []string
	LowItems       []string
	Model          string
	TokensUsed     int
}

// SIGReport is the final combined report for a single SIG.
type SIGReport struct {
	SIGID           string
	SIGName         string
	Category        string
	DateRangeStart  string
	DateRangeEnd    string
	SourcesUsed     []string // which sources were available
	SourcesMissing  []string // which sources failed/missing
	RelevanceReport *RelevanceReport
	NotesLink       string
	RecordingLink   string
	SlackChannel    string
}

// RunStats tracks resource usage for the entire pipeline run.
type RunStats struct {
	TotalTokensUsed   int
	TotalLLMCalls     int
	Model             string
	Provider          string
	SIGsProcessed     int
	SIGsWithData      int
	DurationSeconds   float64
	EstimatedCostUSD  float64
}

// DigestReport is the weekly digest across all SIGs.
type DigestReport struct {
	DateRangeStart string
	DateRangeEnd   string
	SIGReports     []*SIGReport
	CrossSIGThemes string
	Stats          *RunStats
}
