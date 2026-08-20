package application

import (
	"github.com/tinker-works/donsy/internal/domain/agent"
)

// AgentInvocation carries the run-specific input an engine needs to start one
// round. Environment is transient invocation state rather than part of the run
// record, because it can contain paths that only exist on this host.
type AgentInvocation struct {
	Run         agent.AgentRun
	Prompt      string
	Environment map[string]string
}

// AgentCommandBuilder keeps engine-specific invocation and output parsing in an adapter.
type AgentCommandBuilder interface {
	Command(AgentInvocation) ([]string, error)
	ExtractAnswer(string) string
	// ParseTranscript turns raw engine output into entries for a human reading a
	// transcript, rather than into an answer to be parsed. It classifies rather
	// than flattens, because what the agent said, what it called and what came
	// back all read differently on screen.
	ParseTranscript(string) []agent.TranscriptEntry
	// ParseUsage sums the token and cost accounting the engine reports across
	// a run's output. A zero result means the engine reported none.
	ParseUsage(string) agent.RunUsage
	ReviewApproved(string) bool
}
