package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// SIG represents a parsed SIG entry from the registry.
type SIG struct {
	ID               string
	Name             string
	Category         string
	MeetingTime      string
	NotesDocID       string
	SlackChannelID   string
	SlackChannelName string
	UpdatedAt        time.Time
}

// MeetingNote represents a parsed meeting note entry.
type MeetingNote struct {
	ID          int64
	SIGID       string
	DocID       string
	MeetingDate time.Time
	RawText     string
	ContentHash string
	FetchedAt   time.Time
}

// VideoTranscript represents a video transcript entry.
type VideoTranscript struct {
	ID               int64
	SIGID            string
	ZoomURL          string
	RecordingDate    time.Time
	DurationMinutes  int
	Transcript       string
	TranscriptSource string
	ContentHash      string
	FetchedAt        time.Time
}

// SlackMessage represents a Slack message entry.
type SlackMessage struct {
	ID          int64
	SIGID       string
	ChannelID   string
	MessageTS   string
	ThreadTS    string
	UserID      string
	UserName    string
	Text        string
	MessageDate time.Time
	FetchedAt   time.Time
}

// AnalysisCache represents a cached LLM analysis result.
type AnalysisCache struct {
	ID             int64
	CacheKey       string
	SIGID          string
	SourceType     string
	DateRangeStart time.Time
	DateRangeEnd   time.Time
	PromptHash     string
	Result         string
	Model          string
	TokensUsed     int
	CreatedAt      time.Time
}

// Report represents a generated report record.
type Report struct {
	ID             int64
	ReportType     string
	SIGID          string
	DateRangeStart time.Time
	DateRangeEnd   time.Time
	FilePath       string
	ContentHash    string
	CreatedAt      time.Time
}

// FetchLog represents a fetch operation log entry.
type FetchLog struct {
	ID           int64
	SourceType   string
	SIGID        string
	URL          string
	Status       string
	ErrorMessage string
	DurationMS   int64
	CreatedAt    time.Time
}

// Store provides database operations for the application.
type Store struct {
	db *sql.DB
}

// New creates a new Store and runs migrations.
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting WAL mode: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying database connection for advanced queries.
func (s *Store) DB() *sql.DB {
	return s.db
}

// UpsertSIG inserts or updates a SIG entry.
func (s *Store) UpsertSIG(sig *SIG) error {
	_, err := s.db.Exec(`
		INSERT INTO sigs (id, name, category, meeting_time, notes_doc_id, slack_channel_id, slack_channel_name, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			category=excluded.category,
			meeting_time=excluded.meeting_time,
			notes_doc_id=excluded.notes_doc_id,
			slack_channel_id=excluded.slack_channel_id,
			slack_channel_name=excluded.slack_channel_name,
			updated_at=CURRENT_TIMESTAMP
	`, sig.ID, sig.Name, sig.Category, sig.MeetingTime, sig.NotesDocID, sig.SlackChannelID, sig.SlackChannelName)
	return err
}

