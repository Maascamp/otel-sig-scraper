# OTel SIG Scraper — Comprehensive Build Plan

## Project Overview

A Go CLI tool that ingests OpenTelemetry SIG meeting recordings, meeting notes, and Slack discussions, then uses an LLM to produce Markdown intelligence reports focused on topics relevant to Datadog. Designed for direct CLI use, cron scheduling, or invocation by a separate web UI.

---

## 1. Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    otel-sig-scraper CLI                      │
│                                                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────────┐  │
│  │ SIG      │  │ Google   │  │ Zoom     │  │ Slack      │  │
│  │ Registry │  │ Docs     │  │ Recording│  │ Channel    │  │
│  │ Parser   │  │ Fetcher  │  │ Fetcher  │  │ Fetcher    │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  └─────┬──────┘  │
│       │             │             │               │         │
│       └──────┬──────┴──────┬──────┘               │         │
│              │             │                      │         │
│              ▼             ▼                      ▼         │
│       ┌────────────────────────────────────────────────┐    │
│       │              Content Store (SQLite)             │    │
│       │  - raw meeting notes text                      │    │
│       │  - video transcripts                           │    │
│       │  - slack messages                              │    │
│       │  - processing metadata & dedup                 │    │
│       └──────────────────┬─────────────────────────────┘    │
│                          │                                  │
│                          ▼                                  │
│       ┌────────────────────────────────────────────────┐    │
│       │           LLM Analysis Pipeline                 │    │
│       │  1. Per-source summarization                    │    │
│       │  2. Cross-source synthesis                      │    │
│       │  3. Datadog relevance scoring & tagging         │    │
│       └──────────────────┬─────────────────────────────┘    │
│                          │                                  │
│                          ▼                                  │
│       ┌────────────────────────────────────────────────┐    │
│       │         Markdown Report Generator               │    │
│       │  - reports/<date>-<sig>-report.md               │    │
│       │  - reports/<date>-weekly-digest.md              │    │
│       └────────────────────────────────────────────────┘    │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Key Design Decisions

- **Language**: Go — single binary, fast, great concurrency, portable
- **Storage**: SQLite via `modernc.org/sqlite` (pure Go, no CGo) — dedup, incremental processing, caching
- **LLM**: Anthropic Claude API (primary, confirmed available) — configurable to support OpenAI as alternative
- **Concurrency**: Worker pool with configurable parallelism for fetching + LLM calls
- **Output**: Local Markdown files in a configurable output directory
- **Config**: CLI flags + env vars + optional YAML config file

---

## 2. Data Sources — Detailed Design

### 2.1 SIG Registry (GitHub README Parser)

**Source**: `https://raw.githubusercontent.com/open-telemetry/community/main/README.md`

**Data available per SIG** (54 SIGs total across 4 categories):
| Field | Example |
|-------|--------|
| Name | `Collector` |
| Category | `Implementation SIG` |
| Meeting Time | `Wednesday at 09:00 PT` |
| Notes Doc ID | `1r2JC5MB7ab...` |
| Slack Channel ID | `C01N7PP1THC` |
| Slack Channel Name | `#otel-collector` |

**Parsing approach**: Line-by-line pipe-delimited table parsing with regex extraction. The tables appear under `### Specification SIGs`, `### Implementation SIGs`, `### Cross-Cutting SIGs`, `### Localization Teams` headers.

**Cache strategy**: Re-fetch at most once per run. Store parsed registry in SQLite for reference.

### 2.2 Meeting Notes (Google Docs)

**Source**: Each SIG has a single running Google Doc (54 docs total) with meeting notes appended chronologically.

**API Options** (in preference order):
1. **Google Docs API** (`docs.googleapis.com/v1/documents/{docId}`) — returns structured JSON with paragraphs, headings, formatting. Requires API key or service account. **Best option**: structured, reliable, handles large docs.
2. **Google Docs export** (`docs.google.com/document/d/{id}/export?format=txt`) — plain text export. Works without auth for public docs. **Fallback option**.

**Parsing strategy**:
- Meeting notes follow a consistent pattern: date heading → attendees → agenda → discussion → action items
- Parse by date headings to extract individual meetings
- Filter to only meetings within the requested time window
- Each SIG doc is a single ever-growing document, so we need to parse dates from content

**Incremental processing**:
- Store last-processed content hash per doc in SQLite
- On re-run, only process new content (compare against stored text)

### 2.3 Meeting Recordings (Google Sheets → Zoom)

**Source**: Google Spreadsheet `1SYKfjYhZdm2Wh2Cl6KVQalKg_m4NhTPZqq-8SzEVO6s`

