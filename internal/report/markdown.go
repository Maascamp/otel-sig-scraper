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
		b.WriteString("## 🔴 High Relevance to Datadog\n\n")
		for _, item := range report.RelevanceReport.HighItems {
			writeRelevanceItem(&b, item)
		}
	}

	// Medium Relevance Items
	if report.RelevanceReport != nil && len(report.RelevanceReport.MediumItems) > 0 {
		b.WriteString("## 🟡 Medium Relevance to Datadog\n\n")
		for _, item := range report.RelevanceReport.MediumItems {
			writeRelevanceItem(&b, item)
		}
	}

	// Low Relevance Items
	if report.RelevanceReport != nil && len(report.RelevanceReport.LowItems) > 0 {
		b.WriteString("## 🟢 Low Relevance / FYI\n\n")
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

	// Top Items for Datadog - collect all high-relevance items across SIGs
	var topItems []string
	for _, sr := range digest.SIGReports {
		if sr.RelevanceReport != nil {
			for _, item := range sr.RelevanceReport.HighItems {
				topItems = append(topItems, fmt.Sprintf("**[%s]** %s", sr.SIGName, item))
			}
		}
	}
	if len(topItems) > 0 {
		b.WriteString("## 🔴 Top Items for Datadog\n\n")
		for i, item := range topItems {
			fmt.Fprintf(&b, "%d. %s\n", i+1, item)
		}
		b.WriteString("\n")
	}

	// SIG-by-SIG Summaries
	b.WriteString("## SIG-by-SIG Summaries\n\n")
	for _, sr := range digest.SIGReports {
		fmt.Fprintf(&b, "### %s\n\n", sr.SIGName)
		if sr.RelevanceReport != nil && sr.RelevanceReport.Report != "" {
			b.WriteString(sr.RelevanceReport.Report)
			b.WriteString("\n\n")
		} else {
			b.WriteString("_No analysis available._\n\n")
		}
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

// digestFilename generates a filename like "2026-02-19-weekly-digest.md".
func digestFilename(dateEnd string) string {
	date := dateEnd
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	return fmt.Sprintf("%s-weekly-digest.md", date)
}
