package sources

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gordyrad/otel-sig-tracker/internal/store"
)

// newTestStore creates an in-memory SQLite store for testing and registers cleanup.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// insertTestSIG inserts a SIG into the test store and fails the test on error.
func insertTestSIG(t *testing.T, s *store.Store, id, name, notesDocID, slackChannelID string) *store.SIG {
	t.Helper()
	sig := &store.SIG{
		ID:             id,
		Name:           name,
		Category:       "implementation",
		NotesDocID:     notesDocID,
		SlackChannelID: slackChannelID,
	}
	if err := s.UpsertSIG(sig); err != nil {
		t.Fatalf("failed to insert test SIG: %v", err)
	}
	return sig
}

func TestTryParseDate(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantY   int
		wantM   time.Month
		wantD   int
		wantOK  bool
	}{
		{
			name:   "short month with comma",
			input:  "Feb 18, 2026",
			wantY:  2026, wantM: time.February, wantD: 18,
			wantOK: true,
		},
		{
			name:   "ISO date",
			input:  "2026-02-18",
			wantY:  2026, wantM: time.February, wantD: 18,
			wantOK: true,
		},
		{
			name:   "long month with comma",
			input:  "February 18, 2026",
			wantY:  2026, wantM: time.February, wantD: 18,
			wantOK: true,
		},
		{
			name:   "US slash format",
			input:  "2/18/2026",
			wantY:  2026, wantM: time.February, wantD: 18,
			wantOK: true,
		},
		{
			name:   "US slash format with leading zeros",
			input:  "02/18/2026",
			wantY:  2026, wantM: time.February, wantD: 18,
			wantOK: true,
		},
		{
			name:   "short month without comma",
			input:  "Feb 18 2026",
			wantY:  2026, wantM: time.February, wantD: 18,
			wantOK: true,
		},
		{
			name:   "long month without comma",
			input:  "February 18 2026",
			wantY:  2026, wantM: time.February, wantD: 18,
			wantOK: true,
		},
		{
			name:   "markdown heading with date",
			input:  "## Feb 18, 2026",
			wantY:  2026, wantM: time.February, wantD: 18,
			wantOK: true,
		},
		{
			name:   "with day-of-week prefix",
			input:  "Wednesday, Feb 18, 2026",
			wantY:  2026, wantM: time.February, wantD: 18,
			wantOK: true,
		},
		{
			name:   "trailing colon",
			input:  "Feb 18, 2026:",
			wantY:  2026, wantM: time.February, wantD: 18,
			wantOK: true,
		},
		{
			name:   "not a date - random text",
			input:  "This is not a date",
			wantOK: false,
		},
		{
			name:   "not a date - partial date",
			input:  "Feb 2026",
			wantOK: false,
		},
		{
			name:   "empty string",
			input:  "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tryParseDate(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("tryParseDate(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.Year() != tt.wantY || got.Month() != tt.wantM || got.Day() != tt.wantD {
				t.Errorf("tryParseDate(%q) = %v, want %04d-%02d-%02d",
					tt.input, got.Format("2006-01-02"), tt.wantY, tt.wantM, tt.wantD)
			}
		})
	}
}

func TestParseMeetingDates_WithSampleNotes(t *testing.T) {
	content, err := os.ReadFile("../../testdata/sample_meeting_notes.txt")
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}

	s := newTestStore(t)
	fetcher := NewGoogleDocsFetcher(s)

	// Date range covers all three meetings in the sample: Feb 4, 11, 18 of 2026.
	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)

	meetings := fetcher.parseMeetingDates(string(content), start, end)

	if len(meetings) != 3 {
		t.Fatalf("parseMeetingDates returned %d meetings, want 3", len(meetings))
	}

	// Meetings should include Feb 18, Feb 11, Feb 4 (in document order, top to bottom).
	expectedDates := []time.Time{
		time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 4, 0, 0, 0, 0, time.UTC),
	}
	for i, m := range meetings {
		if !m.date.Equal(expectedDates[i]) {
			t.Errorf("meeting[%d].date = %v, want %v", i, m.date, expectedDates[i])
		}
	}

	// Verify content extraction — the first meeting (Feb 18) should contain OTLP/HTTP.
	if meetings[0].content == "" {
		t.Error("first meeting content should not be empty")
	}
	if !containsSubstring(meetings[0].content, "OTLP/HTTP") {
		t.Error("first meeting should contain 'OTLP/HTTP'")
	}
	if !containsSubstring(meetings[0].content, "Pablo") {
		t.Error("first meeting should mention 'Pablo'")
	}
}

