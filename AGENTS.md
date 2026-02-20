# AGENTS.md — OTel SIG Tracker

## Project Overview

A Go CLI tool (`otel-sig-scraper`) that ingests OpenTelemetry SIG meeting recordings, meeting notes, and Slack discussions, then uses an LLM to produce Markdown intelligence reports focused on topics relevant to Datadog.

## Architecture

```
CLI (cobra) → Pipeline Orchestrator → [Fetchers → Store → Analyzers → Report Generators]
```

### Package Map

| Package | Path | Responsibility |
|---------|------|---------------|
| **cmd** | `cmd/` | CLI commands (cobra). Root, report, fetch, list-sigs, slack-login, slack-status, context |
| **config** | `internal/config/` | Configuration loading from flags, env vars, YAML |
| **registry** | `internal/registry/` | SIG registry parser (GitHub README). Name matching for recordings ↔ SIGs |
| **sources** | `internal/sources/` | Data fetchers: Google Docs, Google Sheets, Zoom transcripts, Slack messages + auth |
| **store** | `internal/store/` | SQLite storage layer. Schema migrations. All CRUD operations |
| **analysis** | `internal/analysis/` | LLM client abstraction (Anthropic/OpenAI), summarization, synthesis, relevance scoring |
| **report** | `internal/report/` | Markdown and JSON report generators |
| **browser** | `internal/browser/` | Shared chromedp (headless Chrome) browser pool management |
| **pipeline** | `internal/pipeline/` | Orchestration: fetch → analyze → report. Concurrency via errgroup |

### Data Flow

1. **Registry** → Parse OTel community README → Get SIG metadata (name, doc IDs, Slack channels)
2. **Fetch** (concurrent per SIG):
   - Google Docs → meeting notes text → parse by date → SQLite
   - Google Sheets → recording CSV → Zoom share pages → VTT transcripts → SQLite
   - Slack API → channel history + threads → SQLite
3. **Analyze** (concurrent per SIG):
   - Per-source summarization (meeting notes, video, slack) via LLM
   - Cross-source synthesis via LLM
   - Datadog relevance scoring via LLM
4. **Report** → Generate per-SIG Markdown + weekly digest

### Key Dependencies

| Dependency | Purpose |
|-----------|---------|
| `modernc.org/sqlite` | Pure-Go SQLite (no CGo) |
| `github.com/spf13/cobra` | CLI framework |
| `github.com/spf13/viper` | Configuration management |
| `github.com/liushuangls/go-anthropic/v2` | Anthropic Claude API client |
| `github.com/sashabaranov/go-openai` | OpenAI API client |
| `github.com/slack-go/slack` | Slack API client |
| `github.com/chromedp/chromedp` | Headless Chrome for Zoom transcripts + Slack login |
| `golang.org/x/time/rate` | Rate limiting |
| `golang.org/x/sync/errgroup` | Concurrent worker pools |

### System Requirements

- **Go 1.21+**
- **Google Chrome or Chromium** — required for Zoom transcript extraction and Slack login
- **Anthropic API key** (or OpenAI) — required for LLM analysis

## Coding Conventions

- Use `internal/` for all non-CLI packages (Go convention for unexported packages)
- Error wrapping: use `fmt.Errorf("context: %w", err)` consistently
- Logging: use `log` package with prefix for source identification
- Context: pass `context.Context` as first parameter to all IO/network functions
- Store operations: always use the Store methods, never raw SQL outside store package
- Configuration: access via the `*config.Config` struct, never read env vars directly in packages
- Concurrency: use `errgroup` with `cfg.Workers` limit for parallel operations
- HTTP clients: always set timeouts, never use `http.DefaultClient`

## Testing

- Unit tests in `*_test.go` files alongside source
- Test fixtures in `testdata/`
- Store tests use in-memory SQLite (`:memory:`)
- Network-dependent tests use httptest.NewServer for mocking
- Run all tests: `go test ./...`
- Run with verbose: `go test -v ./...`
- Run specific package: `go test ./internal/store/...`

## Build & Run

```bash
# Build
go build -o otel-sig-scraper .

# Run
./otel-sig-scraper report --lookback 7d
./otel-sig-scraper list-sigs
./otel-sig-scraper fetch --lookback 30d --sigs collector

# Environment variables
export ANTHROPIC_API_KEY=sk-ant-...
export OTEL_OUTPUT_DIR=./reports
```

## Important Notes for AI Agents

1. **Go proxy**: If `go get` or `go mod tidy` fails, use `GOPROXY=https://proxy.golang.org,direct` prefix
2. **SQLite**: Uses pure-Go driver (`modernc.org/sqlite`), no CGo required
3. **Chromedp**: Requires Chrome/Chromium installed on the system. Uses headless mode for Zoom, visible mode for Slack login
4. **Slack auth**: Uses browser session tokens (`xoxc-` + `d` cookie), not bot tokens. Requires interactive login
5. **Google APIs**: All OTel docs/sheets are public. No Google API key needed — uses export URLs
6. **Name matching**: Recording names in Google Sheets don't exactly match SIG registry names. See `registry.MatchSheetNameToSIG()` and `registry.NameMappings`
7. **Date parsing**: Meeting notes use various date formats. The parser must be flexible
8. **Graceful degradation**: If a source fails, continue with others. Reports note which sources were unavailable
