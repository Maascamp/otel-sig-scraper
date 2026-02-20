package config

import (
	"testing"
	"time"
)

func TestParseLookback(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
		err   bool
	}{
		{"7d", 7 * 24 * time.Hour, false},
		{"14d", 14 * 24 * time.Hour, false},
		{"2w", 14 * 24 * time.Hour, false},
		{"1m", 30 * 24 * time.Hour, false},
		{"", 7 * 24 * time.Hour, false}, // default
		{"1h", time.Hour, false},                   // standard duration
		{"30m", 30 * 30 * 24 * time.Hour, false},  // 30 months (custom format takes priority)
		{"2h30m0s", 2*time.Hour + 30*time.Minute, false}, // standard Go duration
		{"abc", 0, true},
		{"x", 0, true},
		{"7x", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseLookback(tt.input)
			if (err != nil) != tt.err {
				t.Errorf("ParseLookback(%q) error = %v, wantErr %v", tt.input, err, tt.err)
				return
			}
			if !tt.err && got != tt.want {
				t.Errorf("ParseLookback(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Lookback != 7*24*time.Hour {
		t.Errorf("Lookback = %v, want 7d", cfg.Lookback)
	}
	if cfg.OutputDir != "./reports" {
		t.Errorf("OutputDir = %q, want %q", cfg.OutputDir, "./reports")
	}
	if cfg.Format != "markdown" {
		t.Errorf("Format = %q, want %q", cfg.Format, "markdown")
	}
	if cfg.Workers != 4 {
		t.Errorf("Workers = %d, want 4", cfg.Workers)
	}
	if cfg.LLM.Provider != "anthropic" {
		t.Errorf("LLM.Provider = %q, want %q", cfg.LLM.Provider, "anthropic")
	}
	if cfg.LLM.Model != "claude-sonnet-4-20250514" {
		t.Errorf("LLM.Model = %q, want %q", cfg.LLM.Model, "claude-sonnet-4-20250514")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid config with anthropic key",
			modify:  func(c *Config) { c.LLM.AnthropicKey = "sk-ant-test" },
			wantErr: false,
		},
		{
			name:    "valid offline config (no key needed)",
			modify:  func(c *Config) { c.Offline = true },
			wantErr: false,
		},
		{
			name:    "invalid workers",
			modify:  func(c *Config) { c.Workers = 0; c.LLM.AnthropicKey = "k" },
			wantErr: true,
		},
		{
			name:    "invalid format",
			modify:  func(c *Config) { c.Format = "xml"; c.LLM.AnthropicKey = "k" },
			wantErr: true,
		},
		{
			name:    "invalid provider",
			modify:  func(c *Config) { c.LLM.Provider = "gemini" },
			wantErr: true,
		},
		{
			name:    "missing anthropic key",
			modify:  func(c *Config) {},
			wantErr: true,
		},
		{
			name:    "missing openai key",
			modify:  func(c *Config) { c.LLM.Provider = "openai" },
			wantErr: true,
		},
		{
			name:    "valid openai config",
			modify:  func(c *Config) { c.LLM.Provider = "openai"; c.LLM.OpenAIKey = "sk-test" },
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
