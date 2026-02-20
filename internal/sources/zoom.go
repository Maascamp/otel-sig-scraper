package sources

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/gordyrad/otel-sig-tracker/internal/browser"
	"github.com/gordyrad/otel-sig-tracker/internal/store"
)

const (
	// minRecordingDuration is the minimum recording duration (in minutes) to
	// attempt transcript extraction. Shorter recordings are typically
	// empty/canceled meetings without transcripts.
	minRecordingDuration = 2

	// zoomPageLoadDelay is the time to wait after navigating to a Zoom share
	// page for the Vue app to initialize.
	zoomPageLoadDelay = 5 * time.Second

	// zoomBaseURL is the base URL for Zoom VTT transcript downloads.
	zoomBaseURL = "https://zoom.us"
)

// ZoomFetcher extracts transcripts from Zoom recording share pages.
type ZoomFetcher struct {
	store       *store.Store
	pool        *browser.Pool
	httpClient  *http.Client
	delayBetween time.Duration
}

// NewZoomFetcher creates a new ZoomFetcher.
func NewZoomFetcher(s *store.Store) *ZoomFetcher {
	return &ZoomFetcher{
		store: s,
		pool:  browser.NewPool(true), // headless
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		delayBetween: 2 * time.Second, // rate limiting between requests
	}
}

// SetDelay sets the delay between consecutive Zoom page requests for rate limiting.
func (f *ZoomFetcher) SetDelay(d time.Duration) {
	f.delayBetween = d
}

// FetchTranscript loads the Zoom share page, extracts the VTT transcript URL
// from the Vue store state, downloads and parses the VTT, and stores the
// transcript in SQLite.
func (f *ZoomFetcher) FetchTranscript(ctx context.Context, recording *Recording) error {
	if recording.ZoomURL == "" {
		return fmt.Errorf("recording has no Zoom URL")
	}

	// Skip very short recordings.
	if recording.DurationMinutes > 0 && recording.DurationMinutes < minRecordingDuration {
		log.Printf("zoom: skipping short recording (%d min) for %s",
			recording.DurationMinutes, recording.SIGID)
		return nil
	}

	fetchStart := time.Now()

	// Create a browser context with extended timeout for page load.
	f.pool.SetTimeout(90 * time.Second)
	browserCtx, cancel := f.pool.NewContext(ctx)
	defer cancel()

	// Extract transcript URL from the Zoom share page's Vue store.
	transcriptURL, hasTranscript, err := f.extractTranscriptURL(browserCtx, recording.ZoomURL)
	if err != nil {
		f.logFetch(recording, "error", fmt.Sprintf("extracting transcript URL: %v", err), time.Since(fetchStart))
		return fmt.Errorf("extracting transcript URL: %w", err)
	}

	if !hasTranscript || transcriptURL == "" {
		log.Printf("zoom: no transcript available for %s (%s)",
			recording.SIGID, recording.ZoomURL)
		f.logFetch(recording, "skipped", "no transcript available", time.Since(fetchStart))
		return nil
	}

	// Build full VTT URL.
	fullVTTURL := transcriptURL
	if !strings.HasPrefix(transcriptURL, "http") {
		fullVTTURL = zoomBaseURL + transcriptURL
	}

	// Download the VTT file.
	vttContent, err := f.downloadVTT(ctx, fullVTTURL)
	if err != nil {
		f.logFetch(recording, "error", fmt.Sprintf("downloading VTT: %v", err), time.Since(fetchStart))
		return fmt.Errorf("downloading VTT: %w", err)
	}

	// Parse VTT to plain text with speaker names.
	transcript := parseVTT(vttContent)
	if transcript == "" {
		log.Printf("zoom: empty transcript after parsing VTT for %s", recording.SIGID)
		f.logFetch(recording, "skipped", "empty transcript after VTT parsing", time.Since(fetchStart))
		return nil
	}

	// Store in SQLite.
	hash := sha256Hash(transcript)
	vt := &store.VideoTranscript{
		SIGID:            recording.SIGID,
		ZoomURL:          recording.ZoomURL,
		RecordingDate:    recording.StartTime,
		DurationMinutes:  recording.DurationMinutes,
		Transcript:       transcript,
		TranscriptSource: "zoom_vtt",
		ContentHash:      hash,
	}

	if err := f.store.UpsertVideoTranscript(vt); err != nil {
		f.logFetch(recording, "error", fmt.Sprintf("storing transcript: %v", err), time.Since(fetchStart))
		return fmt.Errorf("storing transcript: %w", err)
	}

	f.logFetch(recording, "success", "", time.Since(fetchStart))
	log.Printf("zoom: stored transcript for %s (%d chars)",
		recording.SIGID, len(transcript))

	// Rate limiting delay.
	if f.delayBetween > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(f.delayBetween):
		}
	}

	return nil
}

