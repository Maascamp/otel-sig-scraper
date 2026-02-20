package cmd

import (
	"fmt"
	"os"

	"github.com/gordyrad/otel-sig-tracker/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfg *config.Config

var rootCmd = &cobra.Command{
	Use:   "otel-sig-scraper",
	Short: "OpenTelemetry SIG intelligence tracker",
	Long: `A CLI tool that ingests OpenTelemetry SIG meeting recordings, meeting notes,
and Slack discussions, then uses an LLM to produce Markdown intelligence reports
focused on topics relevant to Datadog.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	cobra.OnInitialize(initConfig)

	pf := rootCmd.PersistentFlags()
	pf.String("lookback", "7d", "How far back to look (e.g., 7d, 2w, 1m)")
	pf.StringSlice("sigs", nil, "Comma-separated SIG names to process")
	pf.StringSlice("topics", nil, "Comma-separated topic filters")
	pf.String("output-dir", "./reports", "Output directory for reports")
	pf.String("format", "markdown", "Output format: markdown, json")
	pf.String("llm-provider", "anthropic", "LLM provider: anthropic, openai")
	pf.String("llm-model", "claude-sonnet-4-20250514", "LLM model to use")
	pf.String("anthropic-api-key", "", "Anthropic API key")
	pf.String("openai-api-key", "", "OpenAI API key")
	pf.String("slack-creds", "", "Slack credentials file path")
	pf.String("context-file", "", "Custom context file path")
	pf.String("db-path", "./otel-sig-scraper.db", "SQLite database path")
	pf.Int("workers", 4, "Number of concurrent workers")
	pf.Bool("skip-videos", false, "Skip video transcription")
	pf.Bool("skip-slack", false, "Skip Slack fetching")
	pf.Bool("skip-notes", false, "Skip Google Docs meeting notes")
	pf.Bool("offline", false, "Use only cached data")
	pf.Bool("verbose", false, "Verbose logging")
	pf.String("config", "", "Path to YAML config file")

	// Bind flags to viper
	flags := []string{
		"lookback", "sigs", "topics", "output-dir", "format",
		"llm-provider", "llm-model", "anthropic-api-key", "openai-api-key",
		"slack-creds", "context-file", "db-path", "workers",
		"skip-videos", "skip-slack", "skip-notes", "offline", "verbose", "config",
	}
	for _, f := range flags {
		_ = viper.BindPFlag(f, pf.Lookup(f))
	}
}

func initConfig() {
	cfg = config.DefaultConfig()

	configFile := viper.GetString("config")
	if configFile != "" {
		viper.SetConfigFile(configFile)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
	}

	viper.SetEnvPrefix("")
	viper.AutomaticEnv()

	// Bind environment variables
	_ = viper.BindEnv("anthropic-api-key", "ANTHROPIC_API_KEY")
	_ = viper.BindEnv("openai-api-key", "OPENAI_API_KEY")
	_ = viper.BindEnv("lookback", "OTEL_LOOKBACK")
	_ = viper.BindEnv("output-dir", "OTEL_OUTPUT_DIR")
	_ = viper.BindEnv("format", "OTEL_FORMAT")
	_ = viper.BindEnv("llm-provider", "OTEL_LLM_PROVIDER")
	_ = viper.BindEnv("llm-model", "OTEL_LLM_MODEL")
	_ = viper.BindEnv("db-path", "OTEL_DB_PATH")
	_ = viper.BindEnv("workers", "OTEL_WORKERS")
	_ = viper.BindEnv("verbose", "OTEL_VERBOSE")
	_ = viper.BindEnv("slack-creds", "OTEL_SLACK_CREDS")
	_ = viper.BindEnv("context-file", "OTEL_CONTEXT_FILE")

	_ = viper.ReadInConfig()

	// Apply viper values to config
	if v := viper.GetString("lookback"); v != "" {
		if d, err := config.ParseLookback(v); err == nil {
			cfg.Lookback = d
		}
	}
	if v := viper.GetStringSlice("sigs"); len(v) > 0 {
		cfg.SIGs = v
	}
	if v := viper.GetStringSlice("topics"); len(v) > 0 {
		cfg.Topics = v
	}
	if v := viper.GetString("output-dir"); v != "" {
		cfg.OutputDir = v
	}
	if v := viper.GetString("format"); v != "" {
		cfg.Format = v
	}
	if v := viper.GetString("llm-provider"); v != "" {
		cfg.LLM.Provider = v
	}
	if v := viper.GetString("llm-model"); v != "" {
		cfg.LLM.Model = v
	}
	if v := viper.GetString("anthropic-api-key"); v != "" {
		cfg.LLM.AnthropicKey = v
	}
	if v := viper.GetString("openai-api-key"); v != "" {
		cfg.LLM.OpenAIKey = v
	}
	if v := viper.GetString("slack-creds"); v != "" {
		cfg.Slack.CredentialsFile = v
	}
	if v := viper.GetString("context-file"); v != "" {
		cfg.ContextFile = v
	}
	if v := viper.GetString("db-path"); v != "" {
		cfg.DBPath = v
	}
	if v := viper.GetInt("workers"); v > 0 {
		cfg.Workers = v
	}
	cfg.SkipVideos = viper.GetBool("skip-videos")
	cfg.SkipSlack = viper.GetBool("skip-slack")
	cfg.SkipNotes = viper.GetBool("skip-notes")
	cfg.Offline = viper.GetBool("offline")
	cfg.Verbose = viper.GetBool("verbose")
}

// Execute runs the root command.
func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}
	return nil
}
