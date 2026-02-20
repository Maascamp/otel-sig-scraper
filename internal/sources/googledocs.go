package sources

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gordyrad/otel-sig-tracker/internal/store"
)

const (
	googleDocsExportURL = "https://docs.google.com/document/d/%s/export?format=txt"
)

// GoogleDocsFetcher fetches and parses meeting notes from public Google Docs.
type GoogleDocsFetcher struct {
	store      *store.Store
	httpClient *http.Client
}

// NewGoogleDocsFetcher creates a new GoogleDocsFetcher.
func NewGoogleDocsFetcher(s *store.Store) *GoogleDocsFetcher {
	return &GoogleDocsFetcher{
		store: s,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// parsedMeeting holds a single parsed meeting extracted from a Google Doc.
type parsedMeeting struct {
	date    time.Time
	content string
}

// FetchMeetingNotes downloads the Google Doc for the given SIG, parses it by
// date headings, and stores each meeting that falls within [start, end] in SQLite.
func (f *GoogleDocsFetcher) FetchMeetingNotes(ctx context.Context, sig *store.SIG, start, end time.Time) error {
	if sig.NotesDocID == "" {
		return fmt.Errorf("SIG %q has no notes doc ID", sig.ID)
	}

	url := fmt.Sprintf(googleDocsExportURL, sig.NotesDocID)
	fetchStart := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		f.logFetch(sig.ID, url, "error", err.Error(), time.Since(fetchStart))
		return fmt.Errorf("fetching doc: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		f.logFetch(sig.ID, url, "error", errMsg, time.Since(fetchStart))
		return fmt.Errorf("fetching doc: %s", errMsg)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		f.logFetch(sig.ID, url, "error", err.Error(), time.Since(fetchStart))
		return fmt.Errorf("reading doc body: %w", err)
	}

	content := string(body)
	meetings := f.parseMeetingDates(content, start, end)

	stored := 0
	for _, m := range meetings {
		hash := sha256Hash(m.content)
		note := &store.MeetingNote{
			SIGID:       sig.ID,
			DocID:       sig.NotesDocID,
			MeetingDate: m.date,
			RawText:     m.content,
			ContentHash: hash,
		}
		if err := f.store.UpsertMeetingNote(note); err != nil {
			log.Printf("warning: failed to store meeting note for %s on %s: %v",
				sig.ID, m.date.Format("2006-01-02"), err)
			continue
		}
		stored++
	}

	status := "success"
	if stored == 0 && len(meetings) > 0 {
		status = "error"
	}
	f.logFetch(sig.ID, url, status, "", time.Since(fetchStart))

	log.Printf("googledocs: %s — found %d meetings in range, stored %d",
		sig.ID, len(meetings), stored)
	return nil
}

// parseMeetingDates splits the document content into individual meetings by
// finding date headings and filtering to those within [start, end].
// Most recent notes appear at the top of the document.
func (f *GoogleDocsFetcher) parseMeetingDates(content string, start, end time.Time) []parsedMeeting {
	lines := strings.Split(content, "\n")

	type datePosition struct {
		date    time.Time
		lineIdx int
	}

	var positions []datePosition

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// Try to parse the line as a date heading.
		if d, ok := tryParseDate(trimmed); ok {
			positions = append(positions, datePosition{date: d, lineIdx: i})
		}
	}

	if len(positions) == 0 {
		return nil
	}

	// Extract content between consecutive date headings.
	var meetings []parsedMeeting
	startDay := startOfDay(start)
	endDay := endOfDay(end)

	for i, pos := range positions {
		if pos.date.Before(startDay) || pos.date.After(endDay) {
			continue
		}

		// Determine the end boundary for this meeting's content.
		endLine := len(lines)
		if i+1 < len(positions) {
			endLine = positions[i+1].lineIdx
		}

		// Collect lines for this meeting (including the date heading).
		section := strings.Join(lines[pos.lineIdx:endLine], "\n")
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}

		meetings = append(meetings, parsedMeeting{
			date:    pos.date,
			content: section,
		})
	}

	return meetings
}

// datePatterns holds compiled regex patterns for date matching.
var datePatterns = []struct {
	re     *regexp.Regexp
	layout string
}{
	// "Feb 18, 2026" or "February 18, 2026"
	{re: regexp.MustCompile(`^(?:#*\s*)?(?:Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday)?[,\s]*?((?:Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|Jun(?:e)?|Jul(?:y)?|Aug(?:ust)?|Sep(?:tember)?|Oct(?:ober)?|Nov(?:ember)?|Dec(?:ember)?)\s+\d{1,2},?\s+\d{4})\s*$`)},
	// "2026-02-18"
	{re: regexp.MustCompile(`^(?:#*\s*)?(\d{4}-\d{2}-\d{2})\s*$`)},
	// "2/18/2026" or "02/18/2026"
	{re: regexp.MustCompile(`^(?:#*\s*)?(\d{1,2}/\d{1,2}/\d{4})\s*$`)},
}

// dateLayouts are the Go time layouts to try for parsing.
var dateLayouts = []string{
	"January 2, 2006",
	"January 2 2006",
	"Jan 2, 2006",
	"Jan 2 2006",
	"2006-01-02",
	"1/2/2006",
	"01/02/2006",
}

// tryParseDate attempts to parse a line as a date heading. Returns the date
// and true if successful, or zero time and false otherwise.
func tryParseDate(line string) (time.Time, bool) {
	// Strip leading markdown heading markers and whitespace.
	cleaned := strings.TrimLeft(line, "#")
	cleaned = strings.TrimSpace(cleaned)

	// Strip trailing punctuation that's common in headings.
	cleaned = strings.TrimRight(cleaned, ":")
	cleaned = strings.TrimSpace(cleaned)

	// Strip leading day-of-week names (e.g., "Wednesday, Feb 18, 2026").
	dayNames := []string{
		"Monday", "Tuesday", "Wednesday", "Thursday",
		"Friday", "Saturday", "Sunday",
	}
	for _, day := range dayNames {
		if strings.HasPrefix(cleaned, day) {
			cleaned = strings.TrimPrefix(cleaned, day)
			cleaned = strings.TrimLeft(cleaned, ", ")
			break
		}
	}

	// Try each layout.
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, cleaned); err == nil {
			return t, true
		}
	}

	// Try regex-based extraction for lines with surrounding text.
	for _, dp := range datePatterns {
		if matches := dp.re.FindStringSubmatch(line); len(matches) > 1 {
			dateStr := matches[1]
			for _, layout := range dateLayouts {
				if t, err := time.Parse(layout, dateStr); err == nil {
					return t, true
				}
			}
		}
	}

	return time.Time{}, false
}

// sha256Hash returns the hex-encoded SHA-256 hash of s.
func sha256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", h)
}

// startOfDay returns the start of the day (00:00:00) for the given time.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

// endOfDay returns the end of the day (23:59:59) for the given time.
func endOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 23, 59, 59, 0, t.Location())
}

// logFetch records a fetch operation in the store.
func (f *GoogleDocsFetcher) logFetch(sigID, url, status, errMsg string, duration time.Duration) {
	_ = f.store.LogFetch(&store.FetchLog{
		SourceType:   "meeting_notes",
		SIGID:        sigID,
		URL:          url,
		Status:       status,
		ErrorMessage: errMsg,
		DurationMS:   duration.Milliseconds(),
	})
}

// parseInt is a helper to parse an integer from a string, returning 0 on error.
func parseInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
