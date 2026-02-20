package sources

import (
	"os"
	"strings"
	"testing"
)

func TestParseVTT_SampleTranscript(t *testing.T) {
	content, err := os.ReadFile("../../testdata/sample_transcript.vtt")
	if err != nil {
		t.Fatalf("reading testdata: %v", err)
	}

	result := parseVTT(string(content))

	if result == "" {
		t.Fatal("parseVTT returned empty string for valid VTT content")
	}

	// Should not contain the WEBVTT header.
	if strings.Contains(result, "WEBVTT") {
		t.Error("result should not contain the WEBVTT header")
	}

	// Should not contain timestamp lines.
	if strings.Contains(result, "-->") {
		t.Error("result should not contain timestamp lines")
	}

	// Should not contain cue number lines as standalone entries.
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if vttCueNumberRegex.MatchString(trimmed) {
			t.Errorf("result contains cue number line: %q", trimmed)
		}
	}

	// Should contain speaker names and dialogue.
	if !strings.Contains(result, "Pablo Baeyens:") {
		t.Error("result should contain 'Pablo Baeyens:'")
	}
	if !strings.Contains(result, "Dmitrii Anoshin:") {
		t.Error("result should contain 'Dmitrii Anoshin:'")
	}
	if !strings.Contains(result, "Bogdan Drutu:") {
		t.Error("result should contain 'Bogdan Drutu:'")
	}
	if !strings.Contains(result, "Yang Song:") {
		t.Error("result should contain 'Yang Song:'")
	}

	// Should contain actual content from the transcript.
	if !strings.Contains(result, "OTLP HTTP") || !strings.Contains(result, "partial success") {
		t.Error("result should contain dialogue about OTLP HTTP and partial success")
	}
}

func TestParseVTT_EmptyContent(t *testing.T) {
	result := parseVTT("")
	if result != "" {
		t.Errorf("parseVTT on empty content should return empty string, got %q", result)
	}
}

func TestParseVTT_HeaderOnly(t *testing.T) {
	result := parseVTT("WEBVTT\n\n")
	if result != "" {
		t.Errorf("parseVTT on header-only content should return empty string, got %q", result)
	}
}

func TestParseVTT_NoSpeakerNames(t *testing.T) {
	content := `WEBVTT

1
00:00:05.100 --> 00:00:08.200
Hello, this is a test without speaker names.

2
00:00:08.500 --> 00:00:12.300
Another line of dialogue here.
`
	result := parseVTT(content)

	if result == "" {
		t.Fatal("parseVTT should return content even without speaker names")
	}

	if !strings.Contains(result, "Hello, this is a test without speaker names.") {
		t.Error("result should contain the first dialogue line")
	}
	if !strings.Contains(result, "Another line of dialogue here.") {
		t.Error("result should contain the second dialogue line")
	}
}

func TestParseVTT_MultiLineCues(t *testing.T) {
	content := `WEBVTT

1
00:00:05.100 --> 00:00:08.200
Speaker A: First line of a multi-line cue.
This is the continuation of the cue.

2
00:00:08.500 --> 00:00:12.300
Speaker B: A separate cue.
`
	result := parseVTT(content)

	if !strings.Contains(result, "First line of a multi-line cue.") {
		t.Error("result should contain the first line of the multi-line cue")
	}
	if !strings.Contains(result, "This is the continuation of the cue.") {
		t.Error("result should contain the continuation of the multi-line cue")
	}
	if !strings.Contains(result, "Speaker B: A separate cue.") {
		t.Error("result should contain the separate cue")
	}
}

func TestParseVTT_DeduplicateConsecutiveSameSpeaker(t *testing.T) {
	// Zoom VTT often has overlapping cues where the same speaker's text
	// is progressively extended.
	content := `WEBVTT

1
00:00:05.100 --> 00:00:08.200
Speaker A: Hello

2
00:00:06.000 --> 00:00:10.000
Speaker A: Hello everyone

3
00:00:08.000 --> 00:00:12.000
Speaker B: Thanks for joining
`
	result := parseVTT(content)

	// "Speaker A: Hello" should be replaced by "Speaker A: Hello everyone"
	// since "Hello everyone" starts with "Hello" and is from the same speaker.
	lines := strings.Split(result, "\n")

	helloCount := 0
	for _, line := range lines {
		if strings.Contains(line, "Speaker A:") {
			helloCount++
		}
	}
	// Should only have one line from Speaker A (the longer version).
	if helloCount != 1 {
		t.Errorf("expected 1 line from Speaker A (deduplicated), got %d", helloCount)
	}

	if !strings.Contains(result, "Hello everyone") {
		t.Error("result should contain the longer version 'Hello everyone'")
	}
}

