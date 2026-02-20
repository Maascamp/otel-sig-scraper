package report

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gordyrad/otel-sig-tracker/internal/analysis"
)

// MarkdownGenerator writes Markdown-formatted reports to disk.
type MarkdownGenerator struct {
	outputDir string
}

// NewMarkdownGenerator creates a new MarkdownGenerator that writes to outputDir.
func NewMarkdownGenerator(outputDir string) *MarkdownGenerator {
	return &MarkdownGenerator{outputDir: outputDir}
}

// GenerateSIGReport generates a per-SIG Markdown report and returns the file path.
func (g *MarkdownGenerator) GenerateSIGReport(report *analysis.SIGReport) (string, error) {
	if err := os.MkdirAll(g.outputDir, 0o755); err != nil {
		return "", fmt.Errorf("creating output directory: %w", err)
	}

	var b strings.Builder

	// Title
	dateRange := formatDateRange(report.DateRangeStart, report.DateRangeEnd)
	fmt.Fprintf(&b, "# OTel %s SIG Report — %s\n\n", report.SIGName, dateRange)

	// Metadata line
	notesStatus := sourceStatus("notes", report.SourcesUsed, report.SourcesMissing)
	videoStatus := sourceStatus("video", report.SourcesUsed, report.SourcesMissing)
	slackStatus := sourceStatus("slack", report.SourcesUsed, report.SourcesMissing)
	fmt.Fprintf(&b, "> Generated: %s | Sources: meeting notes %s video %s slack %s\n\n",
		time.Now().UTC().Format("2006-01-02 15:04 UTC"),
		notesStatus, videoStatus, slackStatus,
	)

	// Executive Summary
	if report.RelevanceReport != nil && report.RelevanceReport.Report != "" {
		b.WriteString("## Executive Summary\n\n")
		b.WriteString(report.RelevanceReport.Report)
		b.WriteString("\n\n")
	}

	// High Relevance Items
	if report.RelevanceReport != nil && len(report.RelevanceReport.HighItems) > 0 {
		b.WriteString("#### 🔴 High Relevance to Datadog\n\n")
		for _, item := range report.RelevanceReport.HighItems {
			writeRelevanceItem(&b, item)
		}
	}

	// Medium Relevance Items
	if report.RelevanceReport != nil && len(report.RelevanceReport.MediumItems) > 0 {
		b.WriteString("#### 🟡 Medium Relevance to Datadog\n\n")
		for _, item := range report.RelevanceReport.MediumItems {
			writeRelevanceItem(&b, item)
		}
	}

	// Low Relevance Items
	if report.RelevanceReport != nil && len(report.RelevanceReport.LowItems) > 0 {
		b.WriteString("#### 🟢 Low Relevance / FYI\n\n")
		for _, item := range report.RelevanceReport.LowItems {
			fmt.Fprintf(&b, "- %s\n", item)
		}
		b.WriteString("\n")
	}

	// Source Links
	b.WriteString("## Source Links\n\n")
	if report.NotesLink != "" {
		fmt.Fprintf(&b, "- Meeting Notes: %s\n", report.NotesLink)
	}
	if report.RecordingLink != "" {
		fmt.Fprintf(&b, "- Recording: %s\n", report.RecordingLink)
	}
	if report.SlackChannel != "" {
		fmt.Fprintf(&b, "- Slack Channel: %s\n", report.SlackChannel)
	}
	b.WriteString("\n")

	// Write file
	filename := sigReportFilename(report.DateRangeEnd, report.SIGID)
	filePath := filepath.Join(g.outputDir, filename)

	if err := os.WriteFile(filePath, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("writing SIG report: %w", err)
	}

	return filePath, nil
}

