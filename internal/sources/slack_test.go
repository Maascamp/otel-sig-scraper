package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestParseSlackTS(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantSec int64
		wantErr bool
	}{
		{
			name:    "standard timestamp",
			input:   "1739890000.000100",
			wantSec: 1739890000,
		},
		{
			name:    "timestamp without microseconds",
			input:   "1739890000",
			wantSec: 1739890000,
		},
		{
			name:    "zero timestamp",
			input:   "0.000000",
			wantSec: 0,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "just a dot",
			input:   ".123456",
			wantErr: true,
		},
		{
			name:    "non-numeric",
			input:   "abc.def",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSlackTS(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseSlackTS(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Unix() != tt.wantSec {
				t.Errorf("parseSlackTS(%q) = %d seconds, want %d", tt.input, got.Unix(), tt.wantSec)
			}
		})
	}
}

func TestParseSlackTS_MicrosecondsPreserved(t *testing.T) {
	ts, err := parseSlackTS("1739890000.000100")
	if err != nil {
		t.Fatalf("parseSlackTS failed: %v", err)
	}

	// The microseconds (100) should be preserved in nanoseconds.
	// 100 microseconds * 1000 = 100,000 nanoseconds.
	nanos := ts.Nanosecond()
	if nanos != 100*1000 {
		t.Errorf("expected 100000 nanoseconds, got %d", nanos)
	}
}

func TestSlackFetcher_FetchMessages(t *testing.T) {
	// Compute Unix timestamps that correspond to Feb 18, 2026 in UTC.
	feb18 := time.Date(2026, 2, 18, 15, 0, 0, 0, time.UTC)
	ts1 := fmt.Sprintf("%d.000100", feb18.Unix())
	ts2 := fmt.Sprintf("%d.000400", feb18.Add(1*time.Hour).Unix())
	ts3 := fmt.Sprintf("%d.000200", feb18.Add(1*time.Minute).Unix())

	// Create a mock Slack API server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth headers.
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("missing or invalid Authorization header: %q", auth)
		}
		cookie := r.Header.Get("Cookie")
		if !strings.Contains(cookie, "d=") {
			t.Errorf("missing or invalid Cookie header: %q", cookie)
		}

		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/conversations.history"):
			resp := slackResponse{
				OK: true,
				Messages: []slackMessage{
					{
						Type:       "message",
						Text:       "Hello from test",
						User:       "U01ABC123",
						TS:         ts1,
						ReplyCount: 1,
					},
					{
						Type: "message",
						Text: "Another message",
						User: "U01DEF456",
						TS:   ts2,
					},
				},
				HasMore: false,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		case strings.HasSuffix(path, "/conversations.replies"):
			resp := slackResponse{
				OK: true,
				Messages: []slackMessage{
					{
						Type:     "message",
						Text:     "Hello from test",
						User:     "U01ABC123",
						TS:       ts1,
						ThreadTS: "",
					},
					{
						Type:     "message",
						Text:     "Thread reply here",
						User:     "U01GHI789",
						TS:       ts3,
						ThreadTS: ts1,
					},
				},
				HasMore: false,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	s := newTestStore(t)
	sig := insertTestSIG(t, s, "collector", "Collector", "", "C01N6P7KR6W")

	fetcher := &SlackFetcher{
		store:       s,
		token:       "xoxc-test-token",
		cookie:      "test-cookie",
		rateLimiter: rate.NewLimiter(rate.Inf, 1), // No rate limiting in tests.
		httpClient: &http.Client{Transport: &slackRewriteTransport{
			base:      http.DefaultTransport,
			targetURL: srv.URL,
		}},
	}

	start := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 19, 0, 0, 0, 0, time.UTC)

	err := fetcher.FetchMessages(context.Background(), sig, start, end)
	if err != nil {
		t.Fatalf("FetchMessages failed: %v", err)
	}

	// Verify messages were stored by counting rows directly.
	var count int
	err = s.DB().QueryRow("SELECT COUNT(*) FROM slack_messages WHERE sig_id = 'collector'").Scan(&count)
	if err != nil {
		t.Fatalf("counting slack_messages: %v", err)
	}

	// We should have the 2 top-level messages plus 1 thread reply (the parent in the
	// replies is skipped because it matches threadTS check).
	if count < 2 {
		t.Errorf("expected at least 2 stored messages, got %d", count)
	}
}

func TestSlackFetcher_FetchMessages_NoChannelID(t *testing.T) {
	s := newTestStore(t)
	sig := insertTestSIG(t, s, "collector", "Collector", "", "")

	fetcher := NewSlackFetcher(s, "token", "cookie")

	start := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 19, 0, 0, 0, 0, time.UTC)

	err := fetcher.FetchMessages(context.Background(), sig, start, end)
	if err == nil {
		t.Fatal("expected error for SIG with no Slack channel ID")
	}
	if !strings.Contains(err.Error(), "no Slack channel ID") {
		t.Errorf("error should mention 'no Slack channel ID', got: %v", err)
	}
}