func TestParseVTT_SkipNoteAndStyleBlocks(t *testing.T) {
	content := `WEBVTT

NOTE This is a note block

STYLE
::cue { color: white; }

1
00:00:05.100 --> 00:00:08.200
Speaker A: Actual content here.
`
	result := parseVTT(content)

	if strings.Contains(result, "NOTE") {
		t.Error("result should not contain NOTE blocks")
	}
	if strings.Contains(result, "STYLE") {
		t.Error("result should not contain STYLE blocks")
	}
	if !strings.Contains(result, "Actual content here.") {
		t.Error("result should contain the actual dialogue")
	}
}

func TestParseVTT_SkipExactDuplicates(t *testing.T) {
	content := `WEBVTT

1
00:00:05.100 --> 00:00:08.200
Exact duplicate line.

2
00:00:08.500 --> 00:00:12.300
Exact duplicate line.

3
00:00:12.500 --> 00:00:15.000
A different line.
`
	result := parseVTT(content)
	lines := strings.Split(result, "\n")

	dupCount := 0
	for _, line := range lines {
		if strings.TrimSpace(line) == "Exact duplicate line." {
			dupCount++
		}
	}
	if dupCount != 1 {
		t.Errorf("expected 1 instance of duplicate line, got %d", dupCount)
	}
}

func TestMinRecordingDuration(t *testing.T) {
	// Verify the constant is set as expected.
	if minRecordingDuration != 2 {
		t.Errorf("minRecordingDuration = %d, want 2", minRecordingDuration)
	}
}

func TestExtractJSONString(t *testing.T) {
	tests := []struct {
		name string
		json string
		key  string
		want string
	}{
		{
			name: "simple extraction",
			json: `{"transcriptUrl":"https://example.com/vtt","hasTranscript":true}`,
			key:  "transcriptUrl",
			want: "https://example.com/vtt",
		},
		{
			name: "key not found",
			json: `{"other":"value"}`,
			key:  "transcriptUrl",
			want: "",
		},
		{
			name: "empty value",
			json: `{"transcriptUrl":"","hasTranscript":true}`,
			key:  "transcriptUrl",
			want: "",
		},
		{
			name: "extract topic",
			json: `{"topic":"Collector SIG Meeting","transcriptUrl":"/vtt"}`,
			key:  "topic",
			want: "Collector SIG Meeting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSONString(tt.json, tt.key)
			if got != tt.want {
				t.Errorf("extractJSONString(%q, %q) = %q, want %q", tt.json, tt.key, got, tt.want)
			}
		})
	}
}

func TestVTTTimestampRegex(t *testing.T) {
	tests := []struct {
		input string
		match bool
	}{
		{"00:03:59.730 --> 00:04:01.619", true},
		{"00:00:00.000 --> 00:00:05.000", true},
		{"12:34:56.789 --> 12:34:58.000", true},
		{"not a timestamp", false},
		{"00:03:59 --> 00:04:01", false}, // missing milliseconds
		{"", false},
	}

	for _, tt := range tests {
		got := vttTimestampRegex.MatchString(tt.input)
		if got != tt.match {
			t.Errorf("vttTimestampRegex.MatchString(%q) = %v, want %v", tt.input, got, tt.match)
		}
	}
}

func TestVTTCueNumberRegex(t *testing.T) {
	tests := []struct {
		input string
		match bool
	}{
		{"1", true},
		{"42", true},
		{"100", true},
		{"0", true},
		{"abc", false},
		{"1a", false},
		{"", false},
		{"Speaker A: text", false},
	}

	for _, tt := range tests {
		got := vttCueNumberRegex.MatchString(tt.input)
		if got != tt.match {
			t.Errorf("vttCueNumberRegex.MatchString(%q) = %v, want %v", tt.input, got, tt.match)
		}
	}
}