// GetSIG retrieves a single SIG by ID.
func (s *Store) GetSIG(id string) (*SIG, error) {
	sig := &SIG{}
	err := s.db.QueryRow(`
		SELECT id, name, category, meeting_time, notes_doc_id, slack_channel_id, slack_channel_name, updated_at
		FROM sigs WHERE id = ?`, id).Scan(
		&sig.ID, &sig.Name, &sig.Category, &sig.MeetingTime,
		&sig.NotesDocID, &sig.SlackChannelID, &sig.SlackChannelName, &sig.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return sig, nil
}

// ListSIGs retrieves all SIGs, optionally filtered by IDs.
func (s *Store) ListSIGs(filterIDs []string) ([]*SIG, error) {
	var rows *sql.Rows
	var err error

	if len(filterIDs) > 0 {
		query := "SELECT id, name, category, meeting_time, notes_doc_id, slack_channel_id, slack_channel_name, updated_at FROM sigs WHERE id IN (?" + repeatParam(len(filterIDs)-1) + ") ORDER BY category, name"
		args := make([]interface{}, len(filterIDs))
		for i, id := range filterIDs {
			args[i] = id
		}
		rows, err = s.db.Query(query, args...)
	} else {
		rows, err = s.db.Query("SELECT id, name, category, meeting_time, notes_doc_id, slack_channel_id, slack_channel_name, updated_at FROM sigs ORDER BY category, name")
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sigs []*SIG
	for rows.Next() {
		sig := &SIG{}
		if err := rows.Scan(&sig.ID, &sig.Name, &sig.Category, &sig.MeetingTime,
			&sig.NotesDocID, &sig.SlackChannelID, &sig.SlackChannelName, &sig.UpdatedAt); err != nil {
			return nil, err
		}
		sigs = append(sigs, sig)
	}
	return sigs, rows.Err()
}

// UpsertMeetingNote inserts or updates a meeting note.
func (s *Store) UpsertMeetingNote(note *MeetingNote) error {
	_, err := s.db.Exec(`
		INSERT INTO meeting_notes (sig_id, doc_id, meeting_date, raw_text, content_hash, fetched_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(sig_id, meeting_date) DO UPDATE SET
			raw_text=excluded.raw_text,
			content_hash=excluded.content_hash,
			fetched_at=CURRENT_TIMESTAMP
	`, note.SIGID, note.DocID, note.MeetingDate.Format("2006-01-02"), note.RawText, note.ContentHash)
	return err
}

// GetMeetingNotes retrieves meeting notes for a SIG within a date range.
func (s *Store) GetMeetingNotes(sigID string, start, end time.Time) ([]*MeetingNote, error) {
	rows, err := s.db.Query(`
		SELECT id, sig_id, doc_id, meeting_date, raw_text, content_hash, fetched_at
		FROM meeting_notes
		WHERE sig_id = ? AND meeting_date >= ? AND meeting_date <= ?
		ORDER BY meeting_date DESC
	`, sigID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []*MeetingNote
	for rows.Next() {
		n := &MeetingNote{}
		if err := rows.Scan(&n.ID, &n.SIGID, &n.DocID, &n.MeetingDate, &n.RawText, &n.ContentHash, &n.FetchedAt); err != nil {
			return nil, err
		}
		notes = append(notes, n)
	}
	return notes, rows.Err()
}

// UpsertVideoTranscript inserts or updates a video transcript.
func (s *Store) UpsertVideoTranscript(vt *VideoTranscript) error {
	_, err := s.db.Exec(`
		INSERT INTO video_transcripts (sig_id, zoom_url, recording_date, duration_minutes, transcript, transcript_source, content_hash, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(zoom_url) DO UPDATE SET
			transcript=excluded.transcript,
			transcript_source=excluded.transcript_source,
			content_hash=excluded.content_hash,
			fetched_at=CURRENT_TIMESTAMP
	`, vt.SIGID, vt.ZoomURL, vt.RecordingDate, vt.DurationMinutes, vt.Transcript, vt.TranscriptSource, vt.ContentHash)
	return err
}

// GetVideoTranscripts retrieves transcripts for a SIG within a date range.
func (s *Store) GetVideoTranscripts(sigID string, start, end time.Time) ([]*VideoTranscript, error) {
	rows, err := s.db.Query(`
		SELECT id, sig_id, zoom_url, recording_date, duration_minutes, transcript, transcript_source, content_hash, fetched_at
		FROM video_transcripts
		WHERE sig_id = ? AND recording_date >= ? AND recording_date <= ?
		ORDER BY recording_date DESC
	`, sigID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var transcripts []*VideoTranscript
	for rows.Next() {
		vt := &VideoTranscript{}
		if err := rows.Scan(&vt.ID, &vt.SIGID, &vt.ZoomURL, &vt.RecordingDate,
			&vt.DurationMinutes, &vt.Transcript, &vt.TranscriptSource, &vt.ContentHash, &vt.FetchedAt); err != nil {
			return nil, err
		}
		transcripts = append(transcripts, vt)
	}
	return transcripts, rows.Err()
}

// UpsertSlackMessage inserts or updates a Slack message.
func (s *Store) UpsertSlackMessage(msg *SlackMessage) error {
	_, err := s.db.Exec(`
		INSERT INTO slack_messages (sig_id, channel_id, message_ts, thread_ts, user_id, user_name, text, message_date, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(channel_id, message_ts) DO UPDATE SET
			text=excluded.text,
			user_name=excluded.user_name,
			fetched_at=CURRENT_TIMESTAMP
	`, msg.SIGID, msg.ChannelID, msg.MessageTS, msg.ThreadTS, msg.UserID, msg.UserName, msg.Text, msg.MessageDate)
	return err
}

// GetSlackMessages retrieves Slack messages for a SIG within a date range.
func (s *Store) GetSlackMessages(sigID string, start, end time.Time) ([]*SlackMessage, error) {
	rows, err := s.db.Query(`
		SELECT id, sig_id, channel_id, message_ts, thread_ts, user_id, user_name, text, message_date, fetched_at
		FROM slack_messages
		WHERE sig_id = ? AND message_date >= ? AND message_date <= ?
		ORDER BY message_date DESC
	`, sigID, start.Format("2006-01-02"), end.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []*SlackMessage
	for rows.Next() {
		m := &SlackMessage{}
		if err := rows.Scan(&m.ID, &m.SIGID, &m.ChannelID, &m.MessageTS, &m.ThreadTS,
			&m.UserID, &m.UserName, &m.Text, &m.MessageDate, &m.FetchedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// GetAnalysisCache retrieves a cached analysis result.
func (s *Store) GetAnalysisCache(cacheKey string) (*AnalysisCache, error) {
	ac := &AnalysisCache{}
	err := s.db.QueryRow(`
		SELECT id, cache_key, sig_id, source_type, date_range_start, date_range_end, prompt_hash, result, model, tokens_used, created_at
		FROM analysis_cache WHERE cache_key = ?`, cacheKey).Scan(
		&ac.ID, &ac.CacheKey, &ac.SIGID, &ac.SourceType, &ac.DateRangeStart, &ac.DateRangeEnd,
		&ac.PromptHash, &ac.Result, &ac.Model, &ac.TokensUsed, &ac.CreatedAt)
	if err != nil {
		return nil, err
	}
	return ac, nil
}

// PutAnalysisCache stores an analysis result in the cache.
func (s *Store) PutAnalysisCache(ac *AnalysisCache) error {
	_, err := s.db.Exec(`
		INSERT INTO analysis_cache (cache_key, sig_id, source_type, date_range_start, date_range_end, prompt_hash, result, model, tokens_used, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(cache_key) DO UPDATE SET
			result=excluded.result,
			model=excluded.model,
			tokens_used=excluded.tokens_used,
			created_at=CURRENT_TIMESTAMP
	`, ac.CacheKey, ac.SIGID, ac.SourceType, ac.DateRangeStart.Format("2006-01-02"),
		ac.DateRangeEnd.Format("2006-01-02"), ac.PromptHash, ac.Result, ac.Model, ac.TokensUsed)
	return err
}

// InsertReport inserts a report record.
func (s *Store) InsertReport(r *Report) error {
	_, err := s.db.Exec(`
		INSERT INTO reports (report_type, sig_id, date_range_start, date_range_end, file_path, content_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, r.ReportType, r.SIGID, r.DateRangeStart.Format("2006-01-02"), r.DateRangeEnd.Format("2006-01-02"), r.FilePath, r.ContentHash)
	return err
}

// LogFetch inserts a fetch log entry.
func (s *Store) LogFetch(fl *FetchLog) error {
	_, err := s.db.Exec(`
		INSERT INTO fetch_log (source_type, sig_id, url, status, error_message, duration_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, fl.SourceType, fl.SIGID, fl.URL, fl.Status, fl.ErrorMessage, fl.DurationMS)
	return err
}

func repeatParam(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		s += ",?"
	}
	return s
}
