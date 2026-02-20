package pipeline

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/gordyrad/otel-sig-tracker/internal/config"
	"github.com/gordyrad/otel-sig-tracker/internal/sources"
	"github.com/gordyrad/otel-sig-tracker/internal/store"
)

func TestPartialError_Error(t *testing.T) {
	tests := []struct {
		name     string
		errors   []error
		wantMsg  string
	}{
		{
			name:    "single error",
			errors:  []error{fmt.Errorf("failed to fetch docs")},
			wantMsg: "partial failure: 1 source(s) failed",
		},
		{
			name:    "multiple errors",
			errors:  []error{fmt.Errorf("err1"), fmt.Errorf("err2"), fmt.Errorf("err3")},
			wantMsg: "partial failure: 3 source(s) failed",
		},
		{
			name:    "no errors",
			errors:  nil,
			wantMsg: "partial failure: 0 source(s) failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pe := &PartialError{Errors: tt.errors}
			got := pe.Error()
			if got != tt.wantMsg {
				t.Errorf("PartialError.Error() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestPartialError_ImplementsError(t *testing.T) {
	var _ error = &PartialError{}
}

func TestNewPipeline_WithAnthropicKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DBPath = filepath.Join(t.TempDir(), "test.db")
	cfg.LLM.Provider = "anthropic"
	cfg.LLM.AnthropicKey = "test-key"
	cfg.SkipSlack = true

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating pipeline: %v", err)
	}
	defer p.Close()

	if p.cfg != cfg {
		t.Error("pipeline config should match the provided config")
	}
	if p.store == nil {
		t.Error("pipeline store should not be nil")
	}
	if p.llm == nil {
		t.Error("pipeline LLM client should not be nil")
	}
	if p.summarizer == nil {
		t.Error("pipeline summarizer should not be nil")
	}
	if p.synthesizer == nil {
		t.Error("pipeline synthesizer should not be nil")
	}
	if p.scorer == nil {
		t.Error("pipeline scorer should not be nil")
	}
	if p.mdGenerator == nil {
		t.Error("pipeline markdown generator should not be nil")
	}
	if p.jsonGenerator == nil {
		t.Error("pipeline JSON generator should not be nil")
	}
}

func TestNewPipeline_WithOpenAIKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DBPath = filepath.Join(t.TempDir(), "test.db")
	cfg.LLM.Provider = "openai"
	cfg.LLM.OpenAIKey = "test-key"
	cfg.SkipSlack = true

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error creating pipeline: %v", err)
	}
	defer p.Close()

	if p.llm == nil {
		t.Error("pipeline LLM client should not be nil for openai provider")
	}
}

func TestNewPipeline_UnsupportedProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DBPath = filepath.Join(t.TempDir(), "test.db")
	cfg.LLM.Provider = "unsupported"

	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for unsupported LLM provider")
	}
	want := "unsupported LLM provider: unsupported"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestNewPipeline_InvalidDBPath(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DBPath = "/nonexistent/deep/path/that/doesnt/exist/test.db"
	cfg.LLM.Provider = "anthropic"
	cfg.LLM.AnthropicKey = "test-key"

	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for invalid DB path")
	}
}

