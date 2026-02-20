package cmd

import (
	"fmt"
	"os"

	"github.com/gordyrad/otel-sig-tracker/internal/analysis"
	"github.com/spf13/cobra"
)

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Manage custom context injected into LLM prompts",
	Long: `Manage the custom context that is injected into the Datadog relevance scoring
prompt during LLM analysis. This allows you to customize the focus areas and
priorities without modifying the application code.

The custom context is only used during the final relevance scoring pass (not
during per-source summarization), keeping source summaries neutral.

Use subcommands to show, set, or clear the custom context.`,
}

var contextShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current custom context",
	Long:  `Displays the contents of the custom context file that is injected into LLM prompts.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		contextFile := cfg.ContextFile

		content, err := analysis.LoadCustomContext(contextFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading context: %v\n", err)
			os.Exit(1)
		}

		if content == "" {
			fmt.Fprintln(os.Stdout, "No custom context set.")
			fmt.Fprintf(os.Stdout, "  Context file: %s (not found)\n", contextFile)
			fmt.Fprintln(os.Stdout, "\nUse 'otel-sig-scraper context set' to configure custom context.")
			return nil
		}

		fmt.Fprintf(os.Stdout, "Custom context (%s):\n\n", contextFile)
		fmt.Fprintln(os.Stdout, content)
		return nil
	},
}

var (
	contextSetFile string
	contextSetText string
)

var contextSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Set custom context from a file or string",
	Long: `Sets the custom context that is injected into LLM prompts. You can provide
the context either as a file path (--file) or as an inline string (--text).

Examples:
  otel-sig-scraper context set --file context.md
  otel-sig-scraper context set --text "Focus on OTLP changes and sampling decisions"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		contextFile := cfg.ContextFile

		if contextSetFile == "" && contextSetText == "" {
			fmt.Fprintln(os.Stderr, "Error: either --file or --text must be specified")
			os.Exit(3)
		}
		if contextSetFile != "" && contextSetText != "" {
			fmt.Fprintln(os.Stderr, "Error: --file and --text are mutually exclusive")
			os.Exit(3)
		}

		var content string
		if contextSetFile != "" {
			data, err := os.ReadFile(contextSetFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading file %q: %v\n", contextSetFile, err)
				os.Exit(1)
			}
			content = string(data)
		} else {
			content = contextSetText
		}

		if err := analysis.SaveCustomContext(contextFile, content); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving context: %v\n", err)
			os.Exit(1)
		}

		fmt.Fprintf(os.Stdout, "Custom context saved to: %s\n", contextFile)
		fmt.Fprintf(os.Stdout, "  Size: %d bytes\n", len(content))
		return nil
	},
}

var contextClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Remove custom context",
	Long:  `Removes the custom context file, so no custom context will be injected into LLM prompts.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		contextFile := cfg.ContextFile

		if err := analysis.ClearCustomContext(contextFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error clearing context: %v\n", err)
			os.Exit(1)
		}

		fmt.Fprintf(os.Stdout, "Custom context cleared (removed: %s)\n", contextFile)
		return nil
	},
}

func init() {
	contextSetCmd.Flags().StringVar(&contextSetFile, "file", "", "Path to a file containing custom context")
	contextSetCmd.Flags().StringVar(&contextSetText, "text", "", "Custom context as an inline string")

	contextCmd.AddCommand(contextShowCmd)
	contextCmd.AddCommand(contextSetCmd)
	contextCmd.AddCommand(contextClearCmd)

	rootCmd.AddCommand(contextCmd)
}
