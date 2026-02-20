package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gordyrad/otel-sig-tracker/internal/store"
	"golang.org/x/time/rate"
)

const (
	slackAPIBase = "https://slack.com/api"
	// slackPageSize is the number of messages to fetch per page.
	slackPageSize = 200
)

// SlackFetcher fetches messages from Slack channels using xoxc- token + d cookie.
type SlackFetcher struct {
	store       *store.Store
	token       string
	cookie      string
	rateLimiter *rate.Limiter
	httpClient  *http.Client
}

// NewSlackFetcher creates a new SlackFetcher with the given credentials.
// Rate limited to approximately 50 requests per minute (Slack Tier 3).
func NewSlackFetcher(s *store.Store, token, cookie string) *SlackFetcher {
	return &SlackFetcher{
		store:       s,
		token:       token,
		cookie:      cookie,
		rateLimiter: rate.NewLimiter(rate.Every(1200*time.Millisecond), 1), // ~50 req/min
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// slackResponse is the generic Slack API response envelope.
type slackResponse struct {
	OK               bool           `json:"ok"`
	Error            string         `json:"error,omitempty"`
	Messages         []slackMessage `json:"messages,omitempty"`
	HasMore          bool           `json:"has_more,omitempty"`
	ResponseMetadata struct {
		NextCursor string `json:"next_cursor,omitempty"`
	} `json:"response_metadata,omitempty"`
}

// slackMessage represents a message from the Slack API.
type slackMessage struct {
	Type       string `json:"type"`
	Text       string `json:"text"`
	User       string `json:"user"`
	TS         string `json:"ts"`
	ThreadTS   string `json:"thread_ts,omitempty"`
	ReplyCount int    `json:"reply_count,omitempty"`
	Username   string `json:"username,omitempty"`
	BotID      string `json:"bot_id,omitempty"`
}

// FetchMessages fetches all messages (and threads) from the SIG's Slack channel
// within the given time range and stores them in SQLite.
func (f *SlackFetcher) FetchMessages(ctx context.Context, sig *store.SIG, start, end time.Time) error {
	if sig.SlackChannelID == "" {
		return fmt.Errorf("SIG %q has no Slack channel ID", sig.ID)
	}

	fetchStart := time.Now()
	channelID := sig.SlackChannelID

	// Convert time range to Slack timestamps (Unix epoch with microseconds).
	oldest := fmt.Sprintf("%d.000000", start.Unix())
	latest := fmt.Sprintf("%d.000000", end.Unix())

	// Fetch all messages in the channel within the time range.
	var allMessages []slackMessage
	cursor := ""
	page := 0

	for {
		page++
		if err := f.rateLimiter.Wait(ctx); err != nil {
			return fmt.Errorf("rate limiter: %w", err)
		}

		msgs, nextCursor, err := f.fetchHistoryPage(ctx, channelID, oldest, latest, cursor)
		if err != nil {
			f.logSlackFetch(sig.ID, channelID, "error", err.Error(), time.Since(fetchStart))
			return fmt.Errorf("fetching history page %d: %w", page, err)
		}

		allMessages = append(allMessages, msgs...)
		log.Printf("slack: %s page %d — got %d messages", sig.ID, page, len(msgs))

		if nextCursor == "" {
			break
		}
		cursor = nextCursor
	}

	// Fetch threads for messages that have replies.
	threadsToFetch := 0
	for _, msg := range allMessages {
		if msg.ReplyCount > 0 && msg.ThreadTS == "" {
			threadsToFetch++
		}
	}

	log.Printf("slack: %s — %d messages, %d threads to fetch",
		sig.ID, len(allMessages), threadsToFetch)

	// Store top-level messages and fetch threads.
	stored := 0
	for _, msg := range allMessages {
		// Store the message.
		if err := f.storeMessage(sig, channelID, &msg); err != nil {
			log.Printf("slack: warning: failed to store message %s: %v", msg.TS, err)
			continue
		}
		stored++

		// Fetch thread replies if this is a parent message with replies.
		if msg.ReplyCount > 0 && msg.ThreadTS == "" {
			if err := f.fetchAndStoreThread(ctx, sig, channelID, msg.TS); err != nil {
				log.Printf("slack: warning: failed to fetch thread %s: %v", msg.TS, err)
				// Continue processing other messages.
			}
		}
	}

	f.logSlackFetch(sig.ID, channelID, "success", "", time.Since(fetchStart))
	log.Printf("slack: %s — stored %d messages", sig.ID, stored)

	return nil
}

// fetchHistoryPage fetches a single page of channel history.
func (f *SlackFetcher) fetchHistoryPage(ctx context.Context, channelID, oldest, latest, cursor string) ([]slackMessage, string, error) {
	params := url.Values{
		"channel": {channelID},
		"oldest":  {oldest},
		"latest":  {latest},
		"limit":   {strconv.Itoa(slackPageSize)},
	}
	if cursor != "" {
		params.Set("cursor", cursor)
	}

	var resp slackResponse
	if err := f.slackAPICall(ctx, "conversations.history", params, &resp); err != nil {
		return nil, "", err
	}

	if !resp.OK {
		return nil, "", fmt.Errorf("Slack API error: %s", resp.Error)
	}

	nextCursor := ""
	if resp.HasMore {
		nextCursor = resp.ResponseMetadata.NextCursor
	}

	return resp.Messages, nextCursor, nil
}

// fetchAndStoreThread fetches all replies in a thread and stores them.
func (f *SlackFetcher) fetchAndStoreThread(ctx context.Context, sig *store.SIG, channelID, threadTS string) error {
	if err := f.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter: %w", err)
	}

	params := url.Values{
		"channel": {channelID},
		"ts":      {threadTS},
		"limit":   {strconv.Itoa(slackPageSize)},
	}

	var resp slackResponse
	if err := f.slackAPICall(ctx, "conversations.replies", params, &resp); err != nil {
		return err
	}

	if !resp.OK {
		return fmt.Errorf("Slack API error: %s", resp.Error)
	}

	stored := 0
	for _, msg := range resp.Messages {
		// Skip the parent message (already stored).
		if msg.TS == threadTS && msg.ThreadTS == "" {
			continue
		}

		msg.ThreadTS = threadTS
		if err := f.storeMessage(sig, channelID, &msg); err != nil {
			log.Printf("slack: warning: failed to store thread reply %s: %v", msg.TS, err)
			continue
		}
		stored++
	}

	return nil
}

// storeMessage converts a Slack API message to a store.SlackMessage and upserts it.
func (f *SlackFetcher) storeMessage(sig *store.SIG, channelID string, msg *slackMessage) error {
	// Parse message timestamp to time.Time.
	msgTime, err := parseSlackTS(msg.TS)
	if err != nil {
		return fmt.Errorf("parsing message timestamp: %w", err)
	}

	userName := msg.Username
	if userName == "" {
		userName = msg.User
	}

	sm := &store.SlackMessage{
		SIGID:       sig.ID,
		ChannelID:   channelID,
		MessageTS:   msg.TS,
		ThreadTS:    msg.ThreadTS,
		UserID:      msg.User,
		UserName:    userName,
		Text:        msg.Text,
		MessageDate: msgTime,
	}

	return f.store.UpsertSlackMessage(sm)
}

// slackAPICall makes an authenticated Slack API request.
func (f *SlackFetcher) slackAPICall(ctx context.Context, method string, params url.Values, result interface{}) error {
	apiURL := fmt.Sprintf("%s/%s?%s", slackAPIBase, method, params.Encode())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+f.token)
	if f.cookie != "" {
		req.Header.Set("Cookie", "d="+f.cookie)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("API call %s: %w", method, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API call %s returned HTTP %d", method, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}

	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("parsing response JSON: %w", err)
	}

	return nil
}

// parseSlackTS converts a Slack timestamp (e.g., "1706123456.789012") to time.Time.
func parseSlackTS(ts string) (time.Time, error) {
	parts := strings.SplitN(ts, ".", 2)
	if len(parts) == 0 || parts[0] == "" {
		return time.Time{}, fmt.Errorf("invalid Slack timestamp: %q", ts)
	}

	secs, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing Slack timestamp seconds: %w", err)
	}

	var usecs int64
	if len(parts) > 1 && parts[1] != "" {
		usecs, _ = strconv.ParseInt(parts[1], 10, 64)
	}

	return time.Unix(secs, usecs*1000), nil
}

// logSlackFetch records a Slack fetch operation in the store.
func (f *SlackFetcher) logSlackFetch(sigID, channelID, status, errMsg string, duration time.Duration) {
	_ = f.store.LogFetch(&store.FetchLog{
		SourceType:   "slack",
		SIGID:        sigID,
		URL:          fmt.Sprintf("slack://channel/%s", channelID),
		Status:       status,
		ErrorMessage: errMsg,
		DurationMS:   duration.Milliseconds(),
	})
}
