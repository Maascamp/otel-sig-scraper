package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gordyrad/otel-sig-tracker/internal/analysis"
)

func newTestSIGReport() *analysis.SIGReport {
	return &analysis.SIGReport{
		SIGID:          "collector",
		SIGName:        "Collector",
		Category:       "implementation",
		DateRangeStart: "2026-02-11",
		DateRangeEnd:   "2026-02-18",
		SourcesUsed:    []string{"notes", "video", "slack"},
		SourcesMissing: nil,
		RelevanceReport: &analysis.RelevanceReport{
			SIGID:   "collector",
			SIGName: "Collector",
			Report: "#### HIGH Relevance\n" +
				"- **OTLP/HTTP Partial Success** — New partial success response support directly affects Datadog OTLP ingest. Review the OTEP draft.\n\n" +
				"#### MEDIUM Relevance\n" +
				"- **Pipeline Fan-out/Fan-in** — Architectural change for fan-out patterns could affect Datadog exporter pipeline.\n\n" +
				"#### LOW Relevance\n" +
				"- **Batch Processor Memory** — Minor memory improvements to batch processor.\n",
			HighItems:   []string{"**OTLP/HTTP Partial Success** — New partial success response support directly affects Datadog OTLP ingest. Review the OTEP draft."},
			MediumItems: []string{"**Pipeline Fan-out/Fan-in** — Architectural change for fan-out patterns could affect Datadog exporter pipeline."},
			LowItems:    []string{"**Batch Processor Memory** — Minor memory improvements to batch processor."},
			Model:       "claude-sonnet-4-20250514",
			TokensUsed:  1500,
		},
		NotesLink:     "https://docs.google.com/document/d/1r2JC5MB7ab",
		RecordingLink: "https://zoom.us/rec/share/abc123",
		SlackChannel:  "#otel-collector",
	}
}

func newTestDigestReport() *analysis.DigestReport {
	return &analysis.DigestReport{
		DateRangeStart: "2026-02-11",
		DateRangeEnd:   "2026-02-18",
		SIGReports: []*analysis.SIGReport{
			newTestSIGReport(),
			{
				SIGID:          "specification",
				SIGName:        "Specification",
				Category:       "specification",
				DateRangeStart: "2026-02-11",
				DateRangeEnd:   "2026-02-18",
				SourcesUsed:    []string{"notes"},
				SourcesMissing: []string{"video", "slack"},
				RelevanceReport: &analysis.RelevanceReport{
					SIGID:   "specification",
					SIGName: "Specification",
					Report: "#### HIGH Relevance\n" +
						"- **Profiling Signal OTEP** — New profiling signal specification affects Datadog profiling integration.\n\n" +
						"#### MEDIUM Relevance\nNone this period.\n\n" +
						"#### LOW Relevance\nNone this period.\n",
					HighItems:   []string{"**Profiling Signal OTEP** — New profiling signal specification affects Datadog profiling integration."},
					MediumItems: nil,
					LowItems:    nil,
					Model:       "claude-sonnet-4-20250514",
					TokensUsed:  800,
				},
				NotesLink:    "https://docs.google.com/document/d/spec456",
				SlackChannel: "#otel-specification",
			},
			{
				SIGID:          "empty-sig",
				SIGName:        "Empty SIG",
				Category:       "implementation",
				DateRangeStart: "2026-02-11",
				DateRangeEnd:   "2026-02-18",
				SourcesUsed:    nil,
				SourcesMissing: []string{"notes", "video", "slack"},
			},
		},
		CrossSIGThemes: "Both SIGs discussed improvements to the OTLP protocol.",
		Stats: &analysis.RunStats{
			TotalTokensUsed:  2300,
			TotalLLMCalls:    4,
			Model:            "claude-sonnet-4-20250514",
			Provider:         "anthropic",
			SIGsProcessed:    2,
			SIGsWithData:     2,
			DurationSeconds:  12.5,
			EstimatedCostUSD: 0.03,
		},
	}
}

// ---------------------------------------------------------------------------
// MarkdownGenerator tests
// ---------------------------------------------------------------------------

