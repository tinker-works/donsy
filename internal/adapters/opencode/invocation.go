package opencode

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain/agent"
)

const (
	answerStart = "===GO-MERGE-BEGIN==="
	answerEnd   = "===GO-MERGE-END==="
)

type Builder struct{}

var _ application.AgentCommandBuilder = Builder{}

// Command builds the non-interactive OpenCode command for one run.
func (Builder) Command(invocation application.AgentInvocation) ([]string, error) {
	run := invocation.Run
	prompt := invocation.Prompt
	if err := run.Validate(); err != nil {
		return nil, err
	}
	if prompt == "" {
		return nil, fmt.Errorf("agent prompt is required")
	}
	// The image ships a pinned OpenCode; a binary that self-updates mid-round
	// isn't pinned. The env prefix rides the argv so the guarantee lives with
	// the invocation instead of depending on how a sandbox was provisioned.
	// --dir is what keeps the round's own mounts from counting as external
	// directories (see agent_runtime.GuestMountRoot). Without it the agent runs
	// from the guest home, every tool call touching a mount asks for approval,
	// and a step that issues several at once never gets an answer for any of
	// them. It also changes which session --continue resumes, so a sandbox created
	// before this flag existed starts one fresh round rather than resuming.
	argv := []string{
		"env", "OPENCODE_DISABLE_AUTOUPDATE=1",
	}
	keys := make([]string, 0, len(invocation.Environment))
	for key := range invocation.Environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		argv = append(argv, key+"="+invocation.Environment[key])
	}
	argv = append(argv,
		"opencode", "run", "--auto", "--format", "json",
		"--dir", agent_runtime.GuestMountRoot,
	)
	if run.SessionMode == agent.SessionModeContinue {
		argv = append(argv, "--continue")
	}
	// run.Agent names a model, "provider/model", and goes to --model. OpenCode's
	// --agent selects one of its own configured agents, each carrying its own
	// system prompt and tool policy — which would compete with the role prompt
	// this round already supplies. An unknown value there is not an error either:
	// OpenCode warns and silently falls back to its default agent, so a model
	// passed to --agent runs the whole round on the wrong footing.
	argv = append(argv, "--model", run.Agent)
	if run.Variant != "" {
		argv = append(argv, "--variant", run.Variant)
	}
	prompt += "\n\nReturn your final answer only between these exact lines:\n" +
		answerStart + "\n<answer>\n" + answerEnd
	return append(argv, prompt), nil
}

func (Builder) ExtractAnswer(output string) string {
	text := agentText(output)
	start := strings.LastIndex(text, answerStart)
	if start == -1 {
		return ""
	}
	remainder := text[start+len(answerStart):]
	end := strings.Index(remainder, answerEnd)
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(remainder[:end])
}

func agentText(output string) string {
	var text []string
	for _, line := range strings.Split(output, "\n") {
		var event struct {
			Type string `json:"type"`
			Part struct {
				Text string `json:"text"`
			} `json:"part"`
		}
		if json.Unmarshal([]byte(line), &event) == nil && event.Type == "text" {
			text = append(text, event.Part.Text)
		}
	}
	if len(text) == 0 {
		return output
	}
	return strings.Join(text, "\n")
}

func (Builder) ReviewApproved(answer string) bool {
	lines := strings.Split(answer, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if line == "" {
			continue
		}
		return strings.EqualFold(line, "VERDICT: approve")
	}
	return false
}
