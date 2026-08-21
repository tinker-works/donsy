package opencode

import "testing"

// A step_finish line as opencode 1.18 writes it, trimmed to the fields that
// matter, plus noise the parser must skip. Built by concatenation because each
// event has to stay one line, as it is in the real transcript.
const usageTranscript = `{"type":"step_start","timestamp":1786623913000}` + "\n" +
	`{"type":"step_finish","timestamp":1786623913236,"sessionID":"ses_1",` +
	`"part":{"id":"prt_1","reason":"tool-calls","type":"step-finish",` +
	`"tokens":{"total":11642,"input":2,"output":107,"reasoning":0,` +
	`"cache":{"write":11533,"read":0}},"cost":0.0299065}}` + "\n" +
	`{"type":"text","part":{"text":"working on it"}}` + "\n" +
	`{"type":"step_finish","timestamp":1786623914000,"sessionID":"ses_1",` +
	`"part":{"id":"prt_2","reason":"stop","type":"step-finish",` +
	`"tokens":{"total":900,"input":800,"output":100,"reasoning":0,` +
	`"cache":{"write":0,"read":11533}},"cost":0.01}}` + "\n" +
	"not json at all\n"

func TestParseUsage_ShouldSumEveryStepFinish(t *testing.T) {
	// Act
	usage := Builder{}.ParseUsage(usageTranscript)

	// Assert: cache writes count as input (the model was fed them), cache reads
	// do not (the cache already held them).
	if usage.TokensIn != 2+11533+800 {
		t.Fatalf("expected input plus cache writes, got %d", usage.TokensIn)
	}
	if usage.TokensOut != 107+100 {
		t.Fatalf("expected summed output tokens, got %d", usage.TokensOut)
	}
	if usage.CostUSD < 0.0399 || usage.CostUSD > 0.04 {
		t.Fatalf("expected summed cost ≈ 0.0399, got %f", usage.CostUSD)
	}
}

func TestParseUsage_ShouldReportNothingForAnEmptyTranscript(t *testing.T) {
	if usage := (Builder{}).ParseUsage("no events here"); usage.Reported() {
		t.Fatalf("expected no usage, got %+v", usage)
	}
}
