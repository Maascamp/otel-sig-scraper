package sources

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gordyrad/otel-sig-tracker/internal/registry"
)

const (
	// recordingsSheetID is the Google Sheets ID for the OTel recordings spreadsheet.
	recordingsSheetID = "1SYKfjYhZdm2Wh2Cl6KVQalKg_m4NhTPZqq-8SzEVO6s"
	// googleSheetsExportURL is the URL template for exporting a Google Sheet as CSV.
	googleSheetsExportURL = "https://docs.google.com/spreadsheets/d/%s/export?format=csv"
)

// Recording represents a single meeting recording from the Google Sheet.
type Recording struct {
	SIGName         string
	SIGID           string
	StartTime       time.Time
	DurationMinutes int
	ZoomURL         string
}

// GoogleSheetsFetcher fetches the recording list from the public Google Sheet.
type GoogleSheetsFetcher struct {
	httpClient *http.Client
}

// NewGoogleSheetsFetcher creates a new GoogleSheetsFetcher.
func NewGoogleSheetsFetcher() *GoogleSheetsFetcher {
	return &GoogleSheetsFetcher{
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// FetchRecordings downloads the recording spreadsheet as CSV, parses it, and
// returns recordings filtered by the given date range and SIG IDs.
// If sigIDs is empty, all SIGs are included.
func (f *GoogleSheetsFetcher) FetchRecordings(ctx context.Context, start, end time.Time, sigIDs []string) ([]*Recording, error) {
	url := fmt.Sprintf(googleSheetsExportURL, recordingsSheetID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching sheet: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching sheet: HTTP %d", resp.StatusCode)
	}

	return f.parseCSV(resp.Body, start, end, sigIDs)
}

// parseCSV reads the CSV body and returns filtered recordings.
// Expected columns: Name, Start time, Duration, URL
func (f *GoogleSheetsFetcher) parseCSV(r io.Reader, start, end time.Time, sigIDs []string) ([]*Recording, error) {
	reader := csv.NewReader(r)
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	// Read all records.
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parsing CSV: %w", err)
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("CSV has no data rows (got %d rows)", len(records))
	}

	// Identify column indices from header row.
	header := records[0]
	colIdx := map[string]int{
		"name":     -1,
		"start":    -1,
		"duration": -1,
		"url":      -1,
	}
	for i, col := range header {
		lower := strings.ToLower(strings.TrimSpace(col))
		switch {
		case strings.Contains(lower, "name"):
			colIdx["name"] = i
		case strings.Contains(lower, "start"):
			colIdx["start"] = i
		case strings.Contains(lower, "duration"):
			colIdx["duration"] = i
		case strings.Contains(lower, "url") || strings.Contains(lower, "link"):
			colIdx["url"] = i
		}
	}

	// Validate required columns.
	for key, idx := range colIdx {
		if idx == -1 {
			return nil, fmt.Errorf("CSV missing required column: %s", key)
		}
	}

	// Build a set for quick SIG ID lookup.
	sigSet := make(map[string]bool, len(sigIDs))
	for _, id := range sigIDs {
		sigSet[id] = true
	}

	startDay := startOfDay(start)
	endDay := endOfDay(end)

	var recordings []*Recording
	for _, row := range records[1:] {
		if len(row) <= colIdx["url"] {
			continue
		}

		name := strings.TrimSpace(row[colIdx["name"]])
		startStr := strings.TrimSpace(row[colIdx["start"]])
		durationStr := strings.TrimSpace(row[colIdx["duration"]])
		zoomURL := strings.TrimSpace(row[colIdx["url"]])

		if name == "" || startStr == "" || zoomURL == "" {
			continue
		}

		// Parse start time. Format: "YYYY-MM-DD H:MM:SS"
		recTime, err := parseRecordingTime(startStr)
		if err != nil {
			log.Printf("googlesheets: skipping row with unparseable time %q: %v", startStr, err)
			continue
		}

		// Filter by date range.
		if recTime.Before(startDay) || recTime.After(endDay) {
			continue
		}

		// Match name to SIG ID.
		sigID := registry.MatchSheetNameToSIG(name)

		// Filter by SIG IDs if provided.
		if len(sigSet) > 0 && !sigSet[sigID] {
			continue
		}

		// Parse duration.
		duration := 0
		if durationStr != "" {
			duration, _ = strconv.Atoi(durationStr)
		}

		recordings = append(recordings, &Recording{
			SIGName:         name,
			SIGID:           sigID,
			StartTime:       recTime,
			DurationMinutes: duration,
			ZoomURL:         zoomURL,
		})
	}

	log.Printf("googlesheets: parsed %d recordings in date range from %d total rows",
		len(recordings), len(records)-1)

	return recordings, nil
}

// parseRecordingTime parses a recording timestamp. It tries multiple formats
// to handle variations in the spreadsheet data.
func parseRecordingTime(s string) (time.Time, error) {
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 3:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		"1/2/2006 15:04:05",
		"1/2/2006 3:04:05",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("could not parse time: %q", s)
}