func TestMarkdownGenerator_GenerateSIGReport(t *testing.T) {
	dir := t.TempDir()
	gen := NewMarkdownGenerator(dir)
	report := newTestSIGReport()

	filePath, err := gen.GenerateSIGReport(report)
	if err != nil {
		t.Fatalf("GenerateSIGReport failed: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatalf("report file does not exist: %s", filePath)
	}

	// Verify filename pattern.
	filename := filepath.Base(filePath)
	if !strings.HasPrefix(filename, "2026-02-18-collector-report") {
		t.Errorf("filename = %q, expected prefix '2026-02-18-collector-report'", filename)
	}
	if !strings.HasSuffix(filename, ".md") {
		t.Errorf("filename = %q, expected .md suffix", filename)
	}

	// Read and verify content.
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading report file: %v", err)
	}
	content := string(data)

	// Verify title.
	if !strings.Contains(content, "# OTel Collector SIG Report") {
		t.Error("report should contain title with SIG name")
	}
	if !strings.Contains(content, "2026-02-11 to 2026-02-18") {
		t.Error("report should contain date range")
	}

	// Verify source status.
	if !strings.Contains(content, "meeting notes") {
		t.Error("report should mention meeting notes source")
	}

	// Verify NO "Executive Summary" section (removed).
	if strings.Contains(content, "## Executive Summary") {
		t.Error("report should NOT contain Executive Summary section")
	}

	// Verify NO H/M/L section headers.
	if strings.Contains(content, "High Relevance to Datadog") {
		t.Error("report should NOT contain High Relevance header")
	}
	if strings.Contains(content, "Medium Relevance to Datadog") {
		t.Error("report should NOT contain Medium Relevance header")
	}
	if strings.Contains(content, "Low Relevance") {
		t.Error("report should NOT contain Low Relevance header")
	}

	// Verify items still appear as flat bullets.
	if !strings.Contains(content, "OTLP/HTTP Partial Success") {
		t.Error("report should contain high relevance item")
	}
	if !strings.Contains(content, "Pipeline Fan-out/Fan-in") {
		t.Error("report should contain medium relevance item")
	}
	if !strings.Contains(content, "Batch Processor Memory") {
		t.Error("report should contain low relevance item")
	}

	// Verify inline data sources (replaced old "## Source Links" section).
	if strings.Contains(content, "## Source Links") {
		t.Error("report should NOT contain old Source Links section")
	}
	if !strings.Contains(content, "> Sources:") {
		t.Error("report should contain inline Sources line")
	}
	if !strings.Contains(content, "[Meeting Notes](https://docs.google.com/document/d/1r2JC5MB7ab)") {
		t.Error("report should contain notes link in inline sources")
	}
	if !strings.Contains(content, "[Recording](https://zoom.us/rec/share/abc123)") {
		t.Error("report should contain recording link in inline sources")
	}
	if !strings.Contains(content, "Slack: `#otel-collector`") {
		t.Error("report should contain Slack channel in inline sources")
	}
}