func TestPipeline_Close(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DBPath = filepath.Join(t.TempDir(), "test.db")
	cfg.LLM.Provider = "anthropic"
	cfg.LLM.AnthropicKey = "test-key"
	cfg.SkipSlack = true

	p, err := New(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Close should not return an error.
	if err := p.Close(); err != nil {
		t.Errorf("Close() returned unexpected error: %v", err)
	}
}

func TestPipeline_CloseNilStore(t *testing.T) {
	p := &Pipeline{}
	// Close on a pipeline with nil store should be safe.
	if err := p.Close(); err != nil {
		t.Errorf("Close() on nil store returned unexpected error: %v", err)
	}
}

func TestFilterSIGs_NoFilter(t *testing.T) {
	sigs := []*store.SIG{
		{ID: "sig-a", Name: "SIG A"},
		{ID: "sig-b", Name: "SIG B"},
		{ID: "sig-c", Name: "SIG C"},
	}

	result := filterSIGs(sigs, nil)
	if len(result) != 3 {
		t.Errorf("filterSIGs with nil filter: got %d SIGs, want 3", len(result))
	}

	result = filterSIGs(sigs, []string{})
	if len(result) != 3 {
		t.Errorf("filterSIGs with empty filter: got %d SIGs, want 3", len(result))
	}
}

func TestFilterSIGs_WithFilter(t *testing.T) {
	sigs := []*store.SIG{
		{ID: "sig-a", Name: "SIG A"},
		{ID: "sig-b", Name: "SIG B"},
		{ID: "sig-c", Name: "SIG C"},
	}

	result := filterSIGs(sigs, []string{"sig-a", "sig-c"})
	if len(result) != 2 {
		t.Errorf("filterSIGs: got %d SIGs, want 2", len(result))
	}
	if result[0].ID != "sig-a" {
		t.Errorf("first filtered SIG = %q, want %q", result[0].ID, "sig-a")
	}
	if result[1].ID != "sig-c" {
		t.Errorf("second filtered SIG = %q, want %q", result[1].ID, "sig-c")
	}
}

func TestFilterSIGs_FilterNormalizesIDs(t *testing.T) {
	sigs := []*store.SIG{
		{ID: "sig-collector", Name: "SIG Collector"},
		{ID: "sig-java", Name: "SIG Java"},
	}

	// Pass a filter with uppercase and spaces -- NormalizeSIGID should lowercase and hyphenate.
	result := filterSIGs(sigs, []string{"SIG Collector"})
	if len(result) != 1 {
		t.Errorf("filterSIGs with normalized filter: got %d SIGs, want 1", len(result))
	}
	if len(result) > 0 && result[0].ID != "sig-collector" {
		t.Errorf("filtered SIG ID = %q, want %q", result[0].ID, "sig-collector")
	}
}

func TestFilterSIGs_NoMatch(t *testing.T) {
	sigs := []*store.SIG{
		{ID: "sig-a", Name: "SIG A"},
	}

	result := filterSIGs(sigs, []string{"sig-nonexistent"})
	if len(result) != 0 {
		t.Errorf("filterSIGs with non-matching filter: got %d SIGs, want 0", len(result))
	}
}

func TestFilterSIGs_EmptyInput(t *testing.T) {
	result := filterSIGs(nil, []string{"sig-a"})
	if len(result) != 0 {
		t.Errorf("filterSIGs with nil sigs: got %d, want 0", len(result))
	}
}

func TestSigIDList(t *testing.T) {
	sigs := []*store.SIG{
		{ID: "sig-a"},
		{ID: "sig-b"},
		{ID: "sig-c"},
	}

	ids := sigIDList(sigs)
	if len(ids) != 3 {
		t.Fatalf("sigIDList: got %d IDs, want 3", len(ids))
	}
	for i, want := range []string{"sig-a", "sig-b", "sig-c"} {
		if ids[i] != want {
			t.Errorf("sigIDList[%d] = %q, want %q", i, ids[i], want)
		}
	}
}

func TestSigIDList_Empty(t *testing.T) {
	ids := sigIDList(nil)
	if len(ids) != 0 {
		t.Errorf("sigIDList(nil): got %d IDs, want 0", len(ids))
	}
}

func TestFilterRecordingsForSIG(t *testing.T) {
	recordings := []*sources.Recording{
		{SIGID: "sig-a", ZoomURL: "https://zoom.us/1"},
		{SIGID: "sig-b", ZoomURL: "https://zoom.us/2"},
		{SIGID: "sig-a", ZoomURL: "https://zoom.us/3"},
		{SIGID: "sig-c", ZoomURL: "https://zoom.us/4"},
	}

	result := filterRecordingsForSIG(recordings, "sig-a")
	if len(result) != 2 {
		t.Fatalf("filterRecordingsForSIG(sig-a): got %d, want 2", len(result))
	}
	if result[0].ZoomURL != "https://zoom.us/1" {
		t.Errorf("first recording URL = %q, want %q", result[0].ZoomURL, "https://zoom.us/1")
	}
	if result[1].ZoomURL != "https://zoom.us/3" {
		t.Errorf("second recording URL = %q, want %q", result[1].ZoomURL, "https://zoom.us/3")
	}
}

func TestFilterRecordingsForSIG_NoMatch(t *testing.T) {
	recordings := []*sources.Recording{
		{SIGID: "sig-a", ZoomURL: "https://zoom.us/1"},
	}

	result := filterRecordingsForSIG(recordings, "sig-nonexistent")
	if len(result) != 0 {
		t.Errorf("filterRecordingsForSIG(nonexistent): got %d, want 0", len(result))
	}
}

func TestFilterRecordingsForSIG_NilInput(t *testing.T) {
	result := filterRecordingsForSIG(nil, "sig-a")
	if len(result) != 0 {
		t.Errorf("filterRecordingsForSIG(nil): got %d, want 0", len(result))
	}
}

func TestFilterSIGs_ExcludesLocalization(t *testing.T) {
	sigs := []*store.SIG{
		{ID: "collector", Name: "Collector", Category: "implementation"},
		{ID: "chinese", Name: "Chinese", Category: "localization"},
		{ID: "japanese", Name: "Japanese", Category: "localization"},
		{ID: "specification", Name: "Specification", Category: "specification"},
	}

	// With no filter, localization SIGs should be excluded.
	result := filterSIGs(sigs, nil)
	if len(result) != 2 {
		t.Fatalf("filterSIGs excluding localization: got %d SIGs, want 2", len(result))
	}
	for _, sig := range result {
		if sig.Category == "localization" {
			t.Errorf("localization SIG %q should have been filtered out", sig.ID)
		}
	}
}

func TestDeduplicateSIGs(t *testing.T) {
	sigs := []*store.SIG{
		{ID: "collector", Name: "Collector"},
		{ID: "specification", Name: "Specification"},
		{ID: "collector", Name: "Collector (old)"},
		{ID: "specification", Name: "Specification (old)"},
		{ID: "java", Name: "Java"},
	}

	result := deduplicateSIGs(sigs)
	if len(result) != 3 {
		t.Fatalf("deduplicateSIGs: got %d SIGs, want 3", len(result))
	}
	// Verify first occurrence is kept.
	if result[0].Name != "Collector" {
		t.Errorf("first dedup result name = %q, want %q", result[0].Name, "Collector")
	}
	if result[1].Name != "Specification" {
		t.Errorf("second dedup result name = %q, want %q", result[1].Name, "Specification")
	}
	if result[2].Name != "Java" {
		t.Errorf("third dedup result name = %q, want %q", result[2].Name, "Java")
	}
}

func TestDeduplicateSIGs_NoDuplicates(t *testing.T) {
	sigs := []*store.SIG{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
	}

	result := deduplicateSIGs(sigs)
	if len(result) != 2 {
		t.Errorf("deduplicateSIGs with no dupes: got %d, want 2", len(result))
	}
}

func TestDeduplicateSIGs_Empty(t *testing.T) {
	result := deduplicateSIGs(nil)
	if len(result) != 0 {
		t.Errorf("deduplicateSIGs(nil): got %d, want 0", len(result))
	}
}
