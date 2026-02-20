package cmd

import (
	"fmt"
	"os"

	"github.com/gordyrad/otel-sig-tracker/internal/sources"
	"github.com/spf13/cobra"
)

var slackStatusCmd = &cobra.Command{
	Use:   "slack-status",
	Short: "Check Slack authentication status",
	Long: `Checks whether Slack credentials exist and are still valid by loading the
stored tokens and calling the Slack auth.test API endpoint.

Displays the authenticated user, team name, and token validity status.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		credsFile := cfg.Slack.CredentialsFile

		// Attempt to load credentials.
		creds, err := sources.LoadSlackCredentials(credsFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Not authenticated: %v\n", err)
			fmt.Fprintf(os.Stderr, "\nRun 'otel-sig-scraper slack-login' to authenticate.\n")
			os.Exit(1)
		}
		if creds == nil {
			fmt.Fprintf(os.Stderr, "Not authenticated: no credentials found at %s\n", credsFile)
			fmt.Fprintf(os.Stderr, "\nRun 'otel-sig-scraper slack-login' to authenticate.\n")
			os.Exit(1)
		}

		// Validate credentials with Slack API.
		if err := sources.ValidateSlackCredentials(creds); err != nil {
			fmt.Fprintln(os.Stdout, "Slack credentials found but invalid or expired.")
			fmt.Fprintf(os.Stderr, "  Error: %v\n", err)
			fmt.Fprintf(os.Stderr, "\nRun 'otel-sig-scraper slack-login' to re-authenticate.\n")
			os.Exit(1)
		}

		fmt.Fprintln(os.Stdout, "Slack authentication status: valid")
		fmt.Fprintf(os.Stdout, "  Team ID:          %s\n", creds.TeamID)
		fmt.Fprintf(os.Stdout, "  User ID:          %s\n", creds.UserID)
		fmt.Fprintf(os.Stdout, "  Credentials file: %s\n", credsFile)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(slackStatusCmd)
}