func TestParseMeetingDates_DateRangeFiltering(t *testing.T) {
	content, err := os.ReadFile("../../testdata/sample_meeting_notes.txt")
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}

	s := newTestStore(t)
	fetcher := NewGoogleDocsFetcher(s)

	// Only include Feb 11-18 — should exclude Feb 4.
	start := time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	meetings := fetcher.parseMeetingDates(string(content), start, end)

	if len(meetings) != 2 {
		t.Fatalf("parseMeetingDates returned %d meetings, want 2", len(meetings))
	}

	// Feb 4 meeting should be excluded.
	for _, m := range meetings {
		if m.date.Day() == 4 {
			t.Error("Feb 4 meeting should be excluded by date range filter")
		}
	}
}

func TestParseMeetingDates_NoMatchingDates(t *testing.T) {
	content, err := os.ReadFile("../../testdata/sample_meeting_notes.txt")
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}

	s := newTestStore(t)
	fetcher := NewGoogleDocsFetcher(s)

	// Date range in March — no meetings.
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)

	meetings := fetcher.parseMeetingDates(string(content), start, end)

	if len(meetings) != 0 {
		t.Errorf("parseMeetingDates returned %d meetings for out-of-range query, want 0", len(meetings))
	}
}

func TestParseMeetingDates_EmptyContent(t *testing.T) {
	s := newTestStore(t)
	fetcher := NewGoogleDocsFetcher(s)

	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)

	meetings := fetcher.parseMeetingDates("", start, end)
	if meetings != nil {
		t.Errorf("parseMeetingDates on empty content should return nil, got %d meetings", len(meetings))
	}
}

func TestParseMeetingDates_NoDates(t *testing.T) {
	s := newTestStore(t)
	fetcher := NewGoogleDocsFetcher(s)

	content := "This document has no date headings.\nJust some random notes.\nNothing to parse here."
	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)

	meetings := fetcher.parseMeetingDates(content, start, end)
	if meetings != nil {
		t.Errorf("parseMeetingDates on content without dates should return nil, got %d meetings", len(meetings))
	}
}

func TestSha256Hash(t *testing.T) {
	input := "test content"
	want := fmt.Sprintf("%x", sha256.Sum256([]byte(input)))
	got := sha256Hash(input)
	if got != want {
		t.Errorf("sha256Hash(%q) = %q, want %q", input, got, want)
	}
}

func TestSha256Hash_DifferentContent(t *testing.T) {
	hash1 := sha256Hash("content A")
	hash2 := sha256Hash("content B")
	if hash1 == hash2 {
		t.Error("different content should produce different hashes")
	}
}

func TestSha256Hash_SameContent(t *testing.T) {
	hash1 := sha256Hash("identical content")
	hash2 := sha256Hash("identical content")
	if hash1 != hash2 {
		t.Error("same content should produce same hash")
	}
}

func TestFetchMeetingNotes_Success(t *testing.T) {
	content, err := os.ReadFile("../../testdata/sample_meeting_notes.txt")
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}

	// Mock HTTP server that serves the meeting notes.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer srv.Close()

	s := newTestStore(t)
	sig := insertTestSIG(t, s, "collector", "Collector", "test-doc-id", "C01N6P7KR6W")

	fetcher := NewGoogleDocsFetcher(s)
	fetcher.httpClient = srv.Client()

	// Override the URL format to point to our test server.
	// We need to monkey-patch by using a custom transport or by modifying behavior.
	// Since the fetcher uses a format string with the doc ID, we can't easily override.
	// Instead, create a custom fetcher that redirects to the test server.
	transport := &rewriteTransport{
		base:    http.DefaultTransport,
		rewrite: srv.URL + "/",
	}
	fetcher.httpClient = &http.Client{Transport: transport}

	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)

	if err := fetcher.FetchMeetingNotes(context.Background(), sig, start, end); err != nil {
		t.Fatalf("FetchMeetingNotes failed: %v", err)
	}

	// Verify notes were stored.
	notes, err := s.GetMeetingNotes("collector", start, end)
	if err != nil {
		t.Fatalf("GetMeetingNotes failed: %v", err)
	}
	if len(notes) != 3 {
		t.Errorf("expected 3 meeting notes stored, got %d", len(notes))
	}
}

