package store

import (
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNewStore(t *testing.T) {
	s := newTestStore(t)
	if s == nil {
		t.Fatal("store should not be nil")
	}
	if s.DB() == nil {
		t.Fatal("db should not be nil")
	}
}

func TestMigrations(t *testing.T) {
	s := newTestStore(t)

	// Verify all tables exist
	tables := []string{"sigs", "meeting_notes", "video_transcripts", "slack_messages", "analysis_cache", "reports", "fetch_log", "schema_version"}
	for _, table := range tables {
		var name string
		err := s.DB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %q should exist: %v", table, err)
		}
	}
}

func TestUpsertAndGetSIG(t *testing.T) {
	s := newTestStore(t)

	sig := &SIG{
		ID:               "collector",
		Name:             "Collector",
		Category:         "implementation",
		MeetingTime:      "Wednesday at 09:00 PT",
		NotesDocID:       "1r2JC5MB7ab",
		SlackChannelID:   "C01N6P7KR6W",
		SlackChannelName: "#otel-collector",
	}

	if err := s.UpsertSIG(sig); err != nil {
		t.Fatalf("UpsertSIG failed: %v", err)
	}

	got, err := s.GetSIG("collector")
	if err != nil {
		t.Fatalf("GetSIG failed: %v", err)
	}

	if got.Name != "Collector" {
		t.Errorf("Name = %q, want %q", got.Name, "Collector")
	}
	if got.Category != "implementation" {
		t.Errorf("Category = %q, want %q", got.Category, "implementation")
	}
	if got.SlackChannelID != "C01N6P7KR6W" {
		t.Errorf("SlackChannelID = %q, want %q", got.SlackChannelID, "C01N6P7KR6W")
	}
}

func TestUpsertSIG_Update(t *testing.T) {
	s := newTestStore(t)

	sig := &SIG{ID: "collector", Name: "Collector", Category: "implementation"}
	if err := s.UpsertSIG(sig); err != nil {
		t.Fatalf("UpsertSIG failed: %v", err)
	}

	sig.MeetingTime = "Wednesday at 09:00 PT"
	if err := s.UpsertSIG(sig); err != nil {
		t.Fatalf("UpsertSIG update failed: %v", err)
	}

	got, err := s.GetSIG("collector")
	if err != nil {
		t.Fatalf("GetSIG failed: %v", err)
	}
	if got.MeetingTime != "Wednesday at 09:00 PT" {
		t.Errorf("MeetingTime = %q, want %q", got.MeetingTime, "Wednesday at 09:00 PT")
	}
}

func TestListSIGs(t *testing.T) {
	s := newTestStore(t)

	sigs := []*SIG{
		{ID: "collector", Name: "Collector", Category: "implementation"},
		{ID: "specification", Name: "Specification", Category: "specification"},
		{ID: "semconv", Name: "Semantic Conventions", Category: "cross-cutting"},
	}
	for _, sig := range sigs {
		if err := s.UpsertSIG(sig); err != nil {
			t.Fatalf("UpsertSIG failed: %v", err)
		}
	}

	// List all
	all, err := s.ListSIGs(nil)
	if err != nil {
		t.Fatalf("ListSIGs failed: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListSIGs returned %d, want 3", len(all))
	}

	// List filtered
	filtered, err := s.ListSIGs([]string{"collector", "semconv"})
	if err != nil {
		t.Fatalf("ListSIGs filtered failed: %v", err)
	}
	if len(filtered) != 2 {
		t.Errorf("ListSIGs filtered returned %d, want 2", len(filtered))
	}
}

func TestMeetingNotes(t *testing.T) {
	s := newTestStore(t)

	// Create SIG first
	if err := s.UpsertSIG(&SIG{ID: "collector", Name: "Collector", Category: "implementation"}); err != nil {
		t.Fatalf("UpsertSIG failed: %v", err)
	}

	note := &MeetingNote{
		SIGID:       "collector",
		DocID:       "doc123",
		MeetingDate: time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC),
		RawText:     "Meeting notes content here",
		ContentHash: "abc123",
	}

	if err := s.UpsertMeetingNote(note); err != nil {
		t.Fatalf("UpsertMeetingNote failed: %v", err)
	}

	notes, err := s.GetMeetingNotes("collector",
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetMeetingNotes failed: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("GetMeetingNotes returned %d, want 1", len(notes))
	}
	if notes[0].RawText != "Meeting notes content here" {
		t.Errorf("RawText = %q, want %q", notes[0].RawText, "Meeting notes content here")
	}

	// Test date range filtering
	notes, err = s.GetMeetingNotes("collector",
		time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetMeetingNotes failed: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("GetMeetingNotes returned %d for out-of-range query, want 0", len(notes))
	}
}

