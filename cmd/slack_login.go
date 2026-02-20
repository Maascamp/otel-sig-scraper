package cmd

import (
	"fmt"
	"os"

	"github.com/gordyrad/otel-sig-tracker/internal/sources"
	"github.com/spf13/cobra"
)

var slackLoginCmd = &cobra.Command{
	Use:   "slack-login",
	Short: "Authenticate with CNCF Slack (interactive browser login)",
	Long: `Launches a visible Chromium browser window to authenticate with the CNCF Slack
workspace (cloud-native.slack.com). After you log in interactively, the tool
extracts the session tokens and stores them locally for subsequent API calls.

Supported login methods: Google OAuth, Apple, or email magic code.

The extracted tokens (xoxc- token + d cookie) are saved to the Slack credentials
file (default: ~/.config/otel-sig-scraper/slack-credentials.json). These tokens
typically remain valid for days to weeks. Run this command again if the session
expires.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		credsFile := cfg.Slack.CredentialsFile

		fmt.Fprintln(os.Stdout, "Launching browser for CNCF Slack login...")
		fmt.Fprintln(os.Stdout, "Please authenticate in the browser window that opens.")
		fmt.Fprintln(os.Stdout)

		if err := sources.SlackLogin(ctx, credsFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error: Slack login failed: %v\n", err)
			os.Exit(1)
		}

		// Load the newly saved credentials to display details.
		creds, err := sources.LoadSlackCredentials(credsFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: could not load saved credentials: %v\n", err)
			os.Exit(1)
		}
		if creds == nil {
			fmt.Fprintf(os.Stderr, "Error: credentials file not found after login: %s\n", credsFile)
			os.Exit(1)
		}

		fmt.Fprintln(os.Stdout, "Slack login successful!")
		fmt.Fprintf(os.Stdout, "  Team ID:     %s\n", creds.TeamID)
		fmt.Fprintf(os.Stdout, "  User ID:     %s\n", creds.UserID)
		fmt.Fprintf(os.Stdout, "  Credentials: %s\n", credsFile)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(slackLoginCmd)
}
