package store

import "fmt"

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS sigs (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		category TEXT NOT NULL,
		meeting_time TEXT,
		notes_doc_id TEXT,
		slack_channel_id TEXT,
		slack_channel_name TEXT,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	`CREATE TABLE IF NOT EXISTS meeting_notes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		sig_id TEXT NOT NULL REFERENCES sigs(id),
		doc_id TEXT NOT NULL,
		meeting_date DATE NOT NULL,
		raw_text TEXT NOT NULL,
		content_hash TEXT NOT NULL,
		fetched_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(sig_id, meeting_date)
	)`,

	`CREATE TABLE IF NOT EXISTS video_transcripts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		sig_id TEXT NOT NULL REFERENCES sigs(id),
		zoom_url TEXT NOT NULL,
		recording_date DATETIME NOT NULL,
		duration_minutes INTEGER,
		transcript TEXT,
		transcript_source TEXT,
		content_hash TEXT,
		fetched_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(zoom_url)
	)`,

	`CREATE TABLE IF NOT EXISTS slack_messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		sig_id TEXT NOT NULL REFERENCES sigs(id),
		channel_id TEXT NOT NULL,
		message_ts TEXT NOT NULL,
		thread_ts TEXT,
		user_id TEXT,
		user_name TEXT,
		text TEXT NOT NULL,
		message_date DATETIME NOT NULL,
		fetched_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(channel_id, message_ts)
	)`,

	`CREATE TABLE IF NOT EXISTS analysis_cache (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		cache_key TEXT NOT NULL UNIQUE,
		sig_id TEXT NOT NULL,
		source_type TEXT NOT NULL,
		date_range_start DATE NOT NULL,
		date_range_end DATE NOT NULL,
		prompt_hash TEXT NOT NULL,
		result TEXT NOT NULL,
		model TEXT NOT NULL,
		tokens_used INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	`CREATE TABLE IF NOT EXISTS reports (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		report_type TEXT NOT NULL,
		sig_id TEXT,
		date_range_start DATE NOT NULL,
		date_range_end DATE NOT NULL,
		file_path TEXT NOT NULL,
		content_hash TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	`CREATE TABLE IF NOT EXISTS fetch_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source_type TEXT NOT NULL,
		sig_id TEXT,
		url TEXT,
		status TEXT NOT NULL,
		error_message TEXT,
		duration_ms INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`,

	`CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY
	)`,
}

func (s *Store) migrate() error {
	// Create schema_version table if it doesn't exist
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY)`); err != nil {
		return fmt.Errorf("creating schema_version table: %w", err)
	}

	var currentVersion int
	err := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&currentVersion)
	if err != nil {
		return fmt.Errorf("getting schema version: %w", err)
	}

	for i := currentVersion; i < len(migrations); i++ {
		if _, err := s.db.Exec(migrations[i]); err != nil {
			return fmt.Errorf("running migration %d: %w", i+1, err)
		}
		if _, err := s.db.Exec("INSERT INTO schema_version (version) VALUES (?)", i+1); err != nil {
			return fmt.Errorf("updating schema version to %d: %w", i+1, err)
		}
	}

	return nil
}
