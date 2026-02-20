package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/gordyrad/otel-sig-tracker/internal/browser"
)

const (
	slackWorkspaceURL = "https://cloud-native.slack.com"
	// slackLoginTimeout is the maximum time to wait for the user to complete
	// interactive authentication.
	slackLoginTimeout = 5 * time.Minute
)

// SlackCredentials holds authentication data for Slack API access.
type SlackCredentials struct {
	Token    string `json:"token"`
	Cookie   string `json:"cookie"`
	TeamID   string `json:"team_id"`
	UserID   string `json:"user_id"`
	TeamName string `json:"team_name"`
	UserName string `json:"user_name"`
	SavedAt  string `json:"saved_at"`
}

// SlackLogin launches a visible Chromium window to cloud-native.slack.com,
// waits for the user to authenticate interactively, extracts the xoxc- token
// and d cookie, validates them, and saves credentials to the given file.
func SlackLogin(ctx context.Context, credsFile string) error {
	log.Println("slack-login: launching browser for interactive authentication...")
	log.Println("slack-login: please log in to cloud-native.slack.com in the browser window")

	// Use visible (non-headless) browser for interactive login.
	pool := browser.NewPool(false)
	pool.SetTimeout(slackLoginTimeout)
	defer pool.Cleanup()

	browserCtx, cancel := pool.NewContext(ctx)
	defer cancel()

	// Navigate to the Slack workspace.
	if err := chromedp.Run(browserCtx,
		chromedp.Navigate(slackWorkspaceURL),
	); err != nil {
		return fmt.Errorf("navigating to Slack: %w", err)
	}

	log.Println("slack-login: waiting for authentication to complete...")

	// Wait until the user is logged in by polling for the presence of
	// boot_data or api_token in the page.
	var token string
	err := chromedp.Run(browserCtx,
		// Wait for the main Slack client to load (indicated by boot_data being available).
		chromedp.WaitVisible(`[data-qa="channel_sidebar"]`, chromedp.ByQuery),
		// Extract the API token from the page.
		chromedp.Evaluate(`
			(function() {
				// Try boot_data first (most common).
				if (window.boot_data && window.boot_data.api_token) {
					return window.boot_data.api_token;
				}
				// Try localStorage.
				var localToken = localStorage.getItem('localConfig_v2');
				if (localToken) {
					try {
						var parsed = JSON.parse(localToken);
						if (parsed && parsed.teams) {
							var teams = Object.values(parsed.teams);
							for (var i = 0; i < teams.length; i++) {
								if (teams[i].token) return teams[i].token;
							}
						}
					} catch(e) {}
				}
				// Try other known locations.
				if (window.TS && window.TS.boot_data && window.TS.boot_data.api_token) {
					return window.TS.boot_data.api_token;
				}
				return "";
			})()
		`, &token),
	)
	if err != nil {
		return fmt.Errorf("waiting for login / extracting token: %w", err)
	}

	if token == "" || !strings.HasPrefix(token, "xoxc-") {
		return fmt.Errorf("failed to extract xoxc- token (got: %q)", token)
	}

	log.Println("slack-login: extracted xoxc- token")

	// Extract the d cookie from the browser.
	var cookies string
	err = chromedp.Run(browserCtx,
		chromedp.Evaluate(`document.cookie`, &cookies),
	)
	if err != nil {
		return fmt.Errorf("extracting cookies: %w", err)
	}

	dCookie := extractDCookie(cookies)
	if dCookie == "" {
		// Try getting cookies via the CDP network domain.
		err = chromedp.Run(browserCtx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				cc, err := network.GetCookies().Do(ctx)
				if err != nil {
					return err
				}
				for _, c := range cc {
					if c.Name == "d" {
						dCookie = c.Value
						break
					}
				}
				return nil
			}),
		)
		if err != nil {
			log.Printf("slack-login: warning: CDP cookie extraction failed: %v", err)
		}
	}

	if dCookie == "" {
		return fmt.Errorf("failed to extract d cookie from browser")
	}

	log.Println("slack-login: extracted d cookie")

	// Build credentials.
	creds := &SlackCredentials{
		Token:   token,
		Cookie:  dCookie,
		SavedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Validate via auth.test.
	if err := ValidateSlackCredentials(creds); err != nil {
		return fmt.Errorf("credential validation failed: %w", err)
	}

	// Save credentials to file.
	if err := saveSlackCredentials(credsFile, creds); err != nil {
		return fmt.Errorf("saving credentials: %w", err)
	}

	log.Printf("slack-login: credentials saved to %s", credsFile)
	log.Printf("slack-login: team_id=%s user_id=%s", creds.TeamID, creds.UserID)

	return nil
}

// LoadSlackCredentials reads Slack credentials from a JSON file.
// Returns nil and no error if the file does not exist.
func LoadSlackCredentials(credsFile string) (*SlackCredentials, error) {
	data, err := os.ReadFile(credsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading credentials file: %w", err)
	}

	var creds SlackCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parsing credentials JSON: %w", err)
	}

	return &creds, nil
}

// ValidateSlackCredentials calls auth.test to verify the credentials are valid.
// On success, it populates the TeamID and UserID fields.
func ValidateSlackCredentials(creds *SlackCredentials) error {
	if creds.Token == "" {
		return fmt.Errorf("token is empty")
	}

	client := &http.Client{Timeout: 15 * time.Second}

	req, err := http.NewRequest(http.MethodPost, slackAPIBase+"/auth.test", nil)
	if err != nil {
		return fmt.Errorf("creating auth.test request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+creds.Token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if creds.Cookie != "" {
		req.Header.Set("Cookie", "d="+creds.Cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("calling auth.test: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading auth.test response: %w", err)
	}

	var result struct {
		OK     bool   `json:"ok"`
		Error  string `json:"error,omitempty"`
		TeamID string `json:"team_id,omitempty"`
		UserID string `json:"user_id,omitempty"`
		Team   string `json:"team,omitempty"`
		User   string `json:"user,omitempty"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parsing auth.test response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("auth.test failed: %s", result.Error)
	}

	creds.TeamID = result.TeamID
	creds.UserID = result.UserID
	creds.TeamName = result.Team
	creds.UserName = result.User

	log.Printf("slack-login: authenticated as %s on team %s", result.User, result.Team)

	return nil
}

// saveSlackCredentials writes credentials to a JSON file, creating the
// directory structure if needed.
func saveSlackCredentials(credsFile string, creds *SlackCredentials) error {
	dir := filepath.Dir(credsFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating credentials directory: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling credentials: %w", err)
	}

	if err := os.WriteFile(credsFile, data, 0600); err != nil {
		return fmt.Errorf("writing credentials file: %w", err)
	}

	return nil
}

// extractDCookie extracts the value of the "d" cookie from a cookie string.
func extractDCookie(cookies string) string {
	for _, part := range strings.Split(cookies, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "d=") {
			return strings.TrimPrefix(part, "d=")
		}
	}
	return ""
}
