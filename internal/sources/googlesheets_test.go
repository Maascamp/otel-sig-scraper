package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseRecordingTime(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantY   int
		wantM   time.Month
		wantD   int
		wantH   int
		wantMin int
		wantErr bool
	}{
		{
			name:    "standard datetime",
			input:   "2026-02-18 8:59:46",
			wantY:   2026, wantM: time.February, wantD: 18,
			wantH: 8, wantMin: 59,
		},
		{
			name:    "24h format",
			input:   "2026-02-18 15:30:00",
			wantY:   2026, wantM: time.February, wantD: 18,
			wantH: 15, wantMin: 30,
		},
		{
			name:    "datetime without seconds",
			input:   "2026-02-18 10:00",
			wantY:   2026, wantM: time.February, wantD: 18,
			wantH: 10, wantMin: 0,
		},
		{
			name:    "date only",
			input:   "2026-02-18",
			wantY:   2026, wantM: time.February, wantD: 18,
			wantH: 0, wantMin: 0,
		},
		{
			name:    "US slash format with time",
			input:   "2/18/2026 15:04:05",
			wantY:   2026, wantM: time.February, wantD: 18,
			wantH: 15, wantMin: 4,
		},
		{
			name:    "US slash format single digit time",
			input:   "2/18/2026 3:04:05",
			wantY:   2026, wantM: time.February, wantD: 18,
			wantH: 3, wantMin: 4,
		},
		{
			name:    "unparseable",
			input:   "not-a-date",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRecordingTime(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseRecordingTime(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Year() != tt.wantY || got.Month() != tt.wantM || got.Day() != tt.wantD {
				t.Errorf("parseRecordingTime(%q) date = %v, want %04d-%02d-%02d",
					tt.input, got.Format("2006-01-02"), tt.wantY, tt.wantM, tt.wantD)
			}
			if got.Hour() != tt.wantH || got.Minute() != tt.wantMin {
				t.Errorf("parseRecordingTime(%q) time = %02d:%02d, want %02d:%02d",
					tt.input, got.Hour(), got.Minute(), tt.wantH, tt.wantMin)
			}
		})
	}
}

const sampleCSV = `Name,Start time,Duration (Minutes),URL
Collector SIG,2026-02-18 8:59:46,54,https://zoom.us/rec/share/abc123
.NET SIG,2026-02-17 10:00:00,45,https://zoom.us/rec/share/def456
Java SIG,2026-01-15 9:00:00,60,https://zoom.us/rec/share/old123
`

func TestFetchRecordings_ParsesCSV(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sampleCSV))
	}))
	defer srv.Close()

	fetcher := NewGoogleSheetsFetcher()
	fetcher.httpClient = &http.Client{Transport: &rewriteTransport{
		base:    http.DefaultTransport,
		rewrite: srv.URL + "/",
	}}

	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)

	recordings, err := fetcher.FetchRecordings(context.Background(), start, end, nil)
	if err != nil {
		t.Fatalf("FetchRecordings failed: %v", err)
	}

	// Only Feb 17 and Feb 18 recordings should be in range (Jan 15 excluded).
	if len(recordings) != 2 {
		t.Fatalf("FetchRecordings returned %d recordings, want 2", len(recordings))
	}
}

func TestFetchRecordings_DateRangeFiltering(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sampleCSV))
	}))
	defer srv.Close()

	fetcher := NewGoogleSheetsFetcher()
	fetcher.httpClient = &http.Client{Transport: &rewriteTransport{
		base:    http.DefaultTransport,
		rewrite: srv.URL + "/",
	}}

	// Only Feb 18 in range.
	start := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)

	recordings, err := fetcher.FetchRecordings(context.Background(), start, end, nil)
	if err != nil {
		t.Fatalf("FetchRecordings failed: %v", err)
	}

	if len(recordings) != 1 {
		t.Fatalf("expected 1 recording on Feb 18, got %d", len(recordings))
	}
	if recordings[0].SIGName != "Collector SIG" {
		t.Errorf("expected Collector SIG, got %q", recordings[0].SIGName)
	}
}

func TestFetchRecordings_SIGIDFiltering(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sampleCSV))
	}))
	defer srv.Close()

	fetcher := NewGoogleSheetsFetcher()
	fetcher.httpClient = &http.Client{Transport: &rewriteTransport{
		base:    http.DefaultTransport,
		rewrite: srv.URL + "/",
	}}

	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)

	// Only fetch "collector" SIG.
	recordings, err := fetcher.FetchRecordings(context.Background(), start, end, []string{"collector"})
	if err != nil {
		t.Fatalf("FetchRecordings failed: %v", err)
	}

	if len(recordings) != 1 {
		t.Fatalf("expected 1 recording for collector SIG, got %d", len(recordings))
	}
	if recordings[0].SIGID != "collector" {
		t.Errorf("expected SIGID 'collector', got %q", recordings[0].SIGID)
	}
}

func TestFetchRecordings_SIGNameMatching(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sampleCSV))
	}))
	defer srv.Close()

	fetcher := NewGoogleSheetsFetcher()
	fetcher.httpClient = &http.Client{Transport: &rewriteTransport{
		base:    http.DefaultTransport,
		rewrite: srv.URL + "/",
	}}

	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)

	recordings, err := fetcher.FetchRecordings(context.Background(), start, end, nil)
	if err != nil {
		t.Fatalf("FetchRecordings failed: %v", err)
	}

	// Verify SIG name to ID mapping.
	sigIDs := map[string]string{}
	for _, rec := range recordings {
		sigIDs[rec.SIGName] = rec.SIGID
	}

	if id, ok := sigIDs["Collector SIG"]; !ok || id != "collector" {
		t.Errorf("Collector SIG should map to 'collector', got %q", id)
	}
	if id, ok := sigIDs[".NET SIG"]; !ok || id != "net-sdk" {
		t.Errorf(".NET SIG should map to 'net-sdk', got %q", id)
	}
}

