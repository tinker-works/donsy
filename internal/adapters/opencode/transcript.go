package opencode

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tinker-works/donsy/internal/domain/agent"
)

// OpenCode's `run --format json` writes one JSON object per line, shaped
// {"type":…,"timestamp":…,"sessionID":…,"part":{…}}. The type names the part
// that changed; the part itself is the schema OpenCode's own API publishes
// (ToolPart, TextPart and friends).
const (
	eventText       = "text"
	eventReasoning  = "reasoning"
	eventToolUse    = "tool_use"
	eventStepStart  = "step_start"
	eventStepFinish = "step_finish"
	eventError      = "error"
)

type transcriptEvent struct {
	Type string `json:"type"`
	Part struct {
		Text   string `json:"text"`
		Tool   string `json:"tool"`
		CallID string `json:"callID"`
		State  struct {
			Status string          `json:"status"`
			Title  string          `json:"title"`
			Input  map[string]any  `json:"input"`
			Output string          `json:"output"`
			Error  json.RawMessage `json:"error"`
		} `json:"state"`
		// Tokens and Cost arrive on step_finish events, one per model turn.
		Tokens struct {
			Input  int `json:"input"`
			Output int `json:"output"`
			Cache  struct {
				Write int `json:"write"`
				Read  int `json:"read"`
			} `json:"cache"`
		} `json:"tokens"`
		Cost float64 `json:"cost"`
	} `json:"part"`
	Error json.RawMessage `json:"error"`
}

// ParseTranscript turns the engine's event stream into entries a reader can
// render. A line it cannot make sense of becomes one TranscriptUnknown rather
// than being passed through: the raw event is JSON, and showing JSON to someone
// watching an agent work is the bug this exists to fix. Consecutive unknown
// lines collapse into a single entry, so an engine that changed its format
// reports that once instead of filling the panel with it.
func (Builder) ParseTranscript(output string) []agent.TranscriptEntry {
	var entries []agent.TranscriptEntry
	unknown := false
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parsed, ok := parseTranscriptLine(line)
		if !ok {
			if !unknown {
				entries = append(entries, agent.TranscriptEntry{Kind: agent.TranscriptUnknown})
			}
			unknown = true
			continue
		}
		unknown = false
		entries = append(entries, parsed...)
	}
	return entries
}

func parseTranscriptLine(line string) ([]agent.TranscriptEntry, bool) {
	var event transcriptEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return nil, false
	}
	switch event.Type {
	case eventText:
		return textEntry(agent.TranscriptText, event.Part.Text), true
	case eventReasoning:
		return textEntry(agent.TranscriptReasoning, event.Part.Text), true
	case eventToolUse:
		return toolEntries(event), true
	case eventStepStart, eventStepFinish:
		// Structural markers with nothing in them for a *reader*: their token
		// accounting is summed by ParseUsage instead.
		return nil, true
	case eventError:
		return textEntry(agent.TranscriptError, errorText(event.Error)), true
	default:
		return nil, false
	}
}

// ParseUsage sums the token and cost accounting across every step_finish event
// in a run's output. Cache writes count as input — they are tokens the model
// was fed — while cache reads do not: charging a run full price for tokens the
// cache already held would overstate what the round actually consumed.
func (Builder) ParseUsage(output string) agent.RunUsage {
	var usage agent.RunUsage
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, eventStepFinish) {
			continue
		}
		var event transcriptEvent
		if json.Unmarshal([]byte(line), &event) != nil || event.Type != eventStepFinish {
			continue
		}
		usage.TokensIn += event.Part.Tokens.Input + event.Part.Tokens.Cache.Write
		usage.TokensOut += event.Part.Tokens.Output
		usage.CostUSD += event.Part.Cost
	}
	return usage
}

func textEntry(kind agent.TranscriptKind, body string) []agent.TranscriptEntry {
	body = strings.TrimRight(body, "\n")
	if strings.TrimSpace(body) == "" {
		return nil
	}
	return []agent.TranscriptEntry{{Kind: kind, Text: body}}
}

// toolEntries splits one tool event into the call and its result, because the
// two are read differently: the call is the interesting line and the result is
// the bulk that gets capped.
func toolEntries(event transcriptEvent) []agent.TranscriptEntry {
	part := event.Part
	entries := []agent.TranscriptEntry{{
		Kind:   agent.TranscriptToolUse,
		Tool:   part.Tool,
		CallID: part.CallID,
		Text:   toolSummary(part.State.Title, part.State.Input),
	}}
	if output := strings.TrimRight(part.State.Output, "\n"); strings.TrimSpace(output) != "" {
		entries = append(entries, agent.TranscriptEntry{
			Kind: agent.TranscriptToolOutput, Tool: part.Tool, CallID: part.CallID, Text: output,
		})
	}
	if part.State.Status == "error" {
		if message := errorText(part.State.Error); message != "" {
			entries = append(entries, agent.TranscriptEntry{
				Kind: agent.TranscriptError, Tool: part.Tool, CallID: part.CallID, Text: message,
			})
		}
	}
	return entries
}

// toolSummary prefers the title the engine already wrote for a human. Falling
// back to the input keeps a call legible while it is still pending, which is
// when no title exists yet.
func toolSummary(title string, input map[string]any) string {
	if trimmed := strings.TrimSpace(title); trimmed != "" {
		return trimmed
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s", key, oneLine(input[key])))
	}
	return strings.Join(pairs, " ")
}

// oneLine flattens a value to something that fits on a summary line. A tool's
// input can hold a whole file's contents, which belongs nowhere near the call.
func oneLine(value any) string {
	rendered := fmt.Sprintf("%v", value)
	if text, ok := value.(string); ok {
		rendered = text
	}
	rendered = strings.Join(strings.Fields(rendered), " ")
	return rendered
}

// errorText accepts either shape an engine uses for a failure: a bare string, or
// an object carrying a message.
func errorText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var message string
	if json.Unmarshal(raw, &message) == nil {
		return strings.TrimSpace(message)
	}
	var object struct {
		Message string `json:"message"`
		Name    string `json:"name"`
		Data    struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &object) != nil {
		return ""
	}
	for _, candidate := range []string{object.Message, object.Data.Message, object.Name} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