func TestMarkdownGenerator_GenerateSIGReport_NoRelevance(t *testing.T) {
	dir := t.TempDir()
	gen := NewMarkdownGenerator(dir)
	report := &analysis.SIGReport{
		SIGID:          "semconv",
		SIGName:        "Semantic Conventions",
		Category:       "cross-cutting",
		DateRangeStart: "2026-02-11",
		DateRangeEnd:   "2026-02-18",
		SourcesUsed:    nil,
		SourcesMissing: []string{"notes", "video", "slack"},
	}

	filePath, err := gen.GenerateSIGReport(report)
	if err != nil {
		t.Fatalf("GenerateSIGReport failed: %v", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading report file: %v", err)
	}
	content := string(data)

	// Should not contain relevance sections when RelevanceReport is nil.
	if strings.Contains(content, "## Executive Summary") {
		t.Error("report without relevance should not contain Executive Summary")
	}
	if strings.Contains(content, "High Relevance") {
		t.Error("report without relevance should not contain High Relevance")
	}

	// No links set, so inline Sources line should not appear.
	if strings.Contains(content, "> Sources:") {
		t.Error("report without links should not contain Sources line")
	}
}

func TestMarkdownGenerator_GenerateDigestReport(t *testing.T) {
	dir := t.TempDir()
	gen := NewMarkdownGenerator(dir)
	digest := newTestDigestReport()

	filePath, err := gen.GenerateDigestReport(digest)
	if err != nil {
		t.Fatalf("GenerateDigestReport failed: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatalf("digest file does not exist: %s", filePath)
	}

	// Verify filename pattern.
	filename := filepath.Base(filePath)
	if !strings.HasPrefix(filename, "2026-02-18-weekly-digest") {
		t.Errorf("filename = %q, expected prefix '2026-02-18-weekly-digest'", filename)
	}
	if !strings.HasSuffix(filename, ".md") {
		t.Errorf("filename = %q, expected .md suffix", filename)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading digest file: %v", err)
	}
	content := string(data)

	// Verify title.
	if !strings.Contains(content, "# OTel Weekly Digest") {
		t.Error("digest should contain title")
	}
	if !strings.Contains(content, "2026-02-11 to 2026-02-18") {
		t.Error("digest should contain date range")
	}

	// Verify new metadata format: "X SIGs with activity | Y quiet"
	if !strings.Contains(content, "2 SIGs with activity") {
		t.Error("digest should contain active SIG count")
	}
	if !strings.Contains(content, "1 quiet") {
		t.Error("digest should contain quiet SIG count")
	}

	// Verify "Top Takeaways" section.
	if !strings.Contains(content, "## Top Takeaways") {
		t.Error("digest should contain Top Takeaways section")
	}
	if !strings.Contains(content, "[Collector]") {
		t.Error("Top Takeaways should contain [Collector] attribution")
	}
	if !strings.Contains(content, "[Specification]") {
		t.Error("Top Takeaways should contain [Specification] attribution")
	}

	// Verify NO H/M/L section headers.
	if strings.Contains(content, "High Relevance to Datadog") {
		t.Error("digest should NOT contain High Relevance header")
	}
	if strings.Contains(content, "Medium Relevance to Datadog") {
		t.Error("digest should NOT contain Medium Relevance header")
	}

	// Verify inline data sources appear in digest for active SIGs.
	if !strings.Contains(content, "> Sources:") {
		t.Error("digest should contain inline Sources lines for active SIGs")
	}
	if !strings.Contains(content, "[Meeting Notes](https://docs.google.com/document/d/1r2JC5MB7ab)") {
		t.Error("digest should contain Collector notes link")
	}

	// Verify SIG-by-SIG summaries with new heading style.
	if !strings.Contains(content, "## SIG-by-SIG Summaries") {
		t.Error("digest should contain SIG-by-SIG Summaries section")
	}
	if !strings.Contains(content, "### Collector") {
		t.Error("digest should contain Collector SIG heading")
	}
	if !strings.Contains(content, "### Specification") {
		t.Error("digest should contain Specification SIG heading")
	}

	// Verify "Quiet This Week" section.
	if !strings.Contains(content, "## Quiet This Week") {
		t.Error("digest should contain Quiet This Week section")
	}
	if !strings.Contains(content, "Empty SIG") {
		t.Error("Quiet This Week should list Empty SIG")
	}

	// Empty SIGs should NOT appear in the summaries section.
	if strings.Contains(content, "### Empty SIG") {
		t.Error("digest should NOT contain empty SIG heading in summaries")
	}

	// Verify cross-SIG themes.
	if !strings.Contains(content, "## Cross-SIG Themes") {
		t.Error("digest should contain Cross-SIG Themes section")
	}
	if !strings.Contains(content, "Both SIGs discussed improvements to the OTLP protocol.") {
		t.Error("digest should contain cross-SIG themes content")
	}

	// Verify processing stats table.
	if !strings.Contains(content, "## Appendix: Processing Stats") {
		t.Error("digest should contain Processing Stats appendix")
	}
	if !strings.Contains(content, "| Collector |") {
		t.Error("digest should contain Collector row in stats table")
	}
	if !strings.Contains(content, "| Specification |") {
		t.Error("digest should contain Specification row in stats table")
	}
	if !strings.Contains(content, "| Empty SIG |") {
		t.Error("digest should contain Empty SIG row in stats table")
	}

	// Verify Run Info appendix.
	if !strings.Contains(content, "## Appendix: Run Info") {
		t.Error("digest should contain Run Info appendix")
	}
	if !strings.Contains(content, "anthropic") {
		t.Error("digest should contain LLM provider in Run Info")
	}
	if !strings.Contains(content, "claude-sonnet-4-20250514") {
		t.Error("digest should contain model name in Run Info")
	}
	if !strings.Contains(content, "$0.03") {
		t.Error("digest should contain estimated cost in Run Info")
	}
}

func TestMarkdownGenerator_GenerateDigestReport_NoCrossSIGThemes(t *testing.T) {
	dir := t.TempDir()
	gen := NewMarkdownGenerator(dir)
	digest := &analysis.DigestReport{
		DateRangeStart: "2026-02-11",
		DateRangeEnd:   "2026-02-18",
		SIGReports:     []*analysis.SIGReport{newTestSIGReport()},
		CrossSIGThemes: "",
	}

	filePath, err := gen.GenerateDigestReport(digest)
	if err != nil {
		t.Fatalf("GenerateDigestReport failed: %v", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading digest file: %v", err)
	}
	content := string(data)

	if strings.Contains(content, "## Cross-SIG Themes") {
		t.Error("digest with no cross-SIG themes should not contain that section")
	}
}

// ---------------------------------------------------------------------------
// JSONGenerator tests
// ---------------------------------------------------------------------------

func TestJSONGenerator_GenerateSIGReport(t *testing.T) {
	dir := t.TempDir()
	gen := NewJSONGenerator(dir)
	report := newTestSIGReport()

	filePath, err := gen.GenerateSIGReport(report)
	if err != nil {
		t.Fatalf("GenerateSIGReport failed: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatalf("report file does not exist: %s", filePath)
	}

	// Verify filename pattern.
	filename := filepath.Base(filePath)
	if !strings.HasPrefix(filename, "2026-02-18-collector-report") {
		t.Errorf("filename = %q, expected prefix '2026-02-18-collector-report'", filename)
	}
	if !strings.HasSuffix(filename, ".json") {
		t.Errorf("filename = %q, expected .json suffix", filename)
	}

	// Read and verify valid JSON.
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading report file: %v", err)
	}

	if !json.Valid(data) {
		t.Fatal("output is not valid JSON")
	}

	// Unmarshal and verify structure.
	var jr jsonSIGReport
	if err := json.Unmarshal(data, &jr); err != nil {
		t.Fatalf("unmarshaling JSON: %v", err)
	}

	if jr.SIGID != "collector" {
		t.Errorf("sig_id = %q, want %q", jr.SIGID, "collector")
	}
	if jr.SIGName != "Collector" {
		t.Errorf("sig_name = %q, want %q", jr.SIGName, "Collector")
	}
	if jr.Category != "implementation" {
		t.Errorf("category = %q, want %q", jr.Category, "implementation")
	}
	if jr.DateRangeStart != "2026-02-11" {
		t.Errorf("date_range_start = %q, want %q", jr.DateRangeStart, "2026-02-11")
	}
	if jr.DateRangeEnd != "2026-02-18" {
		t.Errorf("date_range_end = %q, want %q", jr.DateRangeEnd, "2026-02-18")
	}
	if len(jr.SourcesUsed) != 3 {
		t.Errorf("sources_used length = %d, want 3", len(jr.SourcesUsed))
	}
	if jr.NotesLink != "https://docs.google.com/document/d/1r2JC5MB7ab" {
		t.Errorf("notes_link = %q, unexpected", jr.NotesLink)
	}
	if jr.RecordingLink != "https://zoom.us/rec/share/abc123" {
		t.Errorf("recording_link = %q, unexpected", jr.RecordingLink)
	}
	if jr.SlackChannel != "#otel-collector" {
		t.Errorf("slack_channel = %q, unexpected", jr.SlackChannel)
	}
	if jr.GeneratedAt == "" {
		t.Error("generated_at should not be empty")
	}

	// Verify relevance section.
	if jr.Relevance == nil {
		t.Fatal("relevance should not be nil")
	}
	if len(jr.Relevance.HighItems) != 1 {
		t.Errorf("high_items length = %d, want 1", len(jr.Relevance.HighItems))
	}
	if len(jr.Relevance.MediumItems) != 1 {
		t.Errorf("medium_items length = %d, want 1", len(jr.Relevance.MediumItems))
	}
	if len(jr.Relevance.LowItems) != 1 {
		t.Errorf("low_items length = %d, want 1", len(jr.Relevance.LowItems))
	}
	if jr.Relevance.Model != "claude-sonnet-4-20250514" {
		t.Errorf("model = %q, want %q", jr.Relevance.Model, "claude-sonnet-4-20250514")
	}
	if jr.Relevance.TokensUsed != 1500 {
		t.Errorf("tokens_used = %d, want 1500", jr.Relevance.TokensUsed)
	}
}

func TestJSONGenerator_GenerateSIGReport_NoRelevance(t *testing.T) {
	dir := t.TempDir()
	gen := NewJSONGenerator(dir)
	report := &analysis.SIGReport{
		SIGID:          "semconv",
		SIGName:        "Semantic Conventions",
		Category:       "cross-cutting",
		DateRangeStart: "2026-02-11",
		DateRangeEnd:   "2026-02-18",
		SourcesUsed:    nil,
		SourcesMissing: []string{"notes", "video", "slack"},
	}

	filePath, err := gen.GenerateSIGReport(report)
	if err != nil {
		t.Fatalf("GenerateSIGReport failed: %v", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading report file: %v", err)
	}

	var jr jsonSIGReport
	if err := json.Unmarshal(data, &jr); err != nil {
		t.Fatalf("unmarshaling JSON: %v", err)
	}

	if jr.Relevance != nil {
		t.Error("relevance should be nil (omitempty) when no relevance report")
	}
	if len(jr.SourcesMissing) != 3 {
		t.Errorf("sources_missing length = %d, want 3", len(jr.SourcesMissing))
	}
}

func TestJSONGenerator_GenerateDigestReport(t *testing.T) {
	dir := t.TempDir()
	gen := NewJSONGenerator(dir)
	digest := newTestDigestReport()

	filePath, err := gen.GenerateDigestReport(digest)
	if err != nil {
		t.Fatalf("GenerateDigestReport failed: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatalf("digest file does not exist: %s", filePath)
	}

	// Verify filename pattern.
	filename := filepath.Base(filePath)
	if !strings.HasPrefix(filename, "2026-02-18-weekly-digest") {
		t.Errorf("filename = %q, expected prefix '2026-02-18-weekly-digest'", filename)
	}
	if !strings.HasSuffix(filename, ".json") {
		t.Errorf("filename = %q, expected .json suffix", filename)
	}

	// Read and verify valid JSON.
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading digest file: %v", err)
	}

	if !json.Valid(data) {
		t.Fatal("output is not valid JSON")
	}

	// Unmarshal and verify structure.
	var jd jsonDigestReport
	if err := json.Unmarshal(data, &jd); err != nil {
		t.Fatalf("unmarshaling JSON: %v", err)
	}

	if jd.DateRangeStart != "2026-02-11" {
		t.Errorf("date_range_start = %q, want %q", jd.DateRangeStart, "2026-02-11")
	}
	if jd.DateRangeEnd != "2026-02-18" {
		t.Errorf("date_range_end = %q, want %q", jd.DateRangeEnd, "2026-02-18")
	}
	if jd.SIGCount != 3 {
		t.Errorf("sig_count = %d, want 3", jd.SIGCount)
	}
	if len(jd.SIGReports) != 3 {
		t.Fatalf("sig_reports length = %d, want 3", len(jd.SIGReports))
	}
	if jd.CrossSIGThemes != "Both SIGs discussed improvements to the OTLP protocol." {
		t.Errorf("cross_sig_themes = %q, unexpected", jd.CrossSIGThemes)
	}
	if jd.GeneratedAt == "" {
		t.Error("generated_at should not be empty")
	}

	// Verify individual SIG reports in the digest.
	if jd.SIGReports[0].SIGID != "collector" {
		t.Errorf("first SIG report sig_id = %q, want %q", jd.SIGReports[0].SIGID, "collector")
	}
	if jd.SIGReports[1].SIGID != "specification" {
		t.Errorf("second SIG report sig_id = %q, want %q", jd.SIGReports[1].SIGID, "specification")
	}

	// Verify stats.
	if jd.Stats == nil {
		t.Fatal("stats should not be nil")
	}
	if jd.Stats.TotalTokensUsed != 2300 {
		t.Errorf("total_tokens_used = %d, want 2300", jd.Stats.TotalTokensUsed)
	}
	if jd.Stats.TotalLLMCalls != 4 {
		t.Errorf("total_llm_calls = %d, want 4", jd.Stats.TotalLLMCalls)
	}
	if jd.Stats.Model != "claude-sonnet-4-20250514" {
		t.Errorf("model = %q, want %q", jd.Stats.Model, "claude-sonnet-4-20250514")
	}
	if jd.Stats.Provider != "anthropic" {
		t.Errorf("provider = %q, want %q", jd.Stats.Provider, "anthropic")
	}
}

func TestJSONGenerator_GenerateDigestReport_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	gen := NewJSONGenerator(dir)
	digest := newTestDigestReport()

	filePath, err := gen.GenerateDigestReport(digest)
	if err != nil {
		t.Fatalf("GenerateDigestReport failed: %v", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("reading digest file: %v", err)
	}

	// Unmarshal into a generic map to verify round-trip.
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshaling into generic map: %v", err)
	}

	// Re-marshal and verify it's still valid JSON.
	reData, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("re-marshaling JSON: %v", err)
	}
	if !json.Valid(reData) {
		t.Fatal("re-marshaled output is not valid JSON")
	}
}

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestSourceStatus(t *testing.T) {
	used := []string{"notes", "video"}
	missing := []string{"slack"}

	tests := []struct {
		sourceType string
		want       string
	}{
		{"notes", "\xe2\x9c\x93"},   // checkmark
		{"video", "\xe2\x9c\x93"},   // checkmark
		{"slack", "\xe2\x9c\x97"},   // cross
		{"other", "\xe2\x80\x94"},   // em-dash
	}

	for _, tt := range tests {
		got := sourceStatus(tt.sourceType, used, missing)
		if got != tt.want {
			t.Errorf("sourceStatus(%q) = %q, want %q", tt.sourceType, got, tt.want)
		}
	}
}

func TestSigStatus(t *testing.T) {
	tests := []struct {
		name string
		sr   *analysis.SIGReport
		want string
	}{
		{
			name: "complete with relevance",
			sr: &analysis.SIGReport{
				RelevanceReport: &analysis.RelevanceReport{},
				SourcesUsed:     []string{"notes"},
			},
			want: "Complete",
		},
		{
			name: "partial with sources but no relevance",
			sr: &analysis.SIGReport{
				SourcesUsed: []string{"notes"},
			},
			want: "Partial",
		},
		{
			name: "no data",
			sr:   &analysis.SIGReport{},
			want: "No data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sigStatus(tt.sr)
			if got != tt.want {
				t.Errorf("sigStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatDateRange(t *testing.T) {
	tests := []struct {
		start, end string
		want       string
	}{
		{"2026-02-11", "2026-02-18", "2026-02-11 to 2026-02-18"},
		{"2026-02-15", "2026-02-15", "2026-02-15"},
		{"", "", "Unknown date range"},
		{"2026-02-11", "", "2026-02-11 to "},
	}

	for _, tt := range tests {
		got := formatDateRange(tt.start, tt.end)
		if got != tt.want {
			t.Errorf("formatDateRange(%q, %q) = %q, want %q", tt.start, tt.end, got, tt.want)
		}
	}
}

func TestSigReportFilename(t *testing.T) {
	got := sigReportFilename("2026-02-18", "collector")
	if got != "2026-02-18-collector-report.md" {
		t.Errorf("sigReportFilename = %q, want %q", got, "2026-02-18-collector-report.md")
	}

	// Test with spaces in SIG ID.
	got = sigReportFilename("2026-02-18", "semantic conventions")
	if got != "2026-02-18-semantic-conventions-report.md" {
		t.Errorf("sigReportFilename with spaces = %q, want %q", got, "2026-02-18-semantic-conventions-report.md")
	}
}

func TestSigReportJSONFilename(t *testing.T) {
	got := sigReportJSONFilename("2026-02-18", "collector")
	if got != "2026-02-18-collector-report.json" {
		t.Errorf("sigReportJSONFilename = %q, want %q", got, "2026-02-18-collector-report.json")
	}
}

func TestDigestFilename(t *testing.T) {
	got := digestFilename("2026-02-18")
	if got != "2026-02-18-weekly-digest.md" {
		t.Errorf("digestFilename = %q, want %q", got, "2026-02-18-weekly-digest.md")
	}
}

func TestDigestJSONFilename(t *testing.T) {
	got := digestJSONFilename("2026-02-18")
	if got != "2026-02-18-weekly-digest.json" {
		t.Errorf("digestJSONFilename = %q, want %q", got, "2026-02-18-weekly-digest.json")
	}
}

func TestFilenameEmptyDate(t *testing.T) {
	// When date is empty, filenames should use today's date.
	// We just verify they don't panic and have reasonable format.
	md := sigReportFilename("", "collector")
	if !strings.HasSuffix(md, "-collector-report.md") {
		t.Errorf("sigReportFilename with empty date = %q, unexpected format", md)
	}

	jsonf := sigReportJSONFilename("", "collector")
	if !strings.HasSuffix(jsonf, "-collector-report.json") {
		t.Errorf("sigReportJSONFilename with empty date = %q, unexpected format", jsonf)
	}

	digestMd := digestFilename("")
	if !strings.HasSuffix(digestMd, "-weekly-digest.md") {
		t.Errorf("digestFilename with empty date = %q, unexpected format", digestMd)
	}

	digestJson := digestJSONFilename("")
	if !strings.HasSuffix(digestJson, "-weekly-digest.json") {
		t.Errorf("digestJSONFilename with empty date = %q, unexpected format", digestJson)
	}
}

// ---------------------------------------------------------------------------
// New helper function tests
// ---------------------------------------------------------------------------

func TestDeduplicateDigestSIGs(t *testing.T) {
	reports := []*analysis.SIGReport{
		{
			SIGName: "Collector",
			RelevanceReport: &analysis.RelevanceReport{
				HighItems: []string{"item1", "item2"},
			},
		},
		{
			SIGName: "🔧 Collector",
			RelevanceReport: &analysis.RelevanceReport{
				HighItems: []string{"item1"},
			},
		},
		{
			SIGName: "Specification",
			RelevanceReport: &analysis.RelevanceReport{
				HighItems: []string{"spec-item"},
			},
		},
	}

	result := deduplicateDigestSIGs(reports)

	if len(result) != 2 {
		t.Fatalf("deduplicateDigestSIGs returned %d entries, want 2", len(result))
	}

	// The first entry should be "Collector" (has more items).
	if result[0].SIGName != "Collector" {
		t.Errorf("first entry SIGName = %q, want %q", result[0].SIGName, "Collector")
	}
	if result[1].SIGName != "Specification" {
		t.Errorf("second entry SIGName = %q, want %q", result[1].SIGName, "Specification")
	}
}

func TestNormalizeSIGName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Collector", "collector"},
		{"🔧 Collector", "collector"},
		{"  Collector  SIG  ", "collector sig"},
		{"Semantic&amp;Conventions", "semantic&conventions"},
		{"Test&#8211;SIG", "test\u2013sig"},
		{"COLLECTOR", "collector"},
	}

	for _, tt := range tests {
		got := normalizeSIGName(tt.input)
		if got != tt.want {
			t.Errorf("normalizeSIGName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEnsureBoldTopic(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Already bold — no change.
		{"**OTLP** — details here", "**OTLP** — details here"},
		// Has em-dash separator — bold the topic.
		{"OTLP Changes — details here", "**OTLP Changes** — details here"},
		// Has colon separator — bold the topic.
		{"OTLP Changes: details here", "**OTLP Changes**: details here"},
		// No separator — returned as-is.
		{"Just a plain item", "Just a plain item"},
	}

	for _, tt := range tests {
		got := ensureBoldTopic(tt.input)
		if got != tt.want {
			t.Errorf("ensureBoldTopic(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStripReportHeading(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no heading",
			input: "#### Section One\nContent here.",
			want:  "#### Section One\nContent here.",
		},
		{
			name:  "with title heading",
			input: "# Datadog Intelligence Report: OpenTelemetry Communications SIG\n\n#### Section One\nContent here.",
			want:  "#### Section One\nContent here.",
		},
		{
			name:  "with title and subtitle",
			input: "# Datadog Relevance Report: OpenTelemetry Logs SIG (Feb 12-19, 2026)\n**Analysis Period: Feb 12-19, 2026**\n\n#### Section One\nContent here.",
			want:  "#### Section One\nContent here.",
		},
		{
			name:  "heading only",
			input: "# Just a Heading",
			want:  "",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "preserves #### headings",
			input: "#### This is a section\nContent.",
			want:  "#### This is a section\nContent.",
		},
		{
			name:  "preserves ## headings that are not top-level",
			input: "## Overview\n#### Section\nContent.",
			want:  "## Overview\n#### Section\nContent.",
		},
		{
			name:  "multiple blank lines between heading and content",
			input: "# Title\n\n\n#### Section\nContent.",
			want:  "#### Section\nContent.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripReportHeading(tt.input)
			if got != tt.want {
				t.Errorf("stripReportHeading() =\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
}

func TestTotalRelevanceItems(t *testing.T) {
	rr := &analysis.RelevanceReport{
		HighItems:   []string{"a", "b"},
		MediumItems: []string{"c"},
		LowItems:    []string{"d", "e", "f"},
	}
	if got := totalRelevanceItems(rr); got != 6 {
		t.Errorf("totalRelevanceItems = %d, want 6", got)
	}

	if got := totalRelevanceItems(nil); got != 0 {
		t.Errorf("totalRelevanceItems(nil) = %d, want 0", got)
	}
}

func TestWriteDataSources(t *testing.T) {
	tests := []struct {
		name     string
		sr       *analysis.SIGReport
		wantEmpty bool
		contains []string
	}{
		{
			name: "all sources present",
			sr: &analysis.SIGReport{
				NotesLink:     "https://docs.google.com/doc/123",
				RecordingLink: "https://zoom.us/rec/abc",
				SlackChannel:  "#otel-collector",
			},
			contains: []string{
				"> Sources:",
				"[Meeting Notes](https://docs.google.com/doc/123)",
				"[Recording](https://zoom.us/rec/abc)",
				"Slack: `#otel-collector`",
				" | ",
			},
		},
		{
			name: "notes only",
			sr: &analysis.SIGReport{
				NotesLink: "https://docs.google.com/doc/456",
			},
			contains: []string{
				"> Sources: [Meeting Notes](https://docs.google.com/doc/456)",
			},
		},
		{
			name:      "no links",
			sr:        &analysis.SIGReport{},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b strings.Builder
			writeDataSources(&b, tt.sr)
			got := b.String()

			if tt.wantEmpty {
				if got != "" {
					t.Errorf("expected empty output, got %q", got)
				}
				return
			}

			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q, got:\n%s", want, got)
				}
			}
		})
	}
}
