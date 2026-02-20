package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all application configuration.
type Config struct {
	Lookback    time.Duration
	SIGs        []string
	Topics      []string
	OutputDir   string
	Format      string // "markdown" or "json"
	DBPath      string
	Workers     int
	Verbose     bool
	Offline     bool
	SkipVideos  bool
	SkipSlack   bool
	SkipNotes   bool
	ConfigFile  string
	ContextFile string

	LLM   LLMConfig
	Slack SlackConfig
}

// LLMConfig holds LLM provider configuration.
type LLMConfig struct {
	Provider      string // "anthropic" or "openai"
	Model         string
	AnthropicKey  string
	OpenAIKey     string
}

// SlackConfig holds Slack credential paths.
type SlackConfig struct {
	CredentialsFile string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".config", "otel-sig-scraper")

	return &Config{
		Lookback:    7 * 24 * time.Hour,
		OutputDir:   "./reports",
		Format:      "markdown",
		DBPath:      "./otel-sig-scraper.db",
		Workers:     4,
		ContextFile: filepath.Join(configDir, "custom-context.md"),
		LLM: LLMConfig{
			Provider: "anthropic",
			Model:    "claude-sonnet-4-20250514",
		},
		Slack: SlackConfig{
			CredentialsFile: filepath.Join(configDir, "slack-credentials.json"),
		},
	}
}

// Load reads configuration from flags, environment, and optional YAML file.
func Load() (*Config, error) {
	cfg := DefaultConfig()

	// Bind environment variables
	viper.SetEnvPrefix("")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))

	// Map env vars
	envMappings := map[string]string{
		"OTEL_LOOKBACK":     "lookback",
		"OTEL_SIGS":         "sigs",
		"OTEL_TOPICS":       "topics",
		"OTEL_OUTPUT_DIR":   "output-dir",
		"OTEL_FORMAT":       "format",
		"OTEL_LLM_PROVIDER": "llm.provider",
		"OTEL_LLM_MODEL":    "llm.model",
		"ANTHROPIC_API_KEY":  "llm.anthropic-key",
		"OPENAI_API_KEY":     "llm.openai-key",
		"OTEL_SLACK_CREDS":  "slack.credentials-file",
		"OTEL_CONTEXT_FILE": "context-file",
		"OTEL_DB_PATH":      "db-path",
		"OTEL_WORKERS":      "workers",
		"OTEL_VERBOSE":      "verbose",
	}
	for env, key := range envMappings {
		_ = viper.BindEnv(key, env)
	}

	// Load config file if specified
	if cfg.ConfigFile != "" {
		viper.SetConfigFile(cfg.ConfigFile)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(".")
	}

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok && cfg.ConfigFile != "" {
			return nil, fmt.Errorf("reading config file: %w", err)
		}
	}

	return cfg, nil
}

// ParseLookback parses a lookback string like "7d", "2w", "1m" into a duration.
// Supports: Nd (days), Nw (weeks), Nm (months of 30 days), and standard Go durations like "1h".
func ParseLookback(s string) (time.Duration, error) {
	if s == "" {
		return 7 * 24 * time.Hour, nil
	}

	s = strings.TrimSpace(strings.ToLower(s))

	if len(s) < 2 {
		return 0, fmt.Errorf("invalid lookback format: %q", s)
	}

	numStr := s[:len(s)-1]
	unit := s[len(s)-1]

	// Try our custom d/w/m suffixes first (these take priority over Go duration parsing)
	if unit == 'd' || unit == 'w' || unit == 'm' {
		var num int
		if _, err := fmt.Sscanf(numStr, "%d", &num); err == nil {
			switch unit {
			case 'd':
				return time.Duration(num) * 24 * time.Hour, nil
			case 'w':
				return time.Duration(num) * 7 * 24 * time.Hour, nil
			case 'm':
				return time.Duration(num) * 30 * 24 * time.Hour, nil
			}
		}
	}

	// Fall back to standard Go duration (e.g., "1h", "30s", "2h30m")
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	return 0, fmt.Errorf("invalid lookback format: %q (use Nd, Nw, Nm, or Go duration like 1h)", s)
}

// Validate checks config for errors.
func (c *Config) Validate() error {
	if c.Workers < 1 {
		return fmt.Errorf("workers must be >= 1, got %d", c.Workers)
	}
	if c.Format != "markdown" && c.Format != "json" {
		return fmt.Errorf("format must be 'markdown' or 'json', got %q", c.Format)
	}
	if c.LLM.Provider != "anthropic" && c.LLM.Provider != "openai" {
		return fmt.Errorf("llm provider must be 'anthropic' or 'openai', got %q", c.LLM.Provider)
	}
	if !c.Offline {
		switch c.LLM.Provider {
		case "anthropic":
			if c.LLM.AnthropicKey == "" {
				return fmt.Errorf("ANTHROPIC_API_KEY is required when using anthropic provider")
			}
		case "openai":
			if c.LLM.OpenAIKey == "" {
				return fmt.Errorf("OPENAI_API_KEY is required when using openai provider")
			}
		}
	}
	return nil
}
