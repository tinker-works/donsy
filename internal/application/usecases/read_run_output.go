package usecases

import (
	"fmt"
	"strings"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

type ReadRunOutputQuery struct {
	RunID string
	// From is the byte offset a previous read stopped at. Zero reads the
	// transcript from the beginning.
	From int64
}

// RunOutputPage is one poll of a run's transcript. Next is what to pass as
// From next time; when it equals the request's From, nothing new was written.
type RunOutputPage struct {
	Entries []agent.TranscriptEntry
	// Output is the complete transcript text consumed by this page. For normal
	// append-only reads it is byte-for-byte aligned with Next so callers that
	// persist offsets from the returned text can resume safely.
	Output string
	Next   int64
}

// ReadRunOutputUseCase reads what an agent said during a round. The transcript
// is the file the runtime already writes on the host, named after the run, so
// this stores nothing and works for finished runs as well as live ones.
type ReadRunOutputUseCase struct {
	output  agent_runtime.RunOutput
	builder application.AgentCommandBuilder
}

func (u *ReadRunOutputUseCase) Handle(query ReadRunOutputQuery) (RunOutputPage, error) {
	if u.output == nil {
		return RunOutputPage{}, fmt.Errorf("no agent log reader is configured")
	}
	if strings.TrimSpace(query.RunID) == "" {
		return RunOutputPage{}, fmt.Errorf("agent run ID is required")
	}
	from := query.From
	if from < 0 {
		from = 0
	}
	lines, next, err := u.output.Tail(query.RunID, from)
	if err != nil {
		return RunOutputPage{}, err
	}
	output := strings.Join(lines, "\n")
	if len(lines) > 0 {
		output += "\n"
	}
	return RunOutputPage{
		Entries: u.builder.ParseTranscript(output),
		Output:  output,
		Next:    next,
	}, nil
}

// Sizes reports each run's transcript length in bytes, for activity sampling.
// A run whose size cannot be read is simply absent: the sparkline it feeds is
// decoration, and decoration must not surface errors.
func (u *ReadRunOutputUseCase) Sizes(runIDs []string) map[string]int64 {
	sizes := make(map[string]int64, len(runIDs))
	if u.output == nil {
		return sizes
	}
	for _, runID := range runIDs {
		size, err := u.output.Size(runID)
		if err != nil {
			continue
		}
		sizes[runID] = size
	}
	return sizes
}
