package pipeline

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/gordyrad/otel-sig-tracker/internal/analysis"
	"github.com/gordyrad/otel-sig-tracker/internal/config"
	"github.com/gordyrad/otel-sig-tracker/internal/registry"
	"github.com/gordyrad/otel-sig-tracker/internal/report"
	"github.com/gordyrad/otel-sig-tracker/internal/sources"
	"github.com/gordyrad/otel-sig-tracker/internal/store"
)

// PartialError indicates that some sources failed but others succeeded.
// The pipeline was still able to produce output from available data.
type PartialError struct {
	Errors []error
}

func (e *PartialError) Error() string {
	return fmt.Sprintf("partial failure: %d source(s) failed", len(e.Errors))
}

// Pipeline orchestrates the full fetch -> analyze -> report workflow.
type Pipeline struct {
	cfg           *config.Config
	store         *store.Store
	llm           analysis.LLMClient
	registry      *registry.Fetcher
	docsFetcher   *sources.GoogleDocsFetcher
	sheetsFetcher *sources.GoogleSheetsFetcher
	zoomFetcher   *sources.ZoomFetcher
	slackFetcher  *sources.SlackFetcher
	summarizer    *analysis.Summarizer
	synthesizer   *analysis.Synthesizer
	scorer        *analysis.RelevanceScorer
	mdGenerator   *report.MarkdownGenerator
	jsonGenerator *report.JSONGenerator
}

// New initializes all components and returns a ready-to-run Pipeline.
func New(cfg *config.Config) (*Pipeline, error) {
	// Open the SQLite store.
	s, err := store.New(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}

	// Create LLM client based on config.
	var llm analysis.LLMClient
	switch cfg.LLM.Provider {
	case "anthropic":
		llm = analysis.NewAnthropicClient(cfg.LLM.AnthropicKey, cfg.LLM.Model)
	case "openai":
		llm = analysis.NewOpenAIClient(cfg.LLM.OpenAIKey, cfg.LLM.Model)
	default:
		s.Close()
		return nil, fmt.Errorf("unsupported LLM provider: %s", cfg.LLM.Provider)
	}

	// Load custom context for relevance scoring.
	customContext, err := analysis.LoadCustomContext(cfg.ContextFile)
	if err != nil {
		s.Close()
		return nil, fmt.Errorf("loading custom context: %w", err)
	}

	// Create fetchers.
	docsFetcher := sources.NewGoogleDocsFetcher(s)
	sheetsFetcher := sources.NewGoogleSheetsFetcher()
	zoomFetcher := sources.NewZoomFetcher(s)

	// Load Slack credentials and create Slack fetcher if available.
	var slackFetcher *sources.SlackFetcher
	if !cfg.SkipSlack {
		creds, err := sources.LoadSlackCredentials(cfg.Slack.CredentialsFile)
		if err != nil {
			log.Printf("warning: could not load slack credentials: %v", err)
		}
		if creds != nil {
			slackFetcher = sources.NewSlackFetcher(s, creds.Token, creds.Cookie)
		} else {
			log.Printf("warning: no slack credentials found, slack fetching will be skipped")
		}
	}

	// Create analysis components.
	summarizer := analysis.NewSummarizer(llm, s)
	synthesizer := analysis.NewSynthesizer(llm, s)
	scorer := analysis.NewRelevanceScorer(llm, s, customContext)

	// Create report generators.
	mdGenerator := report.NewMarkdownGenerator(cfg.OutputDir)
	jsonGenerator := report.NewJSONGenerator(cfg.OutputDir)

	return &Pipeline{
		cfg:           cfg,
		store:         s,
		llm:           llm,
		registry:      registry.NewFetcher(),
		docsFetcher:   docsFetcher,
		sheetsFetcher: sheetsFetcher,
		zoomFetcher:   zoomFetcher,
		slackFetcher:  slackFetcher,
		summarizer:    summarizer,
		synthesizer:   synthesizer,
		scorer:        scorer,
		mdGenerator:   mdGenerator,
		jsonGenerator: jsonGenerator,
	}, nil
}

// Close releases all resources held by the pipeline.
func (p *Pipeline) Close() error {
	if p.store != nil {
		return p.store.Close()
	}
	return nil
}

// Run executes the full pipeline: fetch sources, analyze, and generate reports.
func (p *Pipeline) Run(ctx context.Context) error {
	log.Println("pipeline: starting full run")

	if err := p.FetchOnly(ctx); err != nil {
		return fmt.Errorf("fetch phase: %w", err)
	}

	if err := p.AnalyzeOnly(ctx); err != nil {
		return fmt.Errorf("analyze phase: %w", err)
	}

	log.Println("pipeline: run complete")
	return nil
}

