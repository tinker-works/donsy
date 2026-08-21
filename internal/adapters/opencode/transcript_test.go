package opencode

import (
	"strings"
	"testing"

	"github.com/tinker-works/donsy/internal/domain/agent"
)

// event wraps a part the way `opencode run --format json` writes one line.
func event(eventType, part string) string {
	return `{"type":"` + eventType + `","timestamp":1,"sessionID":"ses_1","part":` + part + `}`
}

func TestParseTranscript_ShouldClassifyWhatTheAgentSaidAndWhatItCalled(t *testing.T) {
	// Arrange
	raw := strings.Join([]string{
		event("step_start", `{"type":"step-start"}`),
		event("text", `{"type":"text","text":"Splitting the flow."}`),
		event("tool_use", `{"type":"tool","callID":"call_1","tool":"grep","state":{`+
			`"status":"completed","title":"grep checkout","input":{"pattern":"checkout"},`+
			`"output":"runs.go:7\nruns.go:9"}}`),
		event("step_finish", `{"type":"step-finish"}`),
	}, "\n")

	// Act
	entries := Builder{}.ParseTranscript(raw)

	// Assert: the structural markers carry nothing to read and must not become rows.
	want := []agent.TranscriptEntry{
		{Kind: agent.TranscriptText, Text: "Splitting the flow."},
		{Kind: agent.TranscriptToolUse, Tool: "grep", CallID: "call_1", Text: "grep checkout"},
		{Kind: agent.TranscriptToolOutput, Tool: "grep", CallID: "call_1",
			Text: "runs.go:7\nruns.go:9"},
	}
	if len(entries) != len(want) {
		t.Fatalf("expected %d entries, got %+v", len(want), entries)
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Fatalf("entry %d: expected %+v, got %+v", i, want[i], entries[i])
		}
	}
}

func TestParseTranscript_ShouldSummariseAPendingCallFromItsInput(t *testing.T) {
	// Arrange: a call that has not finished has no title yet, and an unsummarised
	// call reads as a blank line.
	raw := event("tool_use", `{"type":"tool","callID":"call_1","tool":"bash","state":{`+
		`"status":"running","input":{"command":"go test ./...","cwd":"/repo"}}}`)

	// Act
	entries := Builder{}.ParseTranscript(raw)

	// Assert: keys are sorted so the summary does not reshuffle between polls.
	if len(entries) != 1 {
		t.Fatalf("expected only the call, got %+v", entries)
	}
	if entries[0].Text != "command=go test ./... cwd=/repo" {
		t.Fatalf("unexpected summary: %q", entries[0].Text)
	}
}

func TestParseTranscript_ShouldReportAFailedCall(t *testing.T) {
	// Arrange
	raw := event("tool_use", `{"type":"tool","callID":"call_1","tool":"bash","state":{`+
		`"status":"error","input":{},"error":"exit status 1"}}`)

	// Act
	entries := Builder{}.ParseTranscript(raw)

	// Assert
	last := entries[len(entries)-1]
	if last.Kind != agent.TranscriptError || last.Text != "exit status 1" {
		t.Fatalf("expected the failure to be reported, got %+v", entries)
	}
}

func TestParseTranscript_ShouldCollapseWhatItCannotUnderstand(t *testing.T) {
	// Arrange: the point of parsing is that raw JSON never reaches a reader. An
	// engine that changed its format has to say so once, not fill the panel.
	raw := strings.Join([]string{
		`not json at all`,
		event("an_event_from_a_future_version", `{}`),
		`{"broken":`,
		event("text", `{"type":"text","text":"still here"}`),
		`also not json`,
	}, "\n")

	// Act
	entries := Builder{}.ParseTranscript(raw)

	// Assert
	want := []agent.TranscriptKind{
		agent.TranscriptUnknown, agent.TranscriptText, agent.TranscriptUnknown,
	}
	if len(entries) != len(want) {
		t.Fatalf("expected the unknown runs to collapse, got %+v", entries)
	}
	for i := range want {
		if entries[i].Kind != want[i] {
			t.Fatalf("entry %d: expected kind %d, got %+v", i, want[i], entries[i])
		}
	}
	for _, entry := range entries {
		if entry.Kind == agent.TranscriptUnknown && strings.Contains(entry.Text, "json") {
			t.Fatalf("expected the raw line to be withheld, got %q", entry.Text)
		}
	}
}

func TestParseTranscript_ShouldDropEmptyText(t *testing.T) {
	// Arrange: engines emit empty text parts as a message opens.
	raw := strings.Join([]string{
		event("text", `{"type":"text","text":""}`),
		event("text", `{"type":"text","text":"\n  \n"}`),
	}, "\n")

	// Act & Assert
	if entries := (Builder{}).ParseTranscript(raw); len(entries) != 0 {
		t.Fatalf("expected nothing to show for empty text, got %+v", entries)
	}
}

func TestParseTranscript_ShouldReadEitherShapeOfAnEngineError(t *testing.T) {
	// Arrange: an engine reports a failure as a bare string or as an object, and
	// the object nests the message in more than one place.
	cases := map[string]string{
		`"the model refused"`:                          "the model refused",
		`{"message":"context length exceeded"}`:        "context length exceeded",
		`{"data":{"message":"upstream timed out"}}`:    "upstream timed out",
		`{"name":"ProviderAuthError"}`:                 "ProviderAuthError",
		`{"message":"  padded  "}`:                     "padded",
		`{"message":"","data":{"message":"fallback"}}`: "fallback",
	}

	// Act & Assert
	for raw, want := range cases {
		entries := Builder{}.ParseTranscript(event("error", `{"type":"error"}`) + "\n" +
			`{"type":"error","timestamp":1,"sessionID":"ses_1","error":` + raw + `}`)
		found := false
		for _, entry := range entries {
			if strings.Contains(entry.Text, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected %q from %s, got %+v", want, raw, entries)
		}
	}
}

func TestErrorText_ShouldReportNothingForAnUnreadableFailure(t *testing.T) {
	// Arrange: an error field that is neither a string nor an object carrying a
	// message has nothing to show, and an invented line would read as the reason.
	cases := []string{"", "[1,2,3]", "{", `{"other":"field"}`, "null"}

	// Act & Assert
	for _, raw := range cases {
		if got := errorText([]byte(raw)); got != "" {
			t.Fatalf("expected nothing from %q, got %q", raw, got)
		}
	}
}
