# OTel SIG Tracker

A Go CLI tool that ingests OpenTelemetry SIG meeting recordings, meeting notes, and Slack discussions, then uses an LLM to produce Markdown intelligence reports focused on topics relevant to Datadog.

## What It Does

The OpenTelemetry project has **54 SIGs** (Special Interest Groups) that meet regularly. Each SIG produces meeting notes (Google Docs), video recordings (Zoom), and Slack discussions. Keeping up with all of them is impractical.

This tool automates it:

1. **Fetches** meeting notes, video transcripts, and Slack messages for the SIGs you care about
2. **Summarizes** each source using an LLM (Claude or GPT)
3. **Synthesizes** across sources to deduplicate and connect related discussions
4. **Scores** each topic for Datadog relevance (HIGH / MEDIUM / LOW)
5. **Generates** per-SIG reports and a weekly digest in Markdown or JSON

## Quick Start

### Prerequisites

- **Go 1.21+**
- **Google Chrome or Chromium** — required for Zoom transcript extraction
- **Anthropic API key** — required for LLM analysis ([console.anthropic.com](https://console.anthropic.com))

### Install

```bash
git clone https://github.com/gordyrad/otel-sig-tracker.git
cd otel-sig-tracker
go build -o otel-sig-scraper .
```

> **Note:** If `go build` fails with a proxy timeout, prefix with `GOPROXY=https://proxy.golang.org,direct`.

### Run

```bash
# Set your API key
export ANTHROPIC_API_KEY=sk-ant-...

# Generate a report for the last 7 days
./otel-sig-scraper report --lookback 7d

# Scope to specific SIGs
./otel-sig-scraper report --lookback 14d --sigs collector,specification,java-sdk

# List all available SIGs
./otel-sig-scraper list-sigs
```

Reports are written to `./reports/` by default.

## Commands

| Command | Description |
|---------|-------------|
| `report` | Fetch data, run LLM analysis, generate reports |
| `fetch` | Fetch and cache data without running analysis |
| `list-sigs` | List all available OTel SIGs |
| `slack-login` | Authenticate with CNCF Slack (interactive browser) |
| `slack-status` | Check Slack authentication status |
| `context show` | Show custom context injected into LLM prompts |
| `context set` | Set custom context from `--file` or `--text` |
| `context clear` | Remove custom context |

## Data Sources

| Source | Auth Required | Method |
|--------|--------------|--------|
| **SIG Registry** | None | Parsed from [community README](https://github.com/open-telemetry/community) |
| **Meeting Notes** | None | Google Docs public export (plain text) |
| **Video Transcripts** | None | Zoom auto-generated WebVTT via headless Chrome |
| **Slack Messages** | Yes (interactive login) | CNCF Slack API with browser session tokens |

Slack is optional. If not configured, reports are generated from meeting notes and video transcripts only.

## Configuration

Configuration is loaded from (in order of precedence): CLI flags > environment variables > YAML config file.

### Environment Variables

```bash
export ANTHROPIC_API_KEY=sk-ant-...   # Required (or OPENAI_API_KEY)
export OTEL_OUTPUT_DIR=./reports       # Output directory
export OTEL_WORKERS=4                  # Concurrent workers
export OTEL_FORMAT=markdown            # markdown or json
export OTEL_VERBOSE=true               # Verbose logging
```

### YAML Config File

```bash
cp config.example.yaml config.yaml
# Edit to taste, then:
./otel-sig-scraper report --config config.yaml
```

### Key Flags

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--lookback` | `OTEL_LOOKBACK` | `7d` | Time window: `7d`, `2w`, `1m` |
| `--sigs` | `OTEL_SIGS` | all | Comma-separated SIG names |
| `--format` | `OTEL_FORMAT` | `markdown` | Output format: `markdown`, `json` |
| `--output-dir` | `OTEL_OUTPUT_DIR` | `./reports` | Report output directory |
| `--workers` | `OTEL_WORKERS` | `4` | Concurrent fetch/analysis workers |
| `--llm-provider` | `OTEL_LLM_PROVIDER` | `anthropic` | LLM provider: `anthropic`, `openai` |
| `--llm-model` | `OTEL_LLM_MODEL` | `claude-sonnet-4-20250514` | Model to use |
| `--skip-videos` | — | `false` | Skip Zoom transcript extraction |
| `--skip-slack` | — | `false` | Skip Slack fetching |
| `--skip-notes` | — | `false` | Skip Google Docs meeting notes |
| `--offline` | — | `false` | Analyze cached data only (no source fetching) |
| `--db-path` | `OTEL_DB_PATH` | `./otel-sig-scraper.db` | SQLite database path |
| `--verbose` | `OTEL_VERBOSE` | `false` | Verbose logging |

## Report Format

### Per-SIG Report

Each SIG gets a report like `reports/2026-02-19-collector-report.md`:

```
# OTel Collector SIG Report — Feb 12-19, 2026

> Generated: 2026-02-19T14:30:00Z | Sources: meeting notes ✓ video ✓ slack ✓

## Executive Summary
Brief overview of the most important developments...

## HIGH Relevance to Datadog
### OTLP/HTTP Partial Success
- **What**: New partial success response support
- **Why it matters**: Directly affects Datadog OTLP ingest pipeline
- **Action recommended**: Review the OTEP draft

## MEDIUM Relevance to Datadog
### Pipeline Fan-out/Fan-in
- **What**: Architectural change for fan-out patterns
- **Context**: Could affect Datadog exporter pipeline configuration

## LOW Relevance / FYI
- Batch processor memory improvements (40% reduction)
```

### Weekly Digest

A cross-SIG summary at `reports/2026-02-19-weekly-digest.md` with top items, per-SIG summaries, cross-SIG themes, and a processing stats table.

## Slack Authentication

CNCF Slack doesn't support bot tokens, so this tool uses interactive browser login:

```bash
# Opens a Chromium window — log in with your CNCF Slack account
./otel-sig-scraper slack-login

# Check session status (sessions last days to weeks)
./otel-sig-scraper slack-status
```

Credentials are stored at `~/.config/otel-sig-scraper/slack-credentials.json` with `0600` permissions. Re-run `slack-login` when the session expires.

## Custom Context

Customize the Datadog relevance scoring by injecting additional context:

```bash
# From a file
./otel-sig-scraper context set --file my-priorities.md

# Inline
./otel-sig-scraper context set --text "We are migrating trace ingest to OTLP. Any OTLP format changes are HIGH priority."

# View / clear
./otel-sig-scraper context show
./otel-sig-scraper context clear
```

Custom context is only used during the relevance scoring pass — source summaries remain neutral.

## Common Workflows

### Pre-cache data, analyze later

```bash
# Fetch everything (no LLM cost)
./otel-sig-scraper fetch --lookback 30d

# Analyze from cache (no network calls to sources)
./otel-sig-scraper report --lookback 7d --offline
```

### JSON output for a web UI

```bash
./otel-sig-scraper report --lookback 7d --format json --output-dir ./api/data
```

### Cron job (weekly report)

```bash
0 9 * * MON ANTHROPIC_API_KEY=sk-ant-... /usr/local/bin/otel-sig-scraper report --lookback 7d --output-dir /var/reports
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Partial failure — some sources failed, report generated from available data |
| 2 | Fatal error — no data could be fetched |
| 3 | Configuration error |

## Development

### Project Structure

```
├── main.go                    # Entrypoint
├── cmd/                       # CLI commands (cobra)
├── internal/
│   ├── config/                # Configuration loading
│   ├── store/                 # SQLite storage + migrations
│   ├── registry/              # SIG registry parser
│   ├── sources/               # Data fetchers (Docs, Sheets, Zoom, Slack)
│   ├── analysis/              # LLM clients + summarization + scoring
│   ├── report/                # Markdown + JSON report generators
│   ├── browser/               # Chromedp browser pool
│   └── pipeline/              # Orchestration
├── reports/                   # Generated reports (gitignored)
├── testdata/                  # Test fixtures
├── AGENTS.md                  # Architecture docs for AI agents
└── config.example.yaml        # Example configuration
```

### Running Tests

```bash
go test ./...
```

156 tests covering all packages — store, config, registry, sources, analysis, report, pipeline, and CLI commands.

### Architecture

See [AGENTS.md](./AGENTS.md) for detailed architecture documentation, package responsibilities, coding conventions, and dependency notes.

## License

TBD
