package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/gordyrad/otel-sig-tracker/internal/registry"
	"github.com/gordyrad/otel-sig-tracker/internal/store"
	"github.com/spf13/cobra"
)

var refreshFlag bool

var listSigsCmd = &cobra.Command{
	Use:   "list-sigs",
	Short: "List available OTel SIGs",
	Long: `Lists all available OpenTelemetry Special Interest Groups (SIGs) with their
category, meeting time, and Slack channel information.

By default, uses cached data from the local SQLite database if available.
If no cached data exists, or if --refresh is specified, fetches fresh data
from the OTel community GitHub repository.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Open the store to check for cached data.
		db, err := store.New(cfg.DBPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
			os.Exit(2)
		}
		defer db.Close()

		var sigs []*store.SIG

		if !refreshFlag {
			// Try to load from cache first.
			sigs, err = db.ListSIGs(nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not read cached SIGs: %v\n", err)
			}
		}

		// If no cached data or refresh requested, fetch from GitHub.
		if len(sigs) == 0 || refreshFlag {
			if cfg.Verbose {
				fmt.Fprintln(os.Stderr, "Fetching SIG registry from GitHub...")
			}

			fetcher := registry.NewFetcher()
			freshSIGs, fetchErr := fetcher.FetchAndParse()
			if fetchErr != nil {
				// If we have cached data, use it despite the fetch failure.
				if len(sigs) > 0 {
					fmt.Fprintf(os.Stderr, "Warning: could not refresh from GitHub: %v (using cached data)\n", fetchErr)
				} else {
					fmt.Fprintf(os.Stderr, "Error: could not fetch SIG registry: %v\n", fetchErr)
					os.Exit(2)
				}
			} else {
				// Store the fresh data.
				for _, sig := range freshSIGs {
					if upsertErr := db.UpsertSIG(sig); upsertErr != nil {
						fmt.Fprintf(os.Stderr, "Warning: could not cache SIG %q: %v\n", sig.ID, upsertErr)
					}
				}
				sigs = freshSIGs
			}
		}

		if len(sigs) == 0 {
			fmt.Fprintln(os.Stdout, "No SIGs found.")
			return nil
		}

		// Print formatted table.
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tCATEGORY\tMEETING TIME\tSLACK")
		for _, sig := range sigs {
			slack := sig.SlackChannelName
			if slack == "" {
				slack = "-"
			}
			meetingTime := sig.MeetingTime
			if meetingTime == "" {
				meetingTime = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", sig.Name, sig.Category, meetingTime, slack)
		}
		w.Flush()

		fmt.Fprintf(os.Stdout, "\n%d SIGs listed.\n", len(sigs))
		return nil
	},
}

func init() {
	listSigsCmd.Flags().BoolVar(&refreshFlag, "refresh", false, "Force re-fetch from GitHub (ignore cache)")
	rootCmd.AddCommand(listSigsCmd)
}