**Spreadsheet structure** (~1500 rows):
| Column | Example |
|--------|---------|
| Name | `Collector SIG` |
| Start time | `2025-06-16 8:59:46` |
| Duration | `54` (minutes) |
| URL | `https://zoom.us/rec/share/...` |

**Fetching**: Export as CSV via `https://docs.google.com/spreadsheets/d/{id}/export?format=csv` (no auth needed for public sheet).

**Video → Text pipeline** (VERIFIED WORKING):

Zoom share links are **publicly accessible** (no auth, no password). Most recordings include **auto-generated transcripts** in WebVTT format with speaker names and timestamps. Short recordings (<2 min) may lack transcripts.

**Transcript extraction flow** (verified against real recordings):
1. **Load share page via headless browser** (chromedp in Go) — the page is a Vue 2 SPA
2. **Extract from Vue store**: `document.querySelector('#app').__vue__.$store.state` provides:
   - `hasTranscript` (bool) — whether transcript exists
   - `transcriptUrl` — relative URL like `/rec/play/vtt?type=transcript&fid={playCheckId}&action=play`
   - `playCheckId` — the token needed for the VTT URL
   - `topic`, `duration` — metadata
3. **Download VTT via plain HTTP GET** — `https://zoom.us{transcriptUrl}` — no auth needed
4. **Parse VTT** — standard WebVTT format with speaker names:
   ```
   WEBVTT
   1
   00:03:59.730 --> 00:04:01.619
   Pablo Baeyens: Should we get started?
   ```
5. **Cache**: Store transcript text in SQLite keyed by Zoom URL

**Why headless browser is needed**: The meetingId in the static HTML is encrypted; the Vue app's JavaScript decrypts it to produce the `fileId`/`playCheckId` used for the play/info API call. There is no practical way to replicate this decryption without executing the JS. The API flow is: share page → JS decrypts meetingId → calls `/nws/recording/1.0/play/info/{fileId}` → returns recording metadata including VTT URL.

**Fallback for recordings without transcripts**: Download audio and use Whisper API. But this should be rare — tested recordings of 10+ minutes all had transcripts.

**Performance note**: Each headless browser page load takes ~3-5 seconds. With 4 workers processing 50 recordings, expect ~1-2 minutes for the transcript extraction phase. VTT downloads are instant (<100ms each).

**Challenges & mitigations**:
- Very short recordings (<2 min) may lack transcripts → skip gracefully, these are typically empty/canceled meetings
- Name matching between sheet and SIG registry is fuzzy → build a name-mapping table
- Zoom may rate-limit headless browser requests → add configurable delay between requests

### 2.4 Slack Messages (CNCF Slack)

**Source**: CNCF Slack workspace (`cloud-native.slack.com`), channels like `#otel-collector`, `#otel-specification`, etc.

