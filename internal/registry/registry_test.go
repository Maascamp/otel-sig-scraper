package registry

import (
	"os"
	"testing"
)

func TestParse(t *testing.T) {
	content, err := os.ReadFile("../../testdata/sample_registry.md")
	if err != nil {
		t.Fatalf("reading test fixture: %v", err)
	}

	sigs, err := Parse(string(content))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(sigs) == 0 {
		t.Fatal("Parse returned no SIGs")
	}

	// Check we got SIGs from multiple categories
	categories := make(map[string]int)
	for _, sig := range sigs {
		categories[sig.Category]++
	}

	if categories["specification"] == 0 {
		t.Error("expected specification SIGs")
	}
	if categories["implementation"] == 0 {
		t.Error("expected implementation SIGs")
	}
	if categories["cross-cutting"] == 0 {
		t.Error("expected cross-cutting SIGs")
	}

	// Find the Collector SIG and verify fields
	var collector *struct {
		Name, ID, Category, NotesDocID, SlackChannelID, SlackChannelName string
	}
	for _, sig := range sigs {
		if sig.Name == "Collector" {
			collector = &struct {
				Name, ID, Category, NotesDocID, SlackChannelID, SlackChannelName string
			}{
				Name:             sig.Name,
				ID:               sig.ID,
				Category:         sig.Category,
				NotesDocID:       sig.NotesDocID,
				SlackChannelID:   sig.SlackChannelID,
				SlackChannelName: sig.SlackChannelName,
			}
			break
		}
	}

	if collector == nil {
		t.Fatal("Collector SIG not found")
	}
	if collector.ID != "collector" {
		t.Errorf("Collector ID = %q, want %q", collector.ID, "collector")
	}
	if collector.Category != "implementation" {
		t.Errorf("Collector category = %q, want %q", collector.Category, "implementation")
	}
	if collector.NotesDocID != "1r2JC5MB7ab_Aw_TyTuMKLJP4kAcmqg_Moh5M39bEBP0" {
		t.Errorf("Collector NotesDocID = %q, want %q", collector.NotesDocID, "1r2JC5MB7ab_Aw_TyTuMKLJP4kAcmqg_Moh5M39bEBP0")
	}
	if collector.SlackChannelID != "C01N6P7KR6W" {
		t.Errorf("Collector SlackChannelID = %q, want %q", collector.SlackChannelID, "C01N6P7KR6W")
	}
	if collector.SlackChannelName != "#otel-collector" {
		t.Errorf("Collector SlackChannelName = %q, want %q", collector.SlackChannelName, "#otel-collector")
	}
}

func TestNormalizeSIGID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Collector", "collector"},
		{"Specification: General", "specification-general"},
		{"GoLang: SDK", "golang-sdk"},
		{"Java: SDK + Instrumentation", "java-sdk-plus-instrumentation"},
		{".NET: SDK", "net-sdk"},
		{"C++: SDK", "cplusplus-sdk"},
		{"Erlang/Elixir: SDK", "erlang-elixir-sdk"},
		{"Semantic Conventions: General", "semantic-conventions-general"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeSIGID(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeSIGID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMatchSheetNameToSIG(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Collector SIG", "collector"},
		{"collector sig", "collector"},
		{".NET SIG", "net-sdk"},
		{"Go SIG", "golang-sdk"},
		{"JavaScript SIG", "javascript-sdk"},
		{"Java SIG", "java-sdk-plus-instrumentation"},
		{"Python SIG", "python-sdk"},
		{"Semantic Convention SIG", "semantic-conventions-general"},
		{"eBPF instrumentation", "ebpf-instrumentation"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := MatchSheetNameToSIG(tt.input)
			if got != tt.want {
				t.Errorf("MatchSheetNameToSIG(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCleanMarkdown(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"[Collector](https://github.com/...)", "Collector"},
		{"**bold text**", "bold text"},
		{"plain text", "plain text"},
		{"  spaces  ", "spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := cleanMarkdown(tt.input)
			if got != tt.want {
				t.Errorf("cleanMarkdown(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSplitTableRow(t *testing.T) {
	tests := []struct {
		input string
		want  int // number of cells
	}{
		{"| cell1 | cell2 | cell3 |", 3},
		{"| single |", 1},
		{"| a | b |", 2},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := splitTableRow(tt.input)
			if len(got) != tt.want {
				t.Errorf("splitTableRow(%q) returned %d cells, want %d", tt.input, len(got), tt.want)
			}
		})
	}
}
