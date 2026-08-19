package agent

// TranscriptKind classifies one entry in a round's transcript, so a reader can
// tell the agent's own words from the tools it reached for and from what those
// tools returned. Engines stream this structure and it is the only thing worth
// keeping from their event format: the raw events are a wire protocol, not
// something a person should be shown.
type TranscriptKind uint8

const (
	// TranscriptText is what the agent said.
	TranscriptText TranscriptKind = iota
	// TranscriptToolUse is a tool the agent called, with a one-line summary of
	// what it was asked to do.
	TranscriptToolUse
	// TranscriptToolOutput is what a tool returned. It is the bulk of any
	// transcript and the least interesting part of it, so a reader caps it.
	TranscriptToolOutput
	// TranscriptReasoning is the agent thinking out loud, which engines report
	// separately from its answer.
	TranscriptReasoning
	// TranscriptError is a failure the engine reported mid-round.
	TranscriptError
	// TranscriptUnknown is a line the engine's parser did not recognise. It is
	// kept rather than dropped so an engine changing its event format shows up as
	// a gap in the transcript instead of as silence.
	TranscriptUnknown
)

// TranscriptEntry is one thing that happened during a round.
type TranscriptEntry struct {
	Kind TranscriptKind
	// Tool names the tool, on TranscriptToolUse and TranscriptToolOutput.
	Tool string
	// CallID identifies one tool call across the several entries an engine emits
	// as it progresses from pending to finished. A reader keyed on it shows the
	// call once, updating in place, rather than once per state it passed through.
	CallID string
	Text   string
}