func TestVideoTranscripts(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertSIG(&SIG{ID: "collector", Name: "Collector", Category: "implementation"}); err != nil {
		t.Fatalf("UpsertSIG failed: %v", err)
	}

	vt := &VideoTranscript{
		SIGID:            "collector",
		ZoomURL:          "https://zoom.us/rec/share/abc123",
		RecordingDate:    time.Date(2026, 2, 18, 9, 0, 0, 0, time.UTC),
		DurationMinutes:  54,
		Transcript:       "Full transcript text",
		TranscriptSource: "zoom_vtt",
		ContentHash:      "hash123",
	}

	if err := s.UpsertVideoTranscript(vt); err != nil {
		t.Fatalf("UpsertVideoTranscript failed: %v", err)
	}

	transcripts, err := s.GetVideoTranscripts("collector",
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetVideoTranscripts failed: %v", err)
	}
	if len(transcripts) != 1 {
		t.Fatalf("GetVideoTranscripts returned %d, want 1", len(transcripts))
	}
	if transcripts[0].DurationMinutes != 54 {
		t.Errorf("DurationMinutes = %d, want 54", transcripts[0].DurationMinutes)
	}
}

func TestSlackMessages(t *testing.T) {
	s := newTestStore(t)

	if err := s.UpsertSIG(&SIG{ID: "collector", Name: "Collector", Category: "implementation"}); err != nil {
		t.Fatalf("UpsertSIG failed: %v", err)
	}

	msg := &SlackMessage{
		SIGID:       "collector",
		ChannelID:   "C01N6P7KR6W",
		MessageTS:   "1739890000.000100",
		UserID:      "U01ABC123",
		UserName:    "pablo",
		Text:        "Hello from Slack",
		MessageDate: time.Date(2026, 2, 18, 15, 0, 0, 0, time.UTC),
	}

	if err := s.UpsertSlackMessage(msg); err != nil {
		t.Fatalf("UpsertSlackMessage failed: %v", err)
	}

	msgs, err := s.GetSlackMessages("collector",
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetSlackMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("GetSlackMessages returned %d, want 1", len(msgs))
	}
	if msgs[0].UserName != "pablo" {
		t.Errorf("UserName = %q, want %q", msgs[0].UserName, "pablo")
	}
}

func TestAnalysisCache(t *testing.T) {
	s := newTestStore(t)

	ac := &AnalysisCache{
		CacheKey:       "test-key-123",
		SIGID:          "collector",
		SourceType:     "notes",
		DateRangeStart: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		DateRangeEnd:   time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC),
		PromptHash:     "prompt-hash-abc",
		Result:         "LLM analysis result text",
		Model:          "claude-sonnet-4-20250514",
		TokensUsed:     1500,
	}

	if err := s.PutAnalysisCache(ac); err != nil {
		t.Fatalf("PutAnalysisCache failed: %v", err)
	}

	got, err := s.GetAnalysisCache("test-key-123")
	if err != nil {
		t.Fatalf("GetAnalysisCache failed: %v", err)
	}
	if got.Result != "LLM analysis result text" {
		t.Errorf("Result = %q, want %q", got.Result, "LLM analysis result text")
	}
	if got.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q, want %q", got.Model, "claude-sonnet-4-20250514")
	}

	// Test cache miss
	_, err = s.GetAnalysisCache("nonexistent-key")
	if err == nil {
		t.Error("GetAnalysisCache should return error for nonexistent key")
	}
}

func TestLogFetch(t *testing.T) {
	s := newTestStore(t)

	fl := &FetchLog{
		SourceType:   "googledocs",
		SIGID:        "collector",
		URL:          "https://docs.google.com/document/d/abc/export?format=txt",
		Status:       "success",
		DurationMS:   1234,
	}

	if err := s.LogFetch(fl); err != nil {
		t.Fatalf("LogFetch failed: %v", err)
	}

	// Verify it was written
	var count int
	err := s.DB().QueryRow("SELECT COUNT(*) FROM fetch_log WHERE source_type = 'googledocs'").Scan(&count)
	if err != nil {
		t.Fatalf("counting fetch_log: %v", err)
	}
	if count != 1 {
		t.Errorf("fetch_log count = %d, want 1", count)
	}
}

func TestInsertReport(t *testing.T) {
	s := newTestStore(t)

	r := &Report{
		ReportType:     "sig",
		SIGID:          "collector",
		DateRangeStart: time.Date(2026, 2, 11, 0, 0, 0, 0, time.UTC),
		DateRangeEnd:   time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC),
		FilePath:       "reports/2026-02-18-collector-sig.md",
		ContentHash:    "report-hash-xyz",
	}

	if err := s.InsertReport(r); err != nil {
		t.Fatalf("InsertReport failed: %v", err)
	}

	var count int
	err := s.DB().QueryRow("SELECT COUNT(*) FROM reports").Scan(&count)
	if err != nil {
		t.Fatalf("counting reports: %v", err)
	}
	if count != 1 {
		t.Errorf("reports count = %d, want 1", count)
	}
}