func TestFetchMeetingNotes_NoDocID(t *testing.T) {
	s := newTestStore(t)
	sig := insertTestSIG(t, s, "collector", "Collector", "", "C01N6P7KR6W")

	fetcher := NewGoogleDocsFetcher(s)
	err := fetcher.FetchMeetingNotes(context.Background(), sig, time.Now(), time.Now())
	if err == nil {
		t.Fatal("expected error for SIG with no notes doc ID")
	}
	if !containsSubstring(err.Error(), "no notes doc ID") {
		t.Errorf("error should mention 'no notes doc ID', got: %v", err)
	}
}

func TestFetchMeetingNotes_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := newTestStore(t)
	sig := insertTestSIG(t, s, "collector", "Collector", "test-doc-id", "C01N6P7KR6W")

	fetcher := NewGoogleDocsFetcher(s)
	fetcher.httpClient = &http.Client{Transport: &rewriteTransport{
		base:    http.DefaultTransport,
		rewrite: srv.URL + "/",
	}}

	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)

	err := fetcher.FetchMeetingNotes(context.Background(), sig, start, end)
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
	if !containsSubstring(err.Error(), "HTTP 500") {
		t.Errorf("error should mention 'HTTP 500', got: %v", err)
	}
}

func TestFetchMeetingNotes_ContentHashDedup(t *testing.T) {
	content, err := os.ReadFile("../../testdata/sample_meeting_notes.txt")
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer srv.Close()

	s := newTestStore(t)
	sig := insertTestSIG(t, s, "collector", "Collector", "test-doc-id", "C01N6P7KR6W")

	fetcher := NewGoogleDocsFetcher(s)
	fetcher.httpClient = &http.Client{Transport: &rewriteTransport{
		base:    http.DefaultTransport,
		rewrite: srv.URL + "/",
	}}

	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)

	// Fetch twice — should upsert, not duplicate.
	if err := fetcher.FetchMeetingNotes(context.Background(), sig, start, end); err != nil {
		t.Fatalf("first FetchMeetingNotes failed: %v", err)
	}
	if err := fetcher.FetchMeetingNotes(context.Background(), sig, start, end); err != nil {
		t.Fatalf("second FetchMeetingNotes failed: %v", err)
	}

	notes, err := s.GetMeetingNotes("collector", start, end)
	if err != nil {
		t.Fatalf("GetMeetingNotes failed: %v", err)
	}
	// Should still be 3 notes (upserted, not duplicated).
	if len(notes) != 3 {
		t.Errorf("expected 3 meeting notes after double fetch, got %d", len(notes))
	}
}

func TestStartOfDay(t *testing.T) {
	input := time.Date(2026, 2, 18, 15, 30, 45, 123, time.UTC)
	got := startOfDay(input)
	want := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("startOfDay(%v) = %v, want %v", input, got, want)
	}
}

func TestEndOfDay(t *testing.T) {
	input := time.Date(2026, 2, 18, 8, 0, 0, 0, time.UTC)
	got := endOfDay(input)
	want := time.Date(2026, 2, 18, 23, 59, 59, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("endOfDay(%v) = %v, want %v", input, got, want)
	}
}

func TestParseInt(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"42", 42},
		{"0", 0},
		{"", 0},
		{"abc", 0},
		{"-1", -1},
	}
	for _, tt := range tests {
		got := parseInt(tt.input)
		if got != tt.want {
			t.Errorf("parseInt(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

// rewriteTransport is an http.RoundTripper that rewrites all request URLs
// to point to a test server.
type rewriteTransport struct {
	base    http.RoundTripper
	rewrite string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = "http"
	req.URL.Host = ""
	req.URL.Path = "/"
	full := t.rewrite
	parsed, _ := req.URL.Parse(full)
	req.URL = parsed
	return t.base.RoundTrip(req)
}

// containsSubstring checks if s contains substr.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsIdx(s, substr))
}

func containsIdx(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