// FetchOnly executes only the data-fetching phase of the pipeline.
func (p *Pipeline) FetchOnly(ctx context.Context) error {
	log.Println("pipeline: starting fetch phase")

	end := time.Now()
	start := end.Add(-p.cfg.Lookback)
	log.Printf("pipeline: date range %s to %s",
		start.Format("2006-01-02"), end.Format("2006-01-02"))

	// Step 1: Fetch and update the SIG registry.
	sigs, err := p.registry.FetchAndParse()
	if err != nil {
		return fmt.Errorf("fetching SIG registry: %w", err)
	}
	for _, sig := range sigs {
		if err := p.store.UpsertSIG(sig); err != nil {
			log.Printf("warning: failed to upsert SIG %s: %v", sig.ID, err)
		}
	}
	log.Printf("pipeline: loaded %d SIGs from registry", len(sigs))

	// Step 2: Filter SIGs based on config.
	filteredSIGs := filterSIGs(sigs, p.cfg.SIGs)
	log.Printf("pipeline: processing %d SIGs after filtering", len(filteredSIGs))

	// Step 3: Fetch recordings list (needed for video transcripts).
	var recordings []*sources.Recording
	if !p.cfg.SkipVideos {
		sigIDs := sigIDList(filteredSIGs)
		recordings, err = p.sheetsFetcher.FetchRecordings(ctx, start, end, sigIDs)
		if err != nil {
			log.Printf("warning: failed to fetch recordings list: %v", err)
		} else {
			log.Printf("pipeline: found %d recordings", len(recordings))
		}
	}

	// Step 4: Fetch all sources concurrently per SIG.
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(p.cfg.Workers)

	for _, sig := range filteredSIGs {
		sig := sig // capture loop variable
		g.Go(func() error {
			return p.fetchSIG(gctx, sig, start, end, recordings)
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("fetching SIG sources: %w", err)
	}

	log.Println("pipeline: fetch phase complete")
	return nil
}

// AnalyzeOnly executes only the analysis and report generation phase,
// using data already cached in the store.
func (p *Pipeline) AnalyzeOnly(ctx context.Context) error {
	log.Println("pipeline: starting analysis phase")
	execStart := time.Now()

	end := time.Now()
	start := end.Add(-p.cfg.Lookback)
	startStr := start.Format("2006-01-02")
	endStr := end.Format("2006-01-02")

	// Load SIGs from the store.
	sigs, err := p.store.ListSIGs(p.cfg.SIGs)
	if err != nil {
		return fmt.Errorf("listing SIGs from store: %w", err)
	}
	if len(sigs) == 0 {
		return fmt.Errorf("no SIGs found in store (run fetch first)")
	}

	// Deduplicate SIGs by ID (stale DB entries may produce duplicates).
	sigs = deduplicateSIGs(sigs)

	// Apply the same localization filter used during fetch.
	sigs = filterSIGs(sigs, p.cfg.SIGs)

	log.Printf("pipeline: analyzing %d SIGs", len(sigs))

	// Analyze each SIG concurrently.
	var mu sync.Mutex
	var sigReports []*analysis.SIGReport

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(p.cfg.Workers)

	for _, sig := range sigs {
		sig := sig
		g.Go(func() error {
			sr, err := p.analyzeSIG(gctx, sig, start, end, startStr, endStr)
			if err != nil {
				log.Printf("warning: analysis failed for SIG %s: %v", sig.ID, err)
				// Build a partial report even on failure.
				sr = &analysis.SIGReport{
					SIGID:          sig.ID,
					SIGName:        sig.Name,
					Category:       sig.Category,
					DateRangeStart: startStr,
					DateRangeEnd:   endStr,
					SourcesMissing: []string{"notes", "video", "slack"},
				}
			}

			mu.Lock()
			sigReports = append(sigReports, sr)
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("analyzing SIGs: %w", err)
	}

	// Compute run stats.
	runDuration := time.Since(execStart)
	totalTokens := 0
	totalCalls := 0
	sigsWithData := 0
	for _, sr := range sigReports {
		if sr.RelevanceReport != nil {
			totalTokens += sr.RelevanceReport.TokensUsed
			totalCalls++ // relevance call
			sigsWithData++
		}
	}
	// Rough estimate: each SIG with data has ~3 summarize + 1 synthesize + 1 relevance = 5 calls.
	totalCalls = sigsWithData * 5

	costPerMillionTokens := 3.0 // default Sonnet pricing
	if p.cfg.LLM.Provider == "openai" {
		costPerMillionTokens = 3.0
	}
	estimatedCost := float64(totalTokens) / 1_000_000 * costPerMillionTokens

	stats := &analysis.RunStats{
		TotalTokensUsed:  totalTokens,
		TotalLLMCalls:    totalCalls,
		Model:            p.cfg.LLM.Model,
		Provider:         p.cfg.LLM.Provider,
		SIGsProcessed:    len(sigReports),
		SIGsWithData:     sigsWithData,
		DurationSeconds:  runDuration.Seconds(),
		EstimatedCostUSD: estimatedCost,
	}

	// Generate digest report (the only output file).
	digest := &analysis.DigestReport{
		DateRangeStart: startStr,
		DateRangeEnd:   endStr,
		SIGReports:     sigReports,
		Stats:          stats,
	}

	if err := p.generateDigestReport(digest); err != nil {
		log.Printf("warning: failed to generate digest report: %v", err)
	}

	log.Println("pipeline: analysis phase complete")
	return nil
}

// fetchSIG fetches all available sources for a single SIG.
func (p *Pipeline) fetchSIG(ctx context.Context, sig *store.SIG, start, end time.Time, recordings []*sources.Recording) error {
	log.Printf("pipeline: fetching sources for SIG %s", sig.ID)

	// Fetch meeting notes.
	if !p.cfg.SkipNotes && sig.NotesDocID != "" {
		if err := p.docsFetcher.FetchMeetingNotes(ctx, sig, start, end); err != nil {
			log.Printf("warning: failed to fetch meeting notes for %s: %v", sig.ID, err)
		}
	}

	// Fetch video transcripts.
	if !p.cfg.SkipVideos {
		sigRecordings := filterRecordingsForSIG(recordings, sig.ID)
		for _, rec := range sigRecordings {
			if err := p.zoomFetcher.FetchTranscript(ctx, rec); err != nil {
				log.Printf("warning: failed to fetch transcript for %s (%s): %v",
					sig.ID, rec.ZoomURL, err)
			}
		}
	}

	// Fetch Slack messages.
	if !p.cfg.SkipSlack && p.slackFetcher != nil && sig.SlackChannelID != "" {
		if err := p.slackFetcher.FetchMessages(ctx, sig, start, end); err != nil {
			log.Printf("warning: failed to fetch slack messages for %s: %v", sig.ID, err)
		}
	}

	return nil
}

// analyzeSIG runs the full analysis pipeline for a single SIG:
// summarize each source, synthesize across sources, score for relevance.
func (p *Pipeline) analyzeSIG(ctx context.Context, sig *store.SIG, start, end time.Time, startStr, endStr string) (*analysis.SIGReport, error) {
	log.Printf("pipeline: analyzing SIG %s", sig.ID)

	var sourcesUsed []string
	var sourcesMissing []string
	var summaries []*analysis.SourceSummary

	// Summarize meeting notes.
	notes, err := p.store.GetMeetingNotes(sig.ID, start, end)
	if err != nil {
		log.Printf("warning: failed to get meeting notes for %s: %v", sig.ID, err)
	}
	if len(notes) > 0 {
		summary, err := p.summarizer.SummarizeMeetingNotes(ctx, sig.ID, sig.Name, notes, start, end)
		if err != nil {
			log.Printf("warning: failed to summarize meeting notes for %s: %v", sig.ID, err)
			sourcesMissing = append(sourcesMissing, "notes")
		} else {
			summaries = append(summaries, summary)
			sourcesUsed = append(sourcesUsed, "notes")
		}
	} else {
		sourcesMissing = append(sourcesMissing, "notes")
	}

	// Summarize video transcripts.
	transcripts, err := p.store.GetVideoTranscripts(sig.ID, start, end)
	if err != nil {
		log.Printf("warning: failed to get video transcripts for %s: %v", sig.ID, err)
	}
	if len(transcripts) > 0 {
		summary, err := p.summarizer.SummarizeVideoTranscripts(ctx, sig.ID, sig.Name, transcripts, start, end)
		if err != nil {
			log.Printf("warning: failed to summarize video transcripts for %s: %v", sig.ID, err)
			sourcesMissing = append(sourcesMissing, "video")
		} else {
			summaries = append(summaries, summary)
			sourcesUsed = append(sourcesUsed, "video")
		}
	} else {
		sourcesMissing = append(sourcesMissing, "video")
	}

	// Summarize Slack messages.
	messages, err := p.store.GetSlackMessages(sig.ID, start, end)
	if err != nil {
		log.Printf("warning: failed to get slack messages for %s: %v", sig.ID, err)
	}
	if len(messages) > 0 {
		summary, err := p.summarizer.SummarizeSlackMessages(ctx, sig.ID, sig.Name, messages, start, end)
		if err != nil {
			log.Printf("warning: failed to summarize slack messages for %s: %v", sig.ID, err)
			sourcesMissing = append(sourcesMissing, "slack")
		} else {
			summaries = append(summaries, summary)
			sourcesUsed = append(sourcesUsed, "slack")
		}
	} else {
		sourcesMissing = append(sourcesMissing, "slack")
	}

	// Build the SIG report.
	sr := &analysis.SIGReport{
		SIGID:          sig.ID,
		SIGName:        sig.Name,
		Category:       sig.Category,
		DateRangeStart: startStr,
		DateRangeEnd:   endStr,
		SourcesUsed:    sourcesUsed,
		SourcesMissing: sourcesMissing,
		SlackChannel:   sig.SlackChannelName,
	}

	if sig.NotesDocID != "" {
		sr.NotesLink = fmt.Sprintf("https://docs.google.com/document/d/%s", sig.NotesDocID)
	}

	// If we have no summaries, return the partial report.
	if len(summaries) == 0 {
		log.Printf("pipeline: no source data available for SIG %s, skipping analysis", sig.ID)
		return sr, nil
	}

	// Synthesize across sources.
	synthesis, err := p.synthesizer.Synthesize(ctx, sig.ID, sig.Name, summaries, start, end)
	if err != nil {
		return sr, fmt.Errorf("synthesizing SIG %s: %w", sig.ID, err)
	}

	// Score for Datadog relevance.
	relevance, err := p.scorer.Score(ctx, sig.ID, sig.Name, synthesis, start, end)
	if err != nil {
		return sr, fmt.Errorf("scoring relevance for SIG %s: %w", sig.ID, err)
	}
	sr.RelevanceReport = relevance

	log.Printf("pipeline: analysis complete for SIG %s (sources: %v)", sig.ID, sourcesUsed)
	return sr, nil
}

// generateDigestReport writes the weekly digest in the configured format.
func (p *Pipeline) generateDigestReport(digest *analysis.DigestReport) error {
	switch p.cfg.Format {
	case "markdown":
		path, err := p.mdGenerator.GenerateDigestReport(digest)
		if err != nil {
			return err
		}
		log.Printf("pipeline: wrote markdown digest %s", path)
	case "json":
		path, err := p.jsonGenerator.GenerateDigestReport(digest)
		if err != nil {
			return err
		}
		log.Printf("pipeline: wrote JSON digest %s", path)
	default:
		if path, err := p.mdGenerator.GenerateDigestReport(digest); err != nil {
			log.Printf("warning: failed to write markdown digest: %v", err)
		} else {
			log.Printf("pipeline: wrote markdown digest %s", path)
		}
		if path, err := p.jsonGenerator.GenerateDigestReport(digest); err != nil {
			log.Printf("warning: failed to write JSON digest: %v", err)
		} else {
			log.Printf("pipeline: wrote JSON digest %s", path)
		}
	}
	return nil
}

// filterSIGs returns only the SIGs whose IDs match the provided filter list.
// If the filter list is empty, all non-localization SIGs are returned.
// Localization teams (language translation SIGs) are always excluded unless
// explicitly requested by name.
func filterSIGs(sigs []*store.SIG, filterIDs []string) []*store.SIG {
	if len(filterIDs) == 0 {
		// Return all SIGs except localization teams.
		var filtered []*store.SIG
		for _, sig := range sigs {
			if sig.Category != "localization" {
				filtered = append(filtered, sig)
			}
		}
		return filtered
	}

	idSet := make(map[string]bool, len(filterIDs))
	for _, id := range filterIDs {
		idSet[registry.NormalizeSIGID(id)] = true
	}

	var filtered []*store.SIG
	for _, sig := range sigs {
		if idSet[sig.ID] {
			filtered = append(filtered, sig)
		}
	}
	return filtered
}

// sigIDList extracts IDs from a slice of SIGs.
func sigIDList(sigs []*store.SIG) []string {
	ids := make([]string, len(sigs))
	for i, sig := range sigs {
		ids[i] = sig.ID
	}
	return ids
}

// deduplicateSIGs removes duplicate SIGs by ID, keeping the first occurrence.
func deduplicateSIGs(sigs []*store.SIG) []*store.SIG {
	seen := make(map[string]bool, len(sigs))
	var unique []*store.SIG
	for _, sig := range sigs {
		if !seen[sig.ID] {
			seen[sig.ID] = true
			unique = append(unique, sig)
		}
	}
	return unique
}

// filterRecordingsForSIG returns recordings that match the given SIG ID.
func filterRecordingsForSIG(recordings []*sources.Recording, sigID string) []*sources.Recording {
	var filtered []*sources.Recording
	for _, r := range recordings {
		if r.SIGID == sigID {
			filtered = append(filtered, r)
		}
	}
	return filtered
}