// GenerateDigestReport generates a weekly digest Markdown report and returns the file path.
func (g *MarkdownGenerator) GenerateDigestReport(digest *analysis.DigestReport) (string, error) {
	if err := os.MkdirAll(g.outputDir, 0o755); err != nil {
		return "", fmt.Errorf("creating output directory: %w", err)
	}

	var b strings.Builder

	// Title
	dateRange := formatDateRange(digest.DateRangeStart, digest.DateRangeEnd)
	fmt.Fprintf(&b, "# OTel Weekly Digest — %s\n\n", dateRange)

	// Metadata line
	fmt.Fprintf(&b, "> Covering: %d SIGs | Generated: %s\n\n",
		len(digest.SIGReports),
		time.Now().UTC().Format("2006-01-02 15:04 UTC"),
	)

	// SIG-by-SIG Summaries (only SIGs with analysis data)
	b.WriteString("## SIG-by-SIG Summaries\n\n")
	for _, sr := range digest.SIGReports {
		if sr.RelevanceReport == nil || sr.RelevanceReport.Report == "" {
			continue
		}
		fmt.Fprintf(&b, "# %s\n\n", sr.SIGName)
		b.WriteString(stripReportHeading(sr.RelevanceReport.Report))
		b.WriteString("\n\n")
	}

	// Cross-SIG Themes
	if digest.CrossSIGThemes != "" {
		b.WriteString("## Cross-SIG Themes\n\n")
		b.WriteString(digest.CrossSIGThemes)
		b.WriteString("\n\n")
	}

	// Appendix: Processing Stats
	b.WriteString("## Appendix: Processing Stats\n\n")
	b.WriteString("| SIG | Notes | Video | Slack | Status |\n")
	b.WriteString("|-----|-------|-------|-------|--------|\n")
	for _, sr := range digest.SIGReports {
		notes := sourceStatus("notes", sr.SourcesUsed, sr.SourcesMissing)
		video := sourceStatus("video", sr.SourcesUsed, sr.SourcesMissing)
		slack := sourceStatus("slack", sr.SourcesUsed, sr.SourcesMissing)
		status := sigStatus(sr)
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s |\n",
			sr.SIGName, notes, video, slack, status,
		)
	}
	b.WriteString("\n")

	// Appendix: Run Info
	if digest.Stats != nil {
		b.WriteString("## Appendix: Run Info\n\n")
		b.WriteString("| Metric | Value |\n")
		b.WriteString("|--------|-------|\n")
		fmt.Fprintf(&b, "| LLM Provider | %s |\n", digest.Stats.Provider)
		fmt.Fprintf(&b, "| Model | `%s` |\n", digest.Stats.Model)
		fmt.Fprintf(&b, "| Total Tokens Used | %s |\n", formatTokens(digest.Stats.TotalTokensUsed))
		fmt.Fprintf(&b, "| LLM Calls | %d |\n", digest.Stats.TotalLLMCalls)
		fmt.Fprintf(&b, "| Estimated Cost | $%.2f |\n", digest.Stats.EstimatedCostUSD)
		fmt.Fprintf(&b, "| SIGs Processed | %d |\n", digest.Stats.SIGsProcessed)
		fmt.Fprintf(&b, "| SIGs With Data | %d |\n", digest.Stats.SIGsWithData)
		fmt.Fprintf(&b, "| Duration | %.1fs |\n", digest.Stats.DurationSeconds)
		b.WriteString("\n")
	}

	// Write file
	filename := digestFilename(digest.DateRangeEnd)
	filePath := filepath.Join(g.outputDir, filename)

	if err := os.WriteFile(filePath, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("writing digest report: %w", err)
	}

	return filePath, nil
}

// sourceStatus returns a checkmark or cross for whether a source type was used or missing.
func sourceStatus(sourceType string, used, missing []string) string {
	for _, s := range used {
		if s == sourceType {
			return "✓"
		}
	}
	for _, s := range missing {
		if s == sourceType {
			return "✗"
		}
	}
	return "—"
}

// sigStatus returns a short status string for the SIG report.
func sigStatus(sr *analysis.SIGReport) string {
	if sr.RelevanceReport != nil {
		return "Complete"
	}
	if len(sr.SourcesUsed) > 0 {
		return "Partial"
	}
	return "No data"
}

// writeRelevanceItem writes a single relevance item as a Markdown section.
// Items are written as-is since the LLM formats them with bullet details.
func writeRelevanceItem(b *strings.Builder, item string) {
	// Each item may contain structured markdown from the LLM.
	// Write it directly, ensuring proper spacing.
	b.WriteString(item)
	b.WriteString("\n\n")
}

// formatDateRange creates a readable date range string.
func formatDateRange(start, end string) string {
	if start == "" && end == "" {
		return "Unknown date range"
	}
	if start == end {
		return start
	}
	return fmt.Sprintf("%s to %s", start, end)
}

// sigReportFilename generates a filename like "2026-02-19-collector-report.md".
func sigReportFilename(dateEnd, sigID string) string {
	// Use the end date for the filename; fall back to today if empty.
	date := dateEnd
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	slug := strings.ToLower(strings.ReplaceAll(sigID, " ", "-"))
	return fmt.Sprintf("%s-%s-report.md", date, slug)
}

// formatTokens formats a token count with commas for readability.
func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
}

// digestFilename generates a filename like "2026-02-19-weekly-digest.md".
func digestFilename(dateEnd string) string {
	date := dateEnd
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	return fmt.Sprintf("%s-weekly-digest.md", date)
}

// stripReportHeading removes the leading title heading and optional subtitle
// lines that the LLM inconsistently adds to its report output. It strips any
// leading lines starting with "#"–"###" or "**Analysis" before the first "####" section.
func stripReportHeading(text string) string {
	lines := strings.Split(text, "\n")
	start := 0
	for start < len(lines) {
		trimmed := strings.TrimSpace(lines[start])
		if trimmed == "" {
			// Skip blank lines between heading and content.
			start++
			continue
		}
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "#### ") {
			// Heading levels 1–3 added by LLM — skip them.
			start++
			continue
		}
		if strings.HasPrefix(trimmed, "**Analysis") {
			// Subtitle line like "**Analysis Period: ...**" — skip it.
			start++
			continue
		}
		// Reached real content.
		break
	}
	if start == 0 {
		return text
	}
	return strings.Join(lines[start:], "\n")
}
