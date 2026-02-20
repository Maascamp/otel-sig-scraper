package registry

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gordyrad/otel-sig-tracker/internal/store"
)

const (
	registryURL = "https://raw.githubusercontent.com/open-telemetry/community/main/README.md"
)

// Fetcher retrieves and parses the SIG registry from the OTel community README.
type Fetcher struct {
	httpClient *http.Client
}

// NewFetcher creates a new registry Fetcher.
func NewFetcher() *Fetcher {
	return &Fetcher{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// FetchAndParse downloads the community README and extracts SIG information.
func (f *Fetcher) FetchAndParse() ([]*store.SIG, error) {
	resp, err := f.httpClient.Get(registryURL)
	if err != nil {
		return nil, fmt.Errorf("fetching registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching registry: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading registry: %w", err)
	}

	return Parse(string(body))
}

// Parse extracts SIG information from the community README markdown content.
func Parse(content string) ([]*store.SIG, error) {
	var sigs []*store.SIG

	lines := strings.Split(content, "\n")
	currentCategory := ""

	categoryMap := map[string]string{
		"Specification SIGs":   "specification",
		"Implementation SIGs":  "implementation",
		"Cross-Cutting SIGs":   "cross-cutting",
		"Localization Teams":   "localization",
	}

	// Regex patterns for extracting data from table cells
	docIDRegex := regexp.MustCompile(`https://docs\.google\.com/document/d/([a-zA-Z0-9_-]+)`)
	slackRegex := regexp.MustCompile(`\[#([^\]]+)\]\(https://cloud-native\.slack\.com/archives/([A-Z0-9]+)\)`)

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Check for category headers
		if strings.HasPrefix(line, "### ") {
			header := strings.TrimPrefix(line, "### ")
			if cat, ok := categoryMap[header]; ok {
				currentCategory = cat
			}
			continue
		}

		// Skip non-table lines, header separator lines
		if !strings.HasPrefix(line, "|") || currentCategory == "" {
			continue
		}
		if strings.Contains(line, "---") {
			continue
		}
		// Skip table header row
		if strings.Contains(strings.ToLower(line), "| name") || strings.Contains(strings.ToLower(line), "|name") {
			continue
		}

		// Parse table row
		cells := splitTableRow(line)
		if len(cells) < 2 {
			continue
		}

		sig := &store.SIG{
			Category: currentCategory,
		}

		// Extract name (first cell)
		sig.Name = cleanMarkdown(cells[0])
		if sig.Name == "" {
			continue
		}
		sig.ID = NormalizeSIGID(sig.Name)

		// Extract meeting time if available
		for _, cell := range cells {
			if strings.Contains(cell, "day") || strings.Contains(cell, "PT") || strings.Contains(cell, "ET") {
				sig.MeetingTime = cleanMarkdown(cell)
				break
			}
		}

		// Extract Google Doc ID
		for _, cell := range cells {
			if matches := docIDRegex.FindStringSubmatch(cell); len(matches) > 1 {
				sig.NotesDocID = matches[1]
				break
			}
		}

		// Extract Slack channel
		for _, cell := range cells {
			if matches := slackRegex.FindStringSubmatch(cell); len(matches) > 2 {
				sig.SlackChannelName = "#" + matches[1]
				sig.SlackChannelID = matches[2]
				break
			}
		}

		sigs = append(sigs, sig)
	}

	return sigs, nil
}

// NormalizeSIGID creates a normalized slug from a SIG name.
func NormalizeSIGID(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, ":", "")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, "+", "plus")
	s = strings.ReplaceAll(s, "#", "")

	// Remove consecutive dashes
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	return s
}

// splitTableRow splits a markdown table row into cells.
func splitTableRow(line string) []string {
	line = strings.Trim(line, "|")
	parts := strings.Split(line, "|")
	var cells []string
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

// cleanMarkdown strips markdown formatting from text.
func cleanMarkdown(s string) string {
	s = strings.TrimSpace(s)
	// Remove markdown links: [text](url) → text
	linkRegex := regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	s = linkRegex.ReplaceAllString(s, "$1")
	// Remove bold/italic markers
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "*", "")
	return strings.TrimSpace(s)
}

// NameMappings provides known mappings from Google Sheet recording names to SIG IDs.
var NameMappings = map[string]string{
	"collector sig":          "collector",
	"specification sig":      "specification-general-plus-otel-maintainers-sync",
	".net sig":               "net-sdk",
	"go sig":                 "golang-sdk",
	"javascript sig":         "javascript-sdk",
	"java sig":               "java-sdk-plus-instrumentation",
	"python sig":             "python-sdk",
	"ruby sig":               "ruby-sdk",
	"rust sig":               "rust-sdk",
	"php sig":                "php-sdk",
	"c++ sig":                "cplusplus-sdk",
	"erlang/elixir sig":      "erlang-elixir-sdk",
	"swift sig":              "swift-sdk",
	"semantic convention sig": "semantic-conventions-general",
	"browser sig":            "browser",
	"android sig":            "android-sdk-plus-automatic-instrumentation",
	"ebpf instrumentation":   "ebpf-instrumentation",
	"arrow sig":              "arrow",
}

// MatchSheetNameToSIG attempts to match a Google Sheet recording name to a SIG ID.
func MatchSheetNameToSIG(sheetName string) string {
	normalized := strings.ToLower(strings.TrimSpace(sheetName))

	// Direct mapping lookup
	if id, ok := NameMappings[normalized]; ok {
		return id
	}

	// Strip common suffixes and try again
	for _, suffix := range []string{" sig", " sdk", " sig mtg"} {
		stripped := strings.TrimSuffix(normalized, suffix)
		if stripped != normalized {
			if id, ok := NameMappings[stripped+suffix]; ok {
				return id
			}
			// Try as direct ID
			return NormalizeSIGID(stripped)
		}
	}

	return NormalizeSIGID(normalized)
}
