package cmd

import (
	"fmt"
	"os"

	"github.com/gordyrad/otel-sig-tracker/internal/pipeline"
	"github.com/spf13/cobra"
)

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Fetch data from sources without running analysis",
	Long: `Fetches meeting notes, video transcripts, and Slack discussions for the
configured SIGs and time window, storing everything in the local SQLite database.
Does not run LLM analysis or generate reports.

This is useful for populating the cache before running analysis, or for archiving
raw data from OTel SIG sources.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Create the pipeline.
		p, err := pipeline.New(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Fatal error: failed to create pipeline: %v\n", err)
			os.Exit(2)
		}
		defer p.Close()

		ctx := cmd.Context()

		if err := p.FetchOnly(ctx); err != nil {
			if pErr, ok := err.(*pipeline.PartialError); ok {
				fmt.Fprintf(os.Stderr, "Warning: partial failure — %d source(s) failed:\n", len(pErr.Errors))
				for _, e := range pErr.Errors {
					fmt.Fprintf(os.Stderr, "  - %v\n", e)
				}
				fmt.Fprintf(os.Stdout, "\nFetch completed with partial data stored in: %s\n", cfg.DBPath)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Fatal error: %v\n", err)
			os.Exit(2)
		}

		fmt.Fprintf(os.Stdout, "Fetch completed successfully. Data stored in: %s\n", cfg.DBPath)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(fetchCmd)
}
