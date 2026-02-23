package report

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

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

	// Relevance items as a flat priority-ordered list (no H/M/L headers)
	if report.RelevanceReport != nil {
		writeRelevanceItemsFlat(&b, report.RelevanceReport)
	}

	// Inline data sources
	writeDataSources(&b, report)

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

	// Deduplicate SIG reports by normalized name.
	deduped := deduplicateDigestSIGs(digest.SIGReports)

	// Partition into active (has relevance data) and quiet (no data).
	var active, quiet []*analysis.SIGReport
	for _, sr := range deduped {
		if sr.RelevanceReport != nil && totalRelevanceItems(sr.RelevanceReport) > 0 {
			active = append(active, sr)
		} else {
			quiet = append(quiet, sr)
		}
	}

	var b strings.Builder

	// Title
	dateRange := formatDateRange(digest.DateRangeStart, digest.DateRangeEnd)
	fmt.Fprintf(&b, "# OTel Weekly Digest — %s\n\n", dateRange)

	// Metadata line
	fmt.Fprintf(&b, "> %d SIGs with activity | %d quiet | Generated: %s\n\n",
		len(active),
		len(quiet),
		time.Now().UTC().Format("2006-01-02 15:04 UTC"),
	)

	// Top Takeaways — top high-relevance items across all SIGs
	writeTopTakeaways(&b, active)

	// SIG-by-SIG Summaries (only active SIGs, flat priority-ordered items)
	b.WriteString("## SIG-by-SIG Summaries\n\n")
	for _, sr := range active {
		fmt.Fprintf(&b, "### %s\n\n", sr.SIGName)
		writeRelevanceItemsFlat(&b, sr.RelevanceReport)
		writeDataSources(&b, sr)
	}

	// Quiet This Week — one-line list of inactive SIGs
	if len(quiet) > 0 {
		b.WriteString("## Quiet This Week\n\n")
		names := make([]string, len(quiet))
		for i, sr := range quiet {
			names[i] = sr.SIGName
		}
		fmt.Fprintf(&b, "%s\n\n", strings.Join(names, ", "))
	}

	// Cross-SIG Themes
	if digest.CrossSIGThemes != "" {
		b.WriteString("## Cross-SIG Themes\n\n")
		b.WriteString(digest.CrossSIGThemes)
		b.WriteString("\n\n")
	}

	// Appendix: Processing Stats (uses deduped list)
	b.WriteString("## Appendix: Processing Stats\n\n")
	b.WriteString("| SIG | Notes | Video | Slack | Status |\n")
	b.WriteString("|-----|-------|-------|-------|--------|\n")
	for _, sr := range deduped {
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

// writeTopTakeaways collects high-relevance items across SIGs and writes the top 10
// with [SIG] attribution.
func writeTopTakeaways(b *strings.Builder, active []*analysis.SIGReport) {
	type attributed struct {
		sigName string
		item    string
	}
	var items []attributed
	for _, sr := range active {
		if sr.RelevanceReport == nil {
			continue
		}
		for _, item := range sr.RelevanceReport.HighItems {
			items = append(items, attributed{sigName: sr.SIGName, item: item})
		}
	}
	if len(items) == 0 {
		return
	}

	b.WriteString("## Top Takeaways\n\n")
	limit := 10
	if len(items) < limit {
		limit = len(items)
	}
	for i := 0; i < limit; i++ {
		fmt.Fprintf(b, "- [%s] %s\n", items[i].sigName, ensureBoldTopic(items[i].item))
	}
	b.WriteString("\n")
}

// writeRelevanceItemsFlat renders high, medium, low items as one flat priority-ordered
// bullet list with no section headers.
func writeRelevanceItemsFlat(b *strings.Builder, rr *analysis.RelevanceReport) {
	if rr == nil {
		return
	}
	hasItems := len(rr.HighItems) > 0 || len(rr.MediumItems) > 0 || len(rr.LowItems) > 0
	if !hasItems {
		return
	}
	for _, item := range rr.HighItems {
		fmt.Fprintf(b, "- %s\n", ensureBoldTopic(item))
	}
	for _, item := range rr.MediumItems {
		fmt.Fprintf(b, "- %s\n", ensureBoldTopic(item))
	}
	for _, item := range rr.LowItems {
		fmt.Fprintf(b, "- %s\n", ensureBoldTopic(item))
	}
	b.WriteString("\n")
}

// writeDataSources renders a compact inline sources line for a SIG report.
// If no links are present, nothing is written.
func writeDataSources(b *strings.Builder, sr *analysis.SIGReport) {
	if sr.NotesLink == "" && sr.RecordingLink == "" && sr.SlackChannel == "" {
		return
	}
	var parts []string
	if sr.NotesLink != "" {
		parts = append(parts, fmt.Sprintf("[Meeting Notes](%s)", sr.NotesLink))
	}
	if sr.RecordingLink != "" {
		parts = append(parts, fmt.Sprintf("[Recording](%s)", sr.RecordingLink))
	}
	if sr.SlackChannel != "" {
		parts = append(parts, fmt.Sprintf("Slack: `%s`", sr.SlackChannel))
	}
	fmt.Fprintf(b, "> Sources: %s\n\n", strings.Join(parts, " | "))
}

// ensureBoldTopic ensures the item starts with a **bold topic** prefix.
// If the item already starts with **, it is returned as-is.
// Otherwise it tries to bold the text before the first colon or em-dash separator.
func ensureBoldTopic(item string) string {
	if strings.HasPrefix(item, "**") {
		return item
	}
	// Look for a natural separator: " — " (em-dash) or ": "
	for _, sep := range []string{" — ", ": "} {
		if idx := strings.Index(item, sep); idx > 0 && idx < 80 {
			return "**" + item[:idx] + "**" + item[idx:]
		}
	}
	return item
}

// emojiPattern matches common emoji sequences (single and multi-codepoint).
var emojiPattern = regexp.MustCompile(`[\x{1F000}-\x{1FFFF}]|[\x{2600}-\x{27BF}]|[\x{FE00}-\x{FE0F}]|[\x{200D}]|[\x{20E3}]|[\x{E0020}-\x{E007F}]`)

// htmlEntityPattern matches HTML entities like &amp; &#8211; etc.
var htmlEntityPattern = regexp.MustCompile(`&[a-zA-Z]+;|&#[0-9]+;|&#x[0-9a-fA-F]+;`)

// normalizeSIGName normalizes a SIG name for deduplication by lowercasing,
// stripping emoji, HTML entities, and collapsing whitespace.
func normalizeSIGName(name string) string {
	// Decode HTML entities first (e.g. &amp; -> &), then strip remaining patterns.
	s := html.UnescapeString(name)
	// Strip HTML entity patterns that survived.
	s = htmlEntityPattern.ReplaceAllString(s, "")
	// Strip emoji.
	s = emojiPattern.ReplaceAllString(s, "")
	// Lowercase.
	s = strings.ToLower(s)
	// Collapse whitespace and trim.
	fields := strings.FieldsFunc(s, unicode.IsSpace)
	return strings.Join(fields, " ")
}

// deduplicateDigestSIGs merges SIG reports that have the same normalized name,
// keeping the entry with the most relevance items.
func deduplicateDigestSIGs(reports []*analysis.SIGReport) []*analysis.SIGReport {
	type entry struct {
		report *analysis.SIGReport
		count  int
	}
	seen := make(map[string]*entry)
	var order []string

	for _, sr := range reports {
		key := normalizeSIGName(sr.SIGName)
		count := 0
		if sr.RelevanceReport != nil {
			count = totalRelevanceItems(sr.RelevanceReport)
		}
		if existing, ok := seen[key]; ok {
			if count > existing.count {
				seen[key] = &entry{report: sr, count: count}
			}
		} else {
			seen[key] = &entry{report: sr, count: count}
			order = append(order, key)
		}
	}

	result := make([]*analysis.SIGReport, 0, len(order))
	for _, key := range order {
		result = append(result, seen[key].report)
	}
	return result
}

// totalRelevanceItems returns the total number of items across all relevance levels.
func totalRelevanceItems(rr *analysis.RelevanceReport) int {
	if rr == nil {
		return 0
	}
	return len(rr.HighItems) + len(rr.MediumItems) + len(rr.LowItems)
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

// stripReportHeading removes the leading title heading and optional subtitle
// lines that the LLM inconsistently adds to its report output. It strips any
// leading lines starting with "#"–"###" or "**Analysis" before the first "####" section.
func stripReportHeading(text string) string {
	lines := strings.Split(text, "\n")
	start := 0
	for start < len(lines) {
		trimmed := strings.TrimSpace(lines[start])
		if trimmed == "" {
			start++
			continue
		}
		if strings.HasPrefix(trimmed, "# ") && !strings.HasPrefix(trimmed, "#### ") {
			start++
			continue
		}
		if strings.HasPrefix(trimmed, "**Analysis") {
			start++
			continue
		}
		break
	}
	if start == 0 {
		return text
	}
	return strings.Join(lines[start:], "\n")
}

// digestFilename generates a filename like "2026-02-19-weekly-digest.md".
func digestFilename(dateEnd string) string {
	date := dateEnd
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	return fmt.Sprintf("%s-weekly-digest.md", date)
}