// extractTranscriptURL navigates to the Zoom share page and extracts the
// transcript URL from the Vue store state.
func (f *ZoomFetcher) extractTranscriptURL(ctx context.Context, shareURL string) (string, bool, error) {
	var hasTranscript bool
	var transcriptURL string

	// JavaScript to extract data from the Vue store.
	extractJS := `
		(function() {
			try {
				var app = document.querySelector('#app');
				if (!app || !app.__vue__ || !app.__vue__.$store) {
					return JSON.stringify({error: "Vue store not found"});
				}
				var state = app.__vue__.$store.state;
				return JSON.stringify({
					hasTranscript: !!state.hasTranscript,
					transcriptUrl: state.transcriptUrl || "",
					playCheckId: state.playCheckId || "",
					topic: state.topic || "",
					duration: state.duration || 0
				});
			} catch(e) {
				return JSON.stringify({error: e.message});
			}
		})()
	`

	var result string
	err := chromedp.Run(ctx,
		chromedp.Navigate(shareURL),
		chromedp.Sleep(zoomPageLoadDelay),
		chromedp.Evaluate(extractJS, &result),
	)
	if err != nil {
		return "", false, fmt.Errorf("running browser actions: %w", err)
	}

	// Parse the JSON result manually to avoid encoding/json import for this simple case.
	// The result is a JSON object with known fields.
	if strings.Contains(result, `"error"`) {
		return "", false, fmt.Errorf("Vue store extraction failed: %s", result)
	}

	hasTranscript = strings.Contains(result, `"hasTranscript":true`)
	transcriptURL = extractJSONString(result, "transcriptUrl")

	return transcriptURL, hasTranscript, nil
}

// extractJSONString extracts a string value from a simple JSON object by key.
func extractJSONString(json, key string) string {
	search := fmt.Sprintf(`"%s":"`, key)
	idx := strings.Index(json, search)
	if idx == -1 {
		return ""
	}
	start := idx + len(search)
	end := strings.Index(json[start:], `"`)
	if end == -1 {
		return ""
	}
	return json[start : start+end]
}

// downloadVTT fetches the VTT transcript file from the given URL.
func (f *ZoomFetcher) downloadVTT(ctx context.Context, vttURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, vttURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating VTT request: %w", err)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading VTT: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("VTT download returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading VTT body: %w", err)
	}

	return string(body), nil
}

// vttTimestampRegex matches WebVTT timestamp lines like "00:03:59.730 --> 00:04:01.619".
var vttTimestampRegex = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}\.\d{3}\s+-->\s+\d{2}:\d{2}:\d{2}\.\d{3}$`)

// vttCueNumberRegex matches WebVTT cue number lines (plain integers).
var vttCueNumberRegex = regexp.MustCompile(`^\d+$`)

// parseVTT converts WebVTT content to plain text with speaker names.
// Input format:
//
//	WEBVTT
//	1
//	00:03:59.730 --> 00:04:01.619
//	Pablo Baeyens: Should we get started?
//
// Output:
//
//	Pablo Baeyens: Should we get started?
func parseVTT(content string) string {
	lines := strings.Split(content, "\n")
	var textLines []string
	lastSpeaker := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines, the WEBVTT header, cue numbers, and timestamps.
		if trimmed == "" {
			continue
		}
		if trimmed == "WEBVTT" {
			continue
		}
		if vttCueNumberRegex.MatchString(trimmed) {
			continue
		}
		if vttTimestampRegex.MatchString(trimmed) {
			continue
		}

		// Skip NOTE and STYLE blocks.
		if strings.HasPrefix(trimmed, "NOTE") || strings.HasPrefix(trimmed, "STYLE") {
			continue
		}

		// This is a text line (possibly with speaker name prefix).
		// Deduplicate consecutive lines from the same speaker with same text.
		speaker := ""
		text := trimmed
		if colonIdx := strings.Index(trimmed, ": "); colonIdx > 0 && colonIdx < 50 {
			speaker = trimmed[:colonIdx]
			text = trimmed[colonIdx+2:]
		}

		// Skip exact duplicate of previous line.
		if len(textLines) > 0 {
			prev := textLines[len(textLines)-1]
			if speaker != "" && lastSpeaker == speaker {
				// Same speaker — check if text is a substring continuation.
				prevText := prev
				if ci := strings.Index(prev, ": "); ci > 0 {
					prevText = prev[ci+2:]
				}
				if strings.HasPrefix(text, prevText) || text == prevText {
					// Replace the previous line with the longer version.
					textLines[len(textLines)-1] = trimmed
					continue
				}
			}
			if trimmed == prev {
				continue
			}
		}

		lastSpeaker = speaker
		textLines = append(textLines, trimmed)
	}

	return strings.Join(textLines, "\n")
}

// logFetch records a fetch operation in the store for a recording.
func (f *ZoomFetcher) logFetch(rec *Recording, status, errMsg string, duration time.Duration) {
	_ = f.store.LogFetch(&store.FetchLog{
		SourceType:   "video_transcript",
		SIGID:        rec.SIGID,
		URL:          rec.ZoomURL,
		Status:       status,
		ErrorMessage: errMsg,
		DurationMS:   duration.Milliseconds(),
	})
}