func TestFetchRecordings_RecordingFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sampleCSV))
	}))
	defer srv.Close()

	fetcher := NewGoogleSheetsFetcher()
	fetcher.httpClient = &http.Client{Transport: &rewriteTransport{
		base:    http.DefaultTransport,
		rewrite: srv.URL + "/",
	}}

	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)

	recordings, err := fetcher.FetchRecordings(context.Background(), start, end, nil)
	if err != nil {
		t.Fatalf("FetchRecordings failed: %v", err)
	}

	// Find the Collector SIG recording.
	var collector *Recording
	for _, rec := range recordings {
		if rec.SIGName == "Collector SIG" {
			collector = rec
			break
		}
	}
	if collector == nil {
		t.Fatal("Collector SIG recording not found")
	}

	if collector.DurationMinutes != 54 {
		t.Errorf("DurationMinutes = %d, want 54", collector.DurationMinutes)
	}
	if collector.ZoomURL != "https://zoom.us/rec/share/abc123" {
		t.Errorf("ZoomURL = %q, want %q", collector.ZoomURL, "https://zoom.us/rec/share/abc123")
	}
	if collector.StartTime.Year() != 2026 || collector.StartTime.Month() != time.February || collector.StartTime.Day() != 18 {
		t.Errorf("StartTime date = %v, want 2026-02-18", collector.StartTime.Format("2006-01-02"))
	}
}

func TestFetchRecordings_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	fetcher := NewGoogleSheetsFetcher()
	fetcher.httpClient = &http.Client{Transport: &rewriteTransport{
		base:    http.DefaultTransport,
		rewrite: srv.URL + "/",
	}}

	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)

	_, err := fetcher.FetchRecordings(context.Background(), start, end, nil)
	if err == nil {
		t.Fatal("expected error for HTTP 403")
	}
}

func TestFetchRecordings_EmptyRows(t *testing.T) {
	csvWithEmptyRows := `Name,Start time,Duration (Minutes),URL
Collector SIG,2026-02-18 8:59:46,54,https://zoom.us/rec/share/abc123
,,,
,2026-02-17 10:00:00,45,
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(csvWithEmptyRows))
	}))
	defer srv.Close()

	fetcher := NewGoogleSheetsFetcher()
	fetcher.httpClient = &http.Client{Transport: &rewriteTransport{
		base:    http.DefaultTransport,
		rewrite: srv.URL + "/",
	}}

	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)

	recordings, err := fetcher.FetchRecordings(context.Background(), start, end, nil)
	if err != nil {
		t.Fatalf("FetchRecordings failed: %v", err)
	}

	// Only the first valid row should be returned; the empty rows should be skipped.
	if len(recordings) != 1 {
		t.Errorf("expected 1 recording (skipping empty rows), got %d", len(recordings))
	}
}

func TestFetchRecordings_MissingColumns(t *testing.T) {
	badCSV := `Foo,Bar
a,b
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(badCSV))
	}))
	defer srv.Close()

	fetcher := NewGoogleSheetsFetcher()
	fetcher.httpClient = &http.Client{Transport: &rewriteTransport{
		base:    http.DefaultTransport,
		rewrite: srv.URL + "/",
	}}

	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)

	_, err := fetcher.FetchRecordings(context.Background(), start, end, nil)
	if err == nil {
		t.Fatal("expected error for CSV with missing columns")
	}
}

func TestFetchRecordings_HeaderOnly(t *testing.T) {
	headerOnlyCSV := `Name,Start time,Duration (Minutes),URL
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(headerOnlyCSV))
	}))
	defer srv.Close()

	fetcher := NewGoogleSheetsFetcher()
	fetcher.httpClient = &http.Client{Transport: &rewriteTransport{
		base:    http.DefaultTransport,
		rewrite: srv.URL + "/",
	}}

	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)

	_, err := fetcher.FetchRecordings(context.Background(), start, end, nil)
	if err == nil {
		t.Fatal("expected error for CSV with only headers (no data rows)")
	}
}

func TestFetchRecordings_MalformedDates(t *testing.T) {
	csvMalformedDate := `Name,Start time,Duration (Minutes),URL
Collector SIG,not-a-date,54,https://zoom.us/rec/share/abc123
.NET SIG,2026-02-17 10:00:00,45,https://zoom.us/rec/share/def456
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(csvMalformedDate))
	}))
	defer srv.Close()

	fetcher := NewGoogleSheetsFetcher()
	fetcher.httpClient = &http.Client{Transport: &rewriteTransport{
		base:    http.DefaultTransport,
		rewrite: srv.URL + "/",
	}}

	start := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)

	recordings, err := fetcher.FetchRecordings(context.Background(), start, end, nil)
	if err != nil {
		t.Fatalf("FetchRecordings failed: %v", err)
	}

	// Only the .NET SIG recording should be returned; the malformed date row should be skipped.
	if len(recordings) != 1 {
		t.Fatalf("expected 1 recording (skipping malformed date), got %d", len(recordings))
	}
	if recordings[0].SIGName != ".NET SIG" {
		t.Errorf("expected .NET SIG, got %q", recordings[0].SIGName)
	}
}