func TestSlackFetcher_APIErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := slackResponse{
			OK:    false,
			Error: "channel_not_found",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	s := newTestStore(t)
	sig := insertTestSIG(t, s, "collector", "Collector", "", "C01INVALID")

	fetcher := &SlackFetcher{
		store:       s,
		token:       "xoxc-test-token",
		cookie:      "test-cookie",
		rateLimiter: rate.NewLimiter(rate.Inf, 1),
		httpClient: &http.Client{Transport: &slackRewriteTransport{
			base:      http.DefaultTransport,
			targetURL: srv.URL,
		}},
	}

	start := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 19, 0, 0, 0, 0, time.UTC)

	err := fetcher.FetchMessages(context.Background(), sig, start, end)
	if err == nil {
		t.Fatal("expected error for Slack API error response")
	}
	if !strings.Contains(err.Error(), "channel_not_found") {
		t.Errorf("error should contain 'channel_not_found', got: %v", err)
	}
}

func TestSlackFetcher_Pagination(t *testing.T) {
	// Compute Unix timestamps for Feb 18, 2026 in UTC.
	feb18 := time.Date(2026, 2, 18, 15, 0, 0, 0, time.UTC)
	tsPage1 := fmt.Sprintf("%d.000100", feb18.Unix())
	tsPage2 := fmt.Sprintf("%d.000200", feb18.Add(1*time.Hour).Unix())

	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/conversations.history") {
			// Reply with empty for any thread fetches.
			resp := slackResponse{OK: true}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}

		page++
		w.Header().Set("Content-Type", "application/json")

		if page == 1 {
			resp := slackResponse{
				OK: true,
				Messages: []slackMessage{
					{Type: "message", Text: "Page 1 message", User: "U01", TS: tsPage1},
				},
				HasMore: true,
			}
			resp.ResponseMetadata.NextCursor = "cursor_page2"
			json.NewEncoder(w).Encode(resp)
		} else {
			resp := slackResponse{
				OK: true,
				Messages: []slackMessage{
					{Type: "message", Text: "Page 2 message", User: "U02", TS: tsPage2},
				},
				HasMore: false,
			}
			json.NewEncoder(w).Encode(resp)
		}
	}))
	defer srv.Close()

	s := newTestStore(t)
	sig := insertTestSIG(t, s, "collector", "Collector", "", "C01TEST")

	fetcher := &SlackFetcher{
		store:       s,
		token:       "xoxc-test",
		cookie:      "test",
		rateLimiter: rate.NewLimiter(rate.Inf, 1),
		httpClient: &http.Client{Transport: &slackRewriteTransport{
			base:      http.DefaultTransport,
			targetURL: srv.URL,
		}},
	}

	start := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 19, 0, 0, 0, 0, time.UTC)

	err := fetcher.FetchMessages(context.Background(), sig, start, end)
	if err != nil {
		t.Fatalf("FetchMessages failed: %v", err)
	}

	if page < 2 {
		t.Errorf("expected at least 2 pages of history fetched, got %d", page)
	}

	// Verify messages were stored by counting rows directly.
	var count int
	err = s.DB().QueryRow("SELECT COUNT(*) FROM slack_messages WHERE sig_id = 'collector'").Scan(&count)
	if err != nil {
		t.Fatalf("counting slack_messages: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 messages from pagination, got %d", count)
	}
}

func TestSlackFetcher_RateLimiterCreated(t *testing.T) {
	s := newTestStore(t)
	fetcher := NewSlackFetcher(s, "token", "cookie")

	if fetcher.rateLimiter == nil {
		t.Error("rate limiter should be created")
	}

	// Verify rate is approximately 50 req/min (1 per 1.2 seconds).
	limit := fetcher.rateLimiter.Limit()
	// rate.Every(1200ms) = 1/1.2 ~= 0.833 events/sec
	expectedLimit := rate.Every(1200 * time.Millisecond)
	if limit != expectedLimit {
		t.Errorf("rate limiter limit = %v, want %v", limit, expectedLimit)
	}
}

func TestSlackFetcher_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := newTestStore(t)
	sig := insertTestSIG(t, s, "collector", "Collector", "", "C01TEST")

	fetcher := &SlackFetcher{
		store:       s,
		token:       "xoxc-test",
		cookie:      "test",
		rateLimiter: rate.NewLimiter(rate.Inf, 1),
		httpClient: &http.Client{Transport: &slackRewriteTransport{
			base:      http.DefaultTransport,
			targetURL: srv.URL,
		}},
	}

	start := time.Date(2026, 2, 18, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 19, 0, 0, 0, 0, time.UTC)

	err := fetcher.FetchMessages(context.Background(), sig, start, end)
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

// slackRewriteTransport rewrites Slack API requests to point to a test server,
// preserving the API method path (e.g., /conversations.history).
type slackRewriteTransport struct {
	base      http.RoundTripper
	targetURL string
}

func (t *slackRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())

	// Extract the Slack API method from the original URL path.
	// Original: https://slack.com/api/conversations.history?...
	// We want to keep the method path part.
	origPath := req.URL.Path
	parts := strings.Split(origPath, "/")
	method := parts[len(parts)-1]

	// Rewrite to test server.
	newURL := t.targetURL + "/" + method
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}

	parsed, err := req.URL.Parse(newURL)
	if err != nil {
		return nil, err
	}
	req.URL = parsed
	return t.base.RoundTrip(req)
}
