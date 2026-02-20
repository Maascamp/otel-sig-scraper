package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gordyrad/otel-sig-tracker/internal/analysis"
)

// JSONGenerator writes JSON-formatted reports to disk.
type JSONGenerator struct {
	outputDir string
}

// NewJSONGenerator creates a new JSONGenerator that writes to outputDir.
func NewJSONGenerator(outputDir string) *JSONGenerator {
	return &JSONGenerator{outputDir: outputDir}
}

// jsonSIGReport is the JSON-serializable form of a SIG report.
type jsonSIGReport struct {
	SIGID          string             `json:"sig_id"`
	SIGName        string             `json:"sig_name"`
	Category       string             `json:"category"`
	DateRangeStart string             `json:"date_range_start"`
	DateRangeEnd   string             `json:"date_range_end"`
	SourcesUsed    []string           `json:"sources_used"`
	SourcesMissing []string           `json:"sources_missing"`
	Relevance      *jsonRelevance     `json:"relevance,omitempty"`
	NotesLink      string             `json:"notes_link,omitempty"`
	RecordingLink  string             `json:"recording_link,omitempty"`
	SlackChannel   string             `json:"slack_channel,omitempty"`
	GeneratedAt    string             `json:"generated_at"`
}

// jsonRelevance is the JSON-serializable form of a relevance report.
type jsonRelevance struct {
	Report      string   `json:"report"`
	HighItems   []string `json:"high_items"`
	MediumItems []string `json:"medium_items"`
	LowItems    []string `json:"low_items"`
	Model       string   `json:"model"`
	TokensUsed  int      `json:"tokens_used"`
}

// jsonDigestReport is the JSON-serializable form of a digest report.
type jsonDigestReport struct {
	DateRangeStart string           `json:"date_range_start"`
	DateRangeEnd   string           `json:"date_range_end"`
	SIGCount       int              `json:"sig_count"`
	SIGReports     []*jsonSIGReport `json:"sig_reports"`
	CrossSIGThemes string           `json:"cross_sig_themes,omitempty"`
	GeneratedAt    string           `json:"generated_at"`
}

// GenerateSIGReport generates a per-SIG JSON report and returns the file path.
func (g *JSONGenerator) GenerateSIGReport(report *analysis.SIGReport) (string, error) {
	if err := os.MkdirAll(g.outputDir, 0o755); err != nil {
		return "", fmt.Errorf("creating output directory: %w", err)
	}

	jr := toJSONSIGReport(report)

	data, err := json.MarshalIndent(jr, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling SIG report to JSON: %w", err)
	}

	filename := sigReportJSONFilename(report.DateRangeEnd, report.SIGID)
	filePath := filepath.Join(g.outputDir, filename)

	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return "", fmt.Errorf("writing SIG report JSON: %w", err)
	}

	return filePath, nil
}

// GenerateDigestReport generates a weekly digest JSON report and returns the file path.
func (g *JSONGenerator) GenerateDigestReport(digest *analysis.DigestReport) (string, error) {
	if err := os.MkdirAll(g.outputDir, 0o755); err != nil {
		return "", fmt.Errorf("creating output directory: %w", err)
	}

	jd := &jsonDigestReport{
		DateRangeStart: digest.DateRangeStart,
		DateRangeEnd:   digest.DateRangeEnd,
		SIGCount:       len(digest.SIGReports),
		CrossSIGThemes: digest.CrossSIGThemes,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	for _, sr := range digest.SIGReports {
		jd.SIGReports = append(jd.SIGReports, toJSONSIGReport(sr))
	}

	data, err := json.MarshalIndent(jd, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling digest report to JSON: %w", err)
	}

	filename := digestJSONFilename(digest.DateRangeEnd)
	filePath := filepath.Join(g.outputDir, filename)

	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return "", fmt.Errorf("writing digest report JSON: %w", err)
	}

	return filePath, nil
}

// toJSONSIGReport converts an analysis.SIGReport to its JSON-serializable form.
func toJSONSIGReport(report *analysis.SIGReport) *jsonSIGReport {
	jr := &jsonSIGReport{
		SIGID:          report.SIGID,
		SIGName:        report.SIGName,
		Category:       report.Category,
		DateRangeStart: report.DateRangeStart,
		DateRangeEnd:   report.DateRangeEnd,
		SourcesUsed:    report.SourcesUsed,
		SourcesMissing: report.SourcesMissing,
		NotesLink:      report.NotesLink,
		RecordingLink:  report.RecordingLink,
		SlackChannel:   report.SlackChannel,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
	}

	if report.RelevanceReport != nil {
		jr.Relevance = &jsonRelevance{
			Report:      report.RelevanceReport.Report,
			HighItems:   report.RelevanceReport.HighItems,
			MediumItems: report.RelevanceReport.MediumItems,
			LowItems:    report.RelevanceReport.LowItems,
			Model:       report.RelevanceReport.Model,
			TokensUsed:  report.RelevanceReport.TokensUsed,
		}
	}

	return jr
}

// sigReportJSONFilename generates a filename like "2026-02-19-collector-report.json".
func sigReportJSONFilename(dateEnd, sigID string) string {
	date := dateEnd
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	slug := strings.ToLower(strings.ReplaceAll(sigID, " ", "-"))
	return fmt.Sprintf("%s-%s-report.json", date, slug)
}

// digestJSONFilename generates a filename like "2026-02-19-weekly-digest.json".
func digestJSONFilename(dateEnd string) string {
	date := dateEnd
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	return fmt.Sprintf("%s-weekly-digest.json", date)
}
