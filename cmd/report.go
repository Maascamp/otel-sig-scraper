package cmd

import (
	"fmt"
	"os"

	"github.com/gordyrad/otel-sig-tracker/internal/pipeline"
	"github.com/spf13/cobra"
)

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Generate intelligence reports for OTel SIGs",
	Long: `Fetches data from all configured sources, runs LLM analysis, and generates
Markdown/JSON reports. Uses the pipeline to fetch meeting notes, video transcripts,
and Slack discussions, then produces Datadog-focused intelligence reports.

Exit codes:
  0 - Success
  1 - Partial failure (some sources failed, report generated from available data)
  2 - Fatal error (no data could be fetched, no report generated)
  3 - Configuration error`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Validate configuration.
		if err := cfg.Validate(); err != nil {
			fmt.Fprintf(os.Stderr, "Configuration error: %v\n", err)
			os.Exit(3)
		}

		// Create the pipeline.
		p, err := pipeline.New(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Fatal error: failed to create pipeline: %v\n", err)
			os.Exit(2)
		}
		defer p.Close()

		ctx := cmd.Context()

		// If offline mode, run analysis only on cached data.
		// Otherwise, run the full pipeline (fetch + analyze + report).
		var runErr error
		if cfg.Offline {
			runErr = p.AnalyzeOnly(ctx)
		} else {
			runErr = p.Run(ctx)
		}

		if runErr != nil {
			// Determine if this is a partial or fatal failure.
			if pErr, ok := runErr.(*pipeline.PartialError); ok {
				fmt.Fprintf(os.Stderr, "Warning: partial failure — %d source(s) failed:\n", len(pErr.Errors))
				for _, e := range pErr.Errors {
					fmt.Fprintf(os.Stderr, "  - %v\n", e)
				}
				fmt.Fprintf(os.Stdout, "\nReports generated with available data in: %s\n", cfg.OutputDir)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Fatal error: %v\n", runErr)
			os.Exit(2)
		}

		fmt.Fprintf(os.Stdout, "Reports generated successfully in: %s\n", cfg.OutputDir)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(reportCmd)
}