**API**: Slack Web API with **User Token** (bot token NOT available — CNCF workspace won't grant one)

**Access approach: Headless Browser Login → Token Extraction → REST API**

Creating a Slack App requires workspace admin approval on CNCF Slack, so we use browser-based authentication instead. Since we already need headless Chromium for Zoom transcripts, we reuse it here.

**Flow:**
1. **`otel-sig-scraper slack-login`** — launches a **visible** (non-headless) Chromium window to `cloud-native.slack.com`
2. User authenticates interactively (Google OAuth, email magic code, or password)
3. After login, the tool extracts from the loaded page:
   - `xoxc-` token from `window.boot_data.api_token` (or equivalent JS variable)
   - `d` cookie from the browser cookie jar
   - Team ID and User ID for validation
4. Tokens are stored locally in `~/.config/otel-sig-scraper/slack-credentials.json`
5. On subsequent runs, tokens are loaded from disk and validated via `auth.test`
6. If expired, the tool prompts: "Slack session expired. Run `otel-sig-scraper slack-login` to re-authenticate."

**API usage (with extracted tokens):**
```bash
curl -H "Authorization: Bearer xoxc-..." \
     -b "d=xoxd-..." \
     "https://slack.com/api/conversations.history?channel=C01N7PP1THC&limit=100"
```

The `xoxc-` + `d` cookie combo works with the standard Slack Web API including `conversations.history`, `conversations.replies`, `conversations.list`, and `auth.test`. No special app or scopes needed — it inherits the logged-in user's permissions (read access to all public channels).

**Token lifetime**: Typically days to weeks. The tool checks validity at the start of each run.

**CNCF Slack has NO public archives** — verified. Login page supports Google, Apple, or email magic code (no password by default).

**Fetching strategy**:
- For each SIG's Slack channel within the time window:
  - Fetch messages with `conversations.history` (paginated, 200/page)
  - For messages with `reply_count > 0`, fetch threads with `conversations.replies`
  - Rate limit: Slack Tier 3 = ~50 req/min → use rate limiter
- Store raw messages + threads in SQLite

**Graceful degradation**: If no Slack token is configured, skip Slack entirely and generate reports from meeting notes + video transcripts only. The CLI should clearly indicate which sources were used.

---

## 3. LLM Analysis Pipeline

### 3.1 Per-Source Summarization

For each SIG within the time window, generate summaries from each available source:

**Meeting Notes Summary** prompt:
```
You are analyzing OpenTelemetry SIG meeting notes for the {SIG_NAME} SIG.
Summarize the key discussions, decisions, and action items from the following
meeting notes dated between {START_DATE} and {END_DATE}.
Focus on: technical decisions, new features, breaking changes, deprecations,
integration changes, protocol/format changes, and anything affecting
telemetry pipelines or clients.
```

**Video Transcript Summary** prompt:
```
You are analyzing a transcript of the {SIG_NAME} SIG meeting from {DATE}.
Summarize the key technical discussions, noting any decisions made,
controversies, and planned work. Identify speakers and their positions
where possible.
```

**Slack Discussion Summary** prompt:
```
You are analyzing Slack discussions from the #{CHANNEL_NAME} channel
({SIG_NAME} SIG) between {START_DATE} and {END_DATE}.
Identify the most significant technical discussions, questions,
and announcements. Group by topic.
```

### 3.2 Cross-Source Synthesis

Merge per-source summaries into a unified SIG report:
```
Given the following summaries from meeting notes, video recordings,
and Slack discussions for the {SIG_NAME} SIG, produce a unified report.
Deduplicate topics discussed across sources. Flag items where different
sources provide complementary information.
```

### 3.3 Datadog Relevance Scoring

Apply a Datadog-focused lens:
```
You are producing an intelligence report for Datadog engineers.
Score each topic's relevance to Datadog (HIGH/MEDIUM/LOW) based on:
- Direct impact on Datadog's OTLP ingest pipeline
- Changes to trace/metric/log formats or semantic conventions
- New instrumentation that Datadog should support
- Collector changes affecting Datadog exporter
- Competitive landscape (features overlapping with Datadog products)
- SDK changes affecting Datadog's tracing libraries
- Changes to sampling, context propagation, or resource detection
- OpAMP or agent management developments
- Profiling signal developments

For HIGH relevance items, provide specific actionable recommendations.
```

### 3.4 Token Budget Management

- Meeting notes docs can be very large (years of history) → only extract relevant date range
- Transcripts of 60-min meetings ≈ 10-15K words ≈ 15-20K tokens → chunk if needed
- Use a two-pass approach: first summarize each source (reduce volume), then synthesize
- Estimated cost per full run (all 54 SIGs, 1 week): ~$5-15 with Claude Sonnet

---

## 4. CLI Interface

### 4.1 Commands

```bash
# Full report for all SIGs, last 7 days
otel-sig-scraper report --lookback 7d

# Report for specific SIGs
otel-sig-scraper report --lookback 14d --sigs collector,java,specification

# Report filtered to specific topics
otel-sig-scraper report --lookback 7d --topics "OTLP,sampling,semantic conventions"

# Only fetch data (no LLM analysis) — useful for populating cache
otel-sig-scraper fetch --lookback 30d --sigs collector

# List available SIGs
otel-sig-scraper list-sigs

# Generate report from already-fetched data (no network calls to sources)
otel-sig-scraper report --lookback 7d --offline

# Output as JSON (for web UI consumption)
otel-sig-scraper report --lookback 7d --format json --output -

# Authenticate with CNCF Slack (interactive browser login)
otel-sig-scraper slack-login

# Check Slack auth status
otel-sig-scraper slack-status

# Manage custom context injected into LLM prompts
otel-sig-scraper context show                     # Show current custom context
otel-sig-scraper context set --file context.md     # Set from a file
otel-sig-scraper context set --text "Focus on..."  # Set from a string
otel-sig-scraper context clear                     # Remove custom context
```

### 4.2 Flags & Environment Variables

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--lookback` | `OTEL_LOOKBACK` | `7d` | How far back to look (e.g., `7d`, `2w`, `1m`) |
| `--sigs` | `OTEL_SIGS` | (all) | Comma-separated SIG names or categories |
| `--topics` | `OTEL_TOPICS` | (all) | Comma-separated topic filters |
| `--output-dir` | `OTEL_OUTPUT_DIR` | `./reports` | Where to write markdown reports |
| `--format` | `OTEL_FORMAT` | `markdown` | Output format: `markdown`, `json` |
| `--llm-provider` | `OTEL_LLM_PROVIDER` | `anthropic` | LLM provider: `anthropic`, `openai` |
| `--llm-model` | `OTEL_LLM_MODEL` | `claude-sonnet-4-20250514` | Model to use |
| `--anthropic-api-key` | `ANTHROPIC_API_KEY` | — | Anthropic API key |
| `--openai-api-key` | `OPENAI_API_KEY` | — | OpenAI API key |
| `--slack-creds` | `OTEL_SLACK_CREDS` | `~/.config/otel-sig-scraper/slack-credentials.json` | Slack credentials file (auto-populated by `slack-login`) |
| `--context-file` | `OTEL_CONTEXT_FILE` | `~/.config/otel-sig-scraper/custom-context.md` | Custom context injected into LLM prompts |
| `--db-path` | `OTEL_DB_PATH` | `./otel-sig-scraper.db` | SQLite database path |
| `--workers` | `OTEL_WORKERS` | `4` | Concurrent fetch/process workers |
| `--skip-videos` | — | `false` | Skip video transcription |
| `--skip-slack` | — | `false` | Skip Slack fetching |
| `--skip-notes` | — | `false` | Skip Google Docs meeting notes |
| `--offline` | — | `false` | Use only cached data |
| `--verbose` | `OTEL_VERBOSE` | `false` | Verbose logging |
| `--config` | `OTEL_CONFIG` | — | Path to YAML config file |

### 4.3 Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Partial failure (some sources failed, report generated from available data) |
| 2 | Fatal error (no data could be fetched, no report generated) |
| 3 | Configuration error |

---

## 5. Report Format

### 5.1 Per-SIG Report (`reports/2025-06-18-collector-sig.md`)

```markdown
# OTel Collector SIG Report — Jun 11-18, 2025

> Generated: 2025-06-18T14:30:00Z | Sources: meeting notes ✓ video ✓ slack ✓

## Executive Summary
[2-3 sentence overview of the most important developments]

## 🔴 High Relevance to Datadog
### [Topic Title]
- **What**: [description]
- **Why it matters**: [Datadog-specific impact]
- **Action recommended**: [specific next step]
- **Sources**: Meeting 2025-06-15, Slack thread #123

## 🟡 Medium Relevance to Datadog
### [Topic Title]
- **What**: [description]
- **Context**: [broader context]
- **Sources**: ...

## 🟢 Low Relevance / FYI
- [bullet point items]

## Key Decisions Made
- [Decision 1] (Meeting 2025-06-15)
- [Decision 2] (Slack discussion)

## Action Items & Upcoming Work
- [ ] [Action item from meeting]
- [ ] [Planned work discussed]

## Source Links
- Meeting Notes: [link]
- Recording: [link]
- Slack Channel: [link]
```

### 5.2 Digest Report (`reports/2025-06-18-weekly-digest.md`)

```markdown
# OTel Weekly Digest — Jun 11-18, 2025

> Covering: 12 SIGs | Generated: 2025-06-18T14:30:00Z

## 🔴 Top Items for Datadog
1. **[Collector]** New OTLP/HTTP changes... → [details link](#collector)
2. **[Specification]** Sampling config format change... → [details link](#specification)

## SIG-by-SIG Summaries
### Collector
[condensed summary with link to full report]

### Specification: General
[condensed summary with link to full report]
...

## Cross-SIG Themes
- [Theme spanning multiple SIGs]

## Appendix: Processing Stats
| SIG | Notes | Video | Slack | Status |
|-----|-------|-------|-------|--------|
| Collector | ✓ | ✓ | ✓ | Complete |
| Java SDK | ✓ | ✗ (no recording) | ✓ | Partial |
```

---

## 6. SQLite Schema

```sql
-- SIG registry cache
CREATE TABLE sigs (
    id TEXT PRIMARY KEY,              -- normalized slug: "collector", "java-sdk"
    name TEXT NOT NULL,               -- display name: "Collector"
    category TEXT NOT NULL,           -- "specification", "implementation", "cross-cutting", "localization"
    meeting_time TEXT,
    notes_doc_id TEXT,
    slack_channel_id TEXT,
    slack_channel_name TEXT,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Raw meeting notes content
CREATE TABLE meeting_notes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sig_id TEXT NOT NULL REFERENCES sigs(id),
    doc_id TEXT NOT NULL,
    meeting_date DATE NOT NULL,
    raw_text TEXT NOT NULL,
    content_hash TEXT NOT NULL,       -- for dedup
    fetched_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(sig_id, meeting_date)
);

-- Video transcripts
CREATE TABLE video_transcripts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sig_id TEXT NOT NULL REFERENCES sigs(id),
    zoom_url TEXT NOT NULL,
    recording_date DATETIME NOT NULL,
    duration_minutes INTEGER,
    transcript TEXT,
    transcript_source TEXT,           -- "zoom_vtt", "whisper", "manual"
    content_hash TEXT,
    fetched_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(zoom_url)
);

-- Slack messages
CREATE TABLE slack_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sig_id TEXT NOT NULL REFERENCES sigs(id),
    channel_id TEXT NOT NULL,
    message_ts TEXT NOT NULL,         -- Slack message timestamp (unique ID)
    thread_ts TEXT,                   -- parent thread timestamp
    user_id TEXT,
    user_name TEXT,
    text TEXT NOT NULL,
    message_date DATETIME NOT NULL,
    fetched_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(channel_id, message_ts)
);

-- LLM analysis cache
CREATE TABLE analysis_cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cache_key TEXT NOT NULL UNIQUE,   -- hash of (sig_id + date_range + source_type + content_hash)
    sig_id TEXT NOT NULL,
    source_type TEXT NOT NULL,        -- "notes", "video", "slack", "synthesis", "relevance"
    date_range_start DATE NOT NULL,
    date_range_end DATE NOT NULL,
    prompt_hash TEXT NOT NULL,
    result TEXT NOT NULL,             -- LLM output
    model TEXT NOT NULL,
    tokens_used INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Generated reports
CREATE TABLE reports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    report_type TEXT NOT NULL,        -- "sig", "digest"
    sig_id TEXT,
    date_range_start DATE NOT NULL,
    date_range_end DATE NOT NULL,
    file_path TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Fetch log for debugging and monitoring
CREATE TABLE fetch_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_type TEXT NOT NULL,
    sig_id TEXT,
    url TEXT,
    status TEXT NOT NULL,             -- "success", "error", "skipped"
    error_message TEXT,
    duration_ms INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

---

## 7. Project Structure

```
otel-sig-scraper/
├── main.go                          # CLI entrypoint (cobra)
├── go.mod
├── go.sum
├── README.md
├── config.example.yaml
│
├── cmd/
│   ├── root.go                      # Root command, global flags
│   ├── report.go                    # `report` subcommand
│   ├── fetch.go                     # `fetch` subcommand
│   ├── list_sigs.go                 # `list-sigs` subcommand
│   ├── slack_login.go               # `slack-login` subcommand (interactive browser)
│   ├── slack_status.go              # `slack-status` subcommand
│   └── context.go                   # `context` subcommand (show/set/clear)
│
├── internal/
│   ├── config/
│   │   └── config.go                # Config loading (flags + env + YAML)
│   │
│   ├── registry/
│   │   └── registry.go              # SIG registry parser (GitHub README)
│   │
│   ├── sources/
│   │   ├── googledocs.go            # Google Docs meeting notes fetcher
│   │   ├── googlesheets.go          # Google Sheets recording list fetcher  
│   │   ├── zoom.go                  # Zoom transcript extraction (chromedp)
│   │   ├── slack.go                 # Slack channel history fetcher (xoxc token)
│   │   └── slack_auth.go            # Slack browser login + token extraction
│   │
│   ├── store/
│   │   ├── store.go                 # SQLite store interface
│   │   └── migrations.go           # Schema migrations
│   │
│   ├── analysis/
│   │   ├── llm.go                   # LLM client abstraction
│   │   ├── anthropic.go             # Anthropic Claude implementation
│   │   ├── openai.go                # OpenAI implementation
│   │   ├── summarize.go             # Per-source summarization
│   │   ├── synthesize.go            # Cross-source synthesis
│   │   ├── relevance.go             # Datadog relevance scoring
│   │   └── context.go               # Custom context management
│   │
│   ├── report/
│   │   ├── markdown.go              # Markdown report generator
│   │   ├── json.go                  # JSON report generator
│   │   └── templates.go            # Report templates
│   │
│   ├── browser/
│   │   └── browser.go               # Shared chromedp browser pool management
│   │
│   └── pipeline/
│       └── pipeline.go              # Orchestration: fetch → analyze → report
│
├── reports/                          # Default output directory
│   └── .gitkeep
│
└── testdata/                         # Test fixtures
    ├── sample_meeting_notes.txt
    ├── sample_transcript.txt
    └── sample_slack_messages.json
```

---

## 8. Implementation Phases

### Phase 1: Foundation (Core infrastructure)
1. Project scaffolding (Go modules, CLI with cobra, config system)
2. SQLite store with migrations
3. SIG registry parser
4. Basic pipeline orchestration skeleton

### Phase 2: Data Ingestion
5. Google Docs meeting notes fetcher + date parsing
6. Google Sheets CSV fetcher + SIG name matching
7. Slack message fetcher with rate limiting
8. Integration: wire fetchers into pipeline

### Phase 3: LLM Analysis
9. LLM client abstraction (Anthropic + OpenAI)
10. Per-source summarization with prompt templates
11. Cross-source synthesis
12. Datadog relevance scoring
13. Analysis caching in SQLite

### Phase 4: Report Generation
14. Per-SIG markdown report generator
15. Digest/rollup report generator
16. JSON output format (for web UI)

### Phase 5: Video Transcription
17. Zoom share page → headless browser → extract VTT transcript URL from Vue store
18. VTT download and parsing (speaker names + timestamps)
19. Whisper API fallback for recordings without auto-generated transcripts
20. Transcript-based summarization

### Phase 6: Polish
20. Comprehensive error handling and graceful degradation
21. Progress reporting (for CLI and programmatic use)
22. Rate limiting and retry logic
23. Testing with real data
24. Documentation

---

## 9. External Dependencies & API Keys Required

| Dependency | Required? | Purpose | Setup |
|------------|-----------|---------|-------|
| **Anthropic API Key** | Yes (or OpenAI) | LLM analysis | Get from console.anthropic.com |
| **CNCF Slack Account** | Recommended | Slack channel history | Run `otel-sig-scraper slack-login`, authenticate interactively in Chromium. Tool extracts `xoxc-` token + `d` cookie and stores them locally. Re-run when session expires. |
| **Google API Key** | No (for now) | Structured Google Docs access | Public export URLs (`/export?format=txt`) work without auth for all OTel docs. Verified working on Collector SIG notes (300KB doc). Can add Docs API later if needed. |
| **Whisper API / binary** | Optional | Fallback video transcription (most recordings have Zoom auto-transcripts) | OpenAI API or local whisper.cpp |

---

## 10. Go Dependencies

```
modernc.org/sqlite          # Pure-Go SQLite driver
github.com/spf13/cobra      # CLI framework
github.com/spf13/viper      # Configuration
github.com/liushuangls/go-anthropic/v2  # Anthropic API client
github.com/sashabaranov/go-openai       # OpenAI API client (optional fallback)
github.com/slack-go/slack               # Slack API client
github.com/chromedp/chromedp            # Headless Chrome for Zoom transcript extraction
golang.org/x/time/rate                  # Rate limiting
golang.org/x/sync/errgroup              # Concurrent worker pools
```

**System dependency**: Google Chrome or Chromium must be installed for Zoom transcript extraction. chromedp will auto-detect the binary. On headless servers, install `chromium-browser` package.

---

## 11. Name Matching Strategy

The Google Sheets recording names don't exactly match SIG registry names. Strategy:

1. Build a normalized lookup table from the SIG registry
2. Apply fuzzy matching rules:
   - Strip common suffixes: "SIG", "SDK", "Automatic Instrumentation"
   - Normalize: lowercase, remove punctuation
   - Common aliases: `.NET` ↔ `dotnet`, `GoLang` ↔ `Go`, `JavaScript` ↔ `JS`
3. Store confirmed mappings in SQLite for reuse
4. Log unmatched names for manual review

---

## 12. Datadog Relevance Topics (Hardcoded Knowledge)

The system should have a built-in knowledge base of Datadog-relevant areas:

```yaml
high_relevance_keywords:
  - OTLP, OTLP/HTTP, OTLP/gRPC
  - trace context, W3C trace context, baggage
  - sampling, tail sampling, head sampling
  - Datadog exporter, vendor exporters
  - semantic conventions (all: HTTP, DB, messaging, etc.)
  - resource detection, resource attributes
  - metrics SDK, delta vs cumulative temporality
  - log bridge, log SDK
  - collector pipeline, processor, receiver, exporter
  - profiling signal, profile data model
  - OpAMP, agent management
  - context propagation
  - instrumentation libraries
  - configuration file format
  - entities, resource lifecycle

medium_relevance_keywords:
  - SDK lifecycle, provider, tracer, meter, logger
  - batch processing, export retry
  - gRPC instrumentation, HTTP instrumentation
  - Kubernetes operator, auto-instrumentation
  - eBPF instrumentation
  - Prometheus compatibility, remote write
```

### Custom Context System

Users can provide additional context that gets injected into the Datadog relevance scoring prompt. This allows customizing the focus areas without modifying code.

Stored at `~/.config/otel-sig-scraper/custom-context.md` and managed via the `context` subcommand.

Example custom context:
```markdown
## Datadog-Specific Focus Areas

### Immediate Priorities
- We are migrating our trace ingest from dd-proto to OTLP. Any changes to
  OTLP trace format, semantic conventions for spans, or trace context
  propagation are HIGH priority.
- The dd-agent Collector distribution is being updated. Changes to the
  Collector's confmap system, pipeline model, or component lifecycle
  are HIGH priority.

### Product Overlap Areas
- Datadog APM competes with OTel tracing. New auto-instrumentation
  capabilities in Java, Python, .NET, Go, JS are relevant.
- Datadog Metrics competes with OTel metrics. Changes to metric
  temporality, aggregation, or Prometheus compatibility matter.
- Datadog Log Management. Changes to OTel log bridge, log SDK,
  or log-based alerting patterns are relevant.

### Known Contacts
- Yang Song and Jade Guiton (Datadog) attend Collector SIG regularly.
- Pablo Baeyens (Datadog) is GC liaison for System Metrics SemConv.
```

The custom context is appended to the hardcoded relevance keywords when generating the Datadog relevance scoring prompt. It is NOT sent for per-source summarization (to keep those neutral), only for the final relevance scoring pass.

---

## 13. Error Handling & Resilience

- **Graceful degradation**: If one source fails, continue with others. Report notes which sources were unavailable.
- **Retry with backoff**: For transient HTTP/API failures (3 retries, exponential backoff).
- **Rate limiting**: Respect Slack rate limits (50 req/min). Google APIs have generous limits.
- **Timeout**: Per-source fetch timeout of 60s. Per-LLM-call timeout of 120s.
- **Deduplication**: Content hashing prevents re-processing identical content.
- **Partial reports**: Always generate a report with whatever data is available, clearly marking gaps.

---

## 14. Configuration File Example

```yaml
# config.yaml
lookback: 7d
output_dir: ./reports
format: markdown
workers: 4

llm:
  provider: anthropic
  model: claude-sonnet-4-20250514

# Optional: restrict to specific SIGs
sigs:
  - collector
  - specification
  - java-sdk
  - python-sdk
  - semantic-conventions

# Optional: focus topics
topics:
  - OTLP
  - sampling
  - semantic conventions

# Custom context is managed separately via:
#   otel-sig-scraper context set --file my-context.md
# Stored at: ~/.config/otel-sig-scraper/custom-context.md
# Injected into the Datadog relevance scoring prompt only.
```

---

## 15. Decisions Made

| Question | Decision |
|----------|----------|
| LLM Provider | **Anthropic Claude** (keys available). Support OpenAI as future alternative. |
| Slack Access | **No bot token available**. Use OAuth User Token (`xoxp-`) approach — create Slack App, run user OAuth flow on CNCF Slack. Fallback to browser cookie token (`xoxc-`). |
| Video Transcription | **Critical for v1**. Zoom auto-generates WebVTT transcripts with speaker names. Verified: publicly accessible, downloadable via headless browser + HTTP GET. |
| Google API | **Use public export URLs** (`/export?format=txt`). No Google API key needed. Verified: works on all OTel docs. |
| Target Audience | Engineers and PMs familiar with OTel and Datadog products. Reports should be technically detailed. |
| Priority SIGs | Collector, Specification, Semantic Conventions, Java/Python/.NET/Go/JS SDKs, Profiling, OpAMP |
| Report Frequency | **Ad hoc** (CLI invocation). No assumptions about scheduling. |
| Zoom Links | **Verified publicly accessible**. No auth wall, no password. Vue SPA loads recording player with transcript access. |
| Cost Budget | ~$5-15/run is acceptable. Can go higher if needed. |
| Custom Context | Provided via `context` subcommand, stored at `~/.config/otel-sig-scraper/custom-context.md`. Injected into relevance scoring prompt only. |
| Chromium | Can be installed on target machines. Used for both Zoom transcripts and Slack auth. |
| LLM Model | Claude Sonnet (default). Configurable via `--llm-model`. |

## 16. Remaining Questions

None blocking. Ready to build.


---

## Appendix A: Verified Data Source Details

### A.1 SIG Registry (54 SIGs parsed from README)

Categories: 19 Specification SIGs, 26 Implementation SIGs, 6 Cross-Cutting SIGs, 4 Localization Teams (skippable).

Every SIG has: name, Google Doc ID, Slack channel ID, Slack channel name, meeting time.

Parsing is straightforward: pipe-delimited markdown tables under section headers.

### A.2 Google Docs Meeting Notes (verified)

- **URL pattern**: `https://docs.google.com/document/d/{DOC_ID}/export?format=txt`
- **Auth**: None needed (public docs)
- **Format**: Plain text with date headings, attendee lists, agenda items, discussion notes
- **Size**: Collector SIG doc = 300KB (years of history). Date parsing needed to extract relevant window.
- **Date formats seen**: `Feb 18, 2026`, `2026-02-18`, various formats → need flexible date parser
- **Structure**: Most recent notes appear at the top of the document

### A.3 Google Sheets Recordings (verified)

- **URL**: `https://docs.google.com/spreadsheets/d/1SYKfjYhZdm2Wh2Cl6KVQalKg_m4NhTPZqq-8SzEVO6s/export?format=csv`
- **Auth**: None needed (public sheet)
- **Columns**: Name, Start time, Duration (minutes), URL
- **Rows**: ~1,528 recordings
- **Date format**: `YYYY-MM-DD H:MM:SS`
- **Name examples**: `Collector SIG`, `Specification SIG`, `.NET SIG`, `JavaScript SIG`, `Go SIG`
- **CSV saved at**: `/home/exedev/otel_meetings.csv`

### A.4 Zoom Transcripts (verified)

- **Share links**: All publicly accessible, no auth
- **Transcript availability**: Most recordings (10+ min) have auto-generated WebVTT transcripts
- **VTT format**: Standard WebVTT with speaker names and timestamps
- **Extraction method**: Headless browser (chromedp) → Vue store → `transcriptUrl` → HTTP GET
- **Vue store path**: `document.querySelector('#app').__vue__.$store.state`
- **Key fields**: `hasTranscript`, `transcriptUrl`, `playCheckId`, `topic`, `duration`
- **VTT URL pattern**: `https://zoom.us/rec/play/vtt?type=transcript&fid={playCheckId}&action=play`
- **VTT size**: ~50-60KB for a 60-min meeting (~500 cue entries)
- **Performance**: Page load ~3-5s per recording, VTT download instant

### A.5 Slack Channels (access research complete)

- **Workspace**: `cloud-native.slack.com` (CNCF Slack)
- **No public archives** exist
- **Bot token**: Not obtainable (requires CNCF admin approval)
- **Slack App (`xoxp-`)**: Likely requires CNCF admin approval to install
- **Browser session token (`xoxc-` + `d` cookie)**: **CHOSEN APPROACH**
  - User authenticates via interactive Chromium browser (`slack-login` command)
  - Login page supports: Google OAuth, Apple, or email magic code
  - After login, extract `xoxc-` token from page JS (`boot_data.api_token` or equivalent)
  - Extract `d` cookie from browser cookie jar
  - Both are needed for API calls: `Authorization: Bearer xoxc-...` header + `d=xoxd-...` cookie
  - Token lifetime: days to weeks (session-scoped)
  - Works with standard Slack Web API: `conversations.history`, `conversations.replies`, `auth.test`
  - Rate limit: Tier 3 (~50 req/min)
  - Inherits user's permissions: read access to all public channels (no need to join them)

---

## Appendix B: Name Matching Table (Sheet → SIG Registry)

Known mappings from the Google Sheets `Name` column to SIG registry names:

| Sheet Name | SIG Registry Name |
|-----------|-------------------|
| `Collector SIG` | `Collector` |
| `Specification SIG` | `Specification: General + OTel Maintainers Sync` |
| `.NET SIG` | `.NET: SDK` |
| `Go SIG` | `GoLang: SDK` |
| `JavaScript SIG` | `JavaScript: SDK` |
| `Java SIG` | `Java: SDK + Instrumentation` |
| `Python SIG` | `Python: SDK` |
| `Ruby SIG` | `Ruby: SDK` |
| `Rust SIG` | `Rust: SDK` |
| `PHP SIG` | `PHP: SDK` |
| `C++ SIG` | `C++: SDK` |
| `Erlang/Elixir SIG` | `Erlang/Elixir: SDK` |
| `Swift SIG` | `Swift: SDK` |
| `Semantic Convention SIG` | `Semantic Conventions: General` |
| `Browser SIG` | `Browser` |
| `Android SIG` | `Android: SDK + Automatic Instrumentation` |
| `eBPF instrumentation` | `eBPF Instrumentation` |
| `Arrow SIG` | `Arrow` |

Strategy: normalize both sides (lowercase, strip "SIG"/"SDK"/"SIG MTG", fuzzy match), store confirmed mappings in SQLite.
