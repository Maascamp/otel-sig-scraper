# OTel SIG Tracker

## Quick Reference

- **Language**: Go
- **Build**: `go build -o otel-sig-scraper .`
- **Test**: `go test ./...`
- **Lint**: `go vet ./...`
- **Go proxy workaround**: Prefix go commands with `GOPROXY=https://proxy.golang.org,direct` if the default proxy times out

## Architecture & Agent Guide

See [AGENTS.md](./AGENTS.md) for full architecture documentation, package map, coding conventions, and notes for AI agents working on this codebase.

## Key Commands

```bash
# Full report
./otel-sig-scraper report --lookback 7d

# Fetch only (populate cache)
./otel-sig-scraper fetch --lookback 30d --sigs collector

# List SIGs
./otel-sig-scraper list-sigs

# Slack auth
./otel-sig-scraper slack-login

# Custom context
./otel-sig-scraper context set --file context.md
./otel-sig-scraper context show
```

## Required Environment Variables

```bash
ANTHROPIC_API_KEY=sk-ant-...  # Required for LLM analysis (or OPENAI_API_KEY)
```

## Project Structure

```
├── main.go              # Entrypoint
├── cmd/                 # CLI commands (cobra)
├── internal/
│   ├── config/          # Configuration
│   ├── registry/        # SIG registry parser
│   ├── sources/         # Data fetchers (Google Docs, Sheets, Zoom, Slack)
│   ├── store/           # SQLite storage
│   ├── analysis/        # LLM analysis pipeline
│   ├── report/          # Report generators (Markdown, JSON)
│   ├── browser/         # Chromedp browser management
│   └── pipeline/        # Orchestration
├── reports/             # Generated reports output
└── testdata/            # Test fixtures
```
