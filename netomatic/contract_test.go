package netomatic

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func TestContractIsComplete(t *testing.T) {
	if err := ValidateContract(); err != nil {
		t.Fatal(err)
	}
	if len(Contract) != ClientOperationCount+13 {
		t.Fatalf("contract rows = %d, want %d", len(Contract), ClientOperationCount+13)
	}
	if Contract[ClientOperationCount-1].Name != "RunOutput" {
		t.Fatalf("last client operation = %q, want RunOutput", Contract[ClientOperationCount-1].Name)
	}
	if Contract[ClientOperationCount].Name != "Health" || Contract[ClientOperationCount+1].Name != "Capabilities" {
		t.Fatal("health and capabilities must follow the client operation inventory")
	}
	if Contract[ClientOperationCount+2].Name != "AddRepository" || !Contract[ClientOperationCount+2].Authenticated {
		t.Fatal("daemon mutation operations should remain authenticated")
	}
	if Contract[len(Contract)-1].Name != "ReadDaemonLog" || !Contract[len(Contract)-1].Authenticated {
		t.Fatal("daemon log must be the authenticated final operation")
	}
}

func TestEveryContractDTOJSONRoundTrips(t *testing.T) {
	for _, operation := range Contract {
		for _, typeName := range []string{operation.Request, operation.Response} {
			if typeName == "" {
				continue
			}
			value, ok := contractDTOs[typeName]
			if !ok {
				t.Errorf("%s names unknown DTO %q", operation.Name, typeName)
				continue
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Errorf("json.Marshal(%s): %v", typeName, err)
				continue
			}
			decoded := reflect.New(reflect.TypeOf(value))
			if err := json.Unmarshal(encoded, decoded.Interface()); err != nil {
				t.Errorf("json.Unmarshal(%s): %v", typeName, err)
				continue
			}
			if !reflect.DeepEqual(value, decoded.Elem().Interface()) {
				t.Errorf("JSON round trip changed %s: %s", typeName, encoded)
			}
		}
	}
}

var contractDTOs = map[string]any{
	"ProcessResponse": ProcessResponse{
		Status: "running", PID: 42, StartedAt: "2026-08-19T12:00:00Z",
	},
	"ListProjectsResponse": ListProjectsResponse{
		Projects: []Project{{Name: "demo", Description: "Example project", Open: true}},
	},
	"CreateProjectRequest": CreateProjectRequest{
		Name: "demo", Description: "Example project",
	},
	"CreateProjectResponse": CreateProjectResponse{
		Project: Project{Name: "demo", Description: "Example project", Open: true},
	},
	"OpenProjectRequest": OpenProjectRequest{Project: "demo"},
	"OpenProjectResponse": OpenProjectResponse{
		Project: Project{Name: "demo", Description: "Example project", Open: true},
	},
	"ForgetProjectRequest":    ForgetProjectRequest{Project: "demo"},
	"ForgetProjectResponse":   ForgetProjectResponse{Forgotten: true},
	"ProjectSummariesRequest": ProjectSummariesRequest{Project: "demo"},
	"ProjectSummariesResponse": ProjectSummariesResponse{
		Summaries: []ProjectSummary{{Project: "demo", EpicCount: 2, IssueCount: 5, PullRequestCount: 3}},
	},
	"GetSetupRequest": GetSetupRequest{Project: "demo"},
	"GetSetupResponse": GetSetupResponse{Setup: Setup{
		Project: "demo", Repository: "origin", Organisation: "acme", Agent: "coder", Variants: []string{"fast", "safe"}, Complete: true,
	}},
	"SaveSetupRequest": SaveSetupRequest{Project: "demo", Setup: Setup{
		Project: "demo", Repository: "origin", Organisation: "acme", Agent: "coder", Variants: []string{"fast", "safe"}, Complete: true,
	}},
	"SaveSetupResponse": SaveSetupResponse{Setup: Setup{
		Project: "demo", Repository: "origin", Organisation: "acme", Agent: "coder", Variants: []string{"fast", "safe"}, Complete: true,
	}},
	"ListEpicsRequest": ListEpicsRequest{Project: "demo"},
	"ListEpicsResponse": ListEpicsResponse{Epics: []Epic{{
		ID: "epic-1", Prefix: "EP-1", Title: "First epic", Description: "Plan the release", Status: "open",
		Issues: []Issue{{ID: "issue-1", EpicID: "epic-1", Title: "First issue", Description: "Implement it", Status: "open"}},
	}}},
	"GetEpicRequest": GetEpicRequest{Project: "demo", Epic: "epic-1"},
	"GetEpicResponse": GetEpicResponse{Epic: Epic{
		ID: "epic-1", Prefix: "EP-1", Title: "First epic", Description: "Plan the release", Status: "open",
		Issues: []Issue{{ID: "issue-1", EpicID: "epic-1", Title: "First issue", Description: "Implement it", Status: "open"}},
	}},
	"CreateEpicRequest": CreateEpicRequest{
		Project: "demo", Title: "First epic", Description: "Plan the release",
	},
	"CreateEpicResponse": CreateEpicResponse{Epic: Epic{
		ID: "epic-1", Prefix: "EP-1", Title: "First epic", Description: "Plan the release", Status: "open",
	}},
	"PrefixEpicRequest": PrefixEpicRequest{Project: "demo", Epic: "epic-1", Prefix: "EP-1"},
	"PrefixEpicResponse": PrefixEpicResponse{Epic: Epic{
		ID: "epic-1", Prefix: "EP-1", Title: "First epic", Description: "Plan the release", Status: "open",
	}},
	"TransitionEpicRequest": TransitionEpicRequest{Project: "demo", Epic: "epic-1", Status: "in_progress"},
	"TransitionEpicResponse": TransitionEpicResponse{Epic: Epic{
		ID: "epic-1", Prefix: "EP-1", Title: "First epic", Description: "Plan the release", Status: "in_progress",
	}},
	"CloseEpicRequest": CloseEpicRequest{Project: "demo", Epic: "epic-1"},
	"CloseEpicResponse": CloseEpicResponse{Epic: Epic{
		ID: "epic-1", Prefix: "EP-1", Title: "First epic", Description: "Plan the release", Status: "closed",
	}},
	"ListIssuesRequest": ListIssuesRequest{Project: "demo", Epic: "epic-1"},
	"ListIssuesResponse": ListIssuesResponse{Issues: []Issue{{
		ID: "issue-1", EpicID: "epic-1", Title: "First issue", Description: "Implement it", Status: "open",
	}}},
	"GetIssueRequest": GetIssueRequest{Project: "demo", Epic: "epic-1", Issue: "issue-1"},
	"GetIssueResponse": GetIssueResponse{Issue: Issue{
		ID: "issue-1", EpicID: "epic-1", Title: "First issue", Description: "Implement it", Status: "open",
	}},
	"CreateIssueRequest": CreateIssueRequest{
		Project: "demo", Epic: "epic-1", Title: "First issue", Description: "Implement it",
	},
	"CreateIssueResponse": CreateIssueResponse{Issue: Issue{
		ID: "issue-1", EpicID: "epic-1", Title: "First issue", Description: "Implement it", Status: "open",
	}},
	"UpdateIssueRequest": UpdateIssueRequest{
		Project: "demo", Epic: "epic-1", Issue: "issue-1", Title: "Updated issue", Description: "Updated details",
	},
	"UpdateIssueResponse": UpdateIssueResponse{Issue: Issue{
		ID: "issue-1", EpicID: "epic-1", Title: "Updated issue", Description: "Updated details", Status: "open",
	}},
	"TransitionIssueRequest": TransitionIssueRequest{Project: "demo", Epic: "epic-1", Issue: "issue-1", Status: "in_progress"},
	"TransitionIssueResponse": TransitionIssueResponse{Issue: Issue{
		ID: "issue-1", EpicID: "epic-1", Title: "First issue", Description: "Implement it", Status: "in_progress",
	}},
	"CloseIssueRequest": CloseIssueRequest{Project: "demo", Epic: "epic-1", Issue: "issue-1"},
	"CloseIssueResponse": CloseIssueResponse{Issue: Issue{
		ID: "issue-1", EpicID: "epic-1", Title: "First issue", Description: "Implement it", Status: "closed",
	}},
	"ListPullRequestsRequest": ListPullRequestsRequest{Project: "demo", Epic: "epic-1", Issue: "issue-1"},
	"ListPullRequestsResponse": ListPullRequestsResponse{PullRequests: []PullRequest{{
		ID: "pr-1", Number: 7, Title: "Implement issue", Description: "The implementation", URL: "https://example.test/pr/7", Branch: "feature/issue-1", Status: "open",
	}}},
	"CreatePullRequestRequest": CreatePullRequestRequest{
		Project: "demo", Epic: "epic-1", Issue: "issue-1", Title: "Implement issue", Description: "The implementation", Branch: "feature/issue-1",
	},
	"CreatePullRequestResponse": CreatePullRequestResponse{PullRequest: PullRequest{
		ID: "pr-1", Number: 7, Title: "Implement issue", Description: "The implementation", URL: "https://example.test/pr/7", Branch: "feature/issue-1", Status: "open",
	}},
	"CommentPullRequestRequest": CommentPullRequestRequest{Project: "demo", PullRequest: "pr-1", Body: "Please review"},
	"CommentPullRequestResponse": CommentPullRequestResponse{Comment: Comment{
		ID: "comment-1", Author: "reviewer", Body: "Please review", CreatedAt: "2026-08-19T12:01:00Z",
	}},
	"MergePullRequestRequest": MergePullRequestRequest{Project: "demo", PullRequest: "pr-1"},
	"MergePullRequestResponse": MergePullRequestResponse{PullRequest: PullRequest{
		ID: "pr-1", Number: 7, Title: "Implement issue", Description: "The implementation", URL: "https://example.test/pr/7", Branch: "feature/issue-1", Status: "merged",
	}},
	"ClosePullRequestRequest": ClosePullRequestRequest{Project: "demo", PullRequest: "pr-1"},
	"ClosePullRequestResponse": ClosePullRequestResponse{PullRequest: PullRequest{
		ID: "pr-1", Number: 7, Title: "Implement issue", Description: "The implementation", URL: "https://example.test/pr/7", Branch: "feature/issue-1", Status: "closed",
	}},
	"ResetPullRequestRequest": ResetPullRequestRequest{Project: "demo", PullRequest: "pr-1"},
	"ResetPullRequestResponse": ResetPullRequestResponse{PullRequest: PullRequest{
		ID: "pr-1", Number: 7, Title: "Implement issue", Description: "The implementation", URL: "https://example.test/pr/7", Branch: "feature/issue-1", Status: "reset",
	}},
	"GrantPullRequestRequest": GrantPullRequestRequest{Project: "demo", PullRequest: "pr-1", Branch: "feature/issue-1"},
	"GrantPullRequestResponse": GrantPullRequestResponse{PullRequest: PullRequest{
		ID: "pr-1", Number: 7, Title: "Implement issue", Description: "The implementation", URL: "https://example.test/pr/7", Branch: "feature/issue-1", Status: "granted",
	}},
	"PullRequestDiffRequest": PullRequestDiffRequest{Project: "demo", PullRequest: "pr-1"},
	"PullRequestDiffResponse": PullRequestDiffResponse{Diff: Diff{
		FilesChanged: 2, Additions: 10, Deletions: 3, Patch: "@@ -1 +1 @@\n-old\n+new\n",
	}},
	"ListRepositoriesRequest": ListRepositoriesRequest{Organisation: "acme"},
	"ListRepositoriesResponse": ListRepositoriesResponse{Repositories: []Repository{{
		ID: "repo-1", Name: "donsy", Owner: "acme", URL: "https://example.test/donsy", Default: "main",
	}}},
	"GetRepositoryRequest": GetRepositoryRequest{Organisation: "acme", Repository: "donsy"},
	"GetRepositoryResponse": GetRepositoryResponse{Repository: Repository{
		ID: "repo-1", Name: "donsy", Owner: "acme", URL: "https://example.test/donsy", Default: "main",
	}},
	"ListOrganisationsRequest": ListOrganisationsRequest{},
	"ListOrganisationsResponse": ListOrganisationsResponse{Organisations: []Organisation{{
		ID: "org-1", Name: "acme",
	}}},
	"GetOrganisationRequest": GetOrganisationRequest{Organisation: "acme"},
	"GetOrganisationResponse": GetOrganisationResponse{Organisation: Organisation{
		ID: "org-1", Name: "acme",
	}},
	"GetAgentSettingsRequest": GetAgentSettingsRequest{Project: "demo"},
	"GetAgentSettingsResponse": GetAgentSettingsResponse{Settings: []AgentSettings{{
		Agent: "coder", Variant: "fast", Values: map[string]string{"model": "small", "temperature": "0.2"},
	}}},
	"ListAgentRunsRequest": ListAgentRunsRequest{Project: "demo"},
	"ListAgentRunsResponse": ListAgentRunsResponse{Runs: []AgentRun{{
		ID: "run-1", Project: "demo", Agent: "coder", Variant: "fast", Status: "failed", SessionID: "session-1", Error: "agent exited", StartedAt: "2026-08-19T12:00:00Z", FinishedAt: "2026-08-19T12:02:00Z", InputTokens: 12, OutputTokens: 34,
	}}},
	"ListSandboxesRequest": ListSandboxesRequest{},
	"ListSandboxesResponse": ListSandboxesResponse{Sandboxes: []Sandbox{{
		ID: "sandbox-1", Name: "demo-sandbox", Status: "ready", AgentRunID: "run-1",
	}}},
	"CancelAgentRunRequest": CancelAgentRunRequest{Run: "run-1"},
	"CancelAgentRunResponse": CancelAgentRunResponse{Run: AgentRun{
		ID: "run-1", Project: "demo", Agent: "coder", Variant: "fast", Status: "cancelled", SessionID: "session-1", StartedAt: "2026-08-19T12:00:00Z", FinishedAt: "2026-08-19T12:02:00Z", InputTokens: 12, OutputTokens: 34,
	}},
	"AgentActivityRequest": AgentActivityRequest{Run: "run-1"},
	"AgentActivityResponse": AgentActivityResponse{Activity: []AgentActivity{{
		RunID: "run-1", Status: "working", Message: "Writing code", UpdatedAt: "2026-08-19T12:01:00Z",
	}}},
	"RunOutputRequest": RunOutputRequest{Run: "run-1", Offset: 128},
	"RunOutputResponse": RunOutputResponse{Output: RunOutput{
		RunID: "run-1", Output: "finished step\n", Done: true,
	}},
	"HealthResponse": HealthResponse{Status: "ready", Protocol: ProtocolVersion, Version: "1.2.3"},
	"CapabilitiesResponse": CapabilitiesResponse{Protocol: ProtocolVersion, Capabilities: []Capability{
		{Name: "reconcile", Available: true}, {Name: "purge", Available: false, Reason: "disabled"},
	}},
	"AddRepositoryRequest": AddRepositoryRequest{
		Project: "demo", Name: "donsy", URL: "https://example.test/donsy", Branch: "main",
	},
	"AddRepositoryResponse": AddRepositoryResponse{Repository: Repository{
		ID: "repo-1", Name: "donsy", Owner: "acme", URL: "https://example.test/donsy", Default: "main",
	}},
	"GetAgentRunRequest": GetAgentRunRequest{Run: "run-1"},
	"GetAgentRunResponse": GetAgentRunResponse{Run: AgentRun{
		ID: "run-1", Project: "demo", Agent: "coder", Variant: "fast", Status: "complete", SessionID: "session-1", StartedAt: "2026-08-19T12:00:00Z", FinishedAt: "2026-08-19T12:02:00Z", InputTokens: 12, OutputTokens: 34,
	}},
	"CompleteRequest":                CompleteRequest{Project: "demo", Run: "run-1"},
	"CompleteResponse":               CompleteResponse{Complete: true},
	"ReviewApprovedBranchesRequest":  ReviewApprovedBranchesRequest{Project: "demo"},
	"ReviewApprovedBranchesResponse": ReviewApprovedBranchesResponse{Branches: []string{"main", "release"}},
	"RunEpicRequest":                 RunEpicRequest{Project: "demo", Epic: "epic-1"},
	"RunEpicResponse": RunEpicResponse{Run: AgentRun{
		ID: "run-1", Project: "demo", Agent: "coder", Variant: "fast", Status: "queued", SessionID: "session-1", StartedAt: "2026-08-19T12:00:00Z", InputTokens: 12, OutputTokens: 34,
	}},
	"RunIssueRequest": RunIssueRequest{Project: "demo", Epic: "epic-1", Issue: "issue-1"},
	"RunIssueResponse": RunIssueResponse{Run: AgentRun{
		ID: "run-1", Project: "demo", Agent: "coder", Variant: "fast", Status: "queued", SessionID: "session-1", StartedAt: "2026-08-19T12:00:00Z", InputTokens: 12, OutputTokens: 34,
	}},
	"OpenPullRequestsRequest": OpenPullRequestsRequest{Project: "demo"},
	"OpenPullRequestsResponse": OpenPullRequestsResponse{PullRequests: []PullRequest{{
		ID: "pr-1", Number: 7, Title: "Implement issue", Description: "The implementation", URL: "https://example.test/pr/7", Branch: "feature/issue-1", Status: "open",
	}}},
	"TransitionPullRequestRequest": TransitionPullRequestRequest{Project: "demo", PullRequest: "pr-1", Status: "approved"},
	"TransitionPullRequestResponse": TransitionPullRequestResponse{PullRequest: PullRequest{
		ID: "pr-1", Number: 7, Title: "Implement issue", Description: "The implementation", URL: "https://example.test/pr/7", Branch: "feature/issue-1", Status: "approved",
	}},
	"ReconcileRequest":     ReconcileRequest{Project: "demo"},
	"ReconcileResponse":    ReconcileResponse{Reconciled: 4},
	"PurgeRequest":         PurgeRequest{Project: "demo"},
	"PurgeResponse":        PurgeResponse{Purged: 2},
	"ReadDaemonLogRequest": ReadDaemonLogRequest{Offset: 32, Limit: 4},
	"ReadDaemonLogResponse": ReadDaemonLogResponse{
		Lines: []string{"first", "second"}, NextOffset: 44, OffsetReset: true,
	},
}

func TestRepresentativeDTOJSON(t *testing.T) {
	value := ReadDaemonLogResponse{Lines: []string{"first", "second"}, NextOffset: 24, OffsetReset: true}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"lines":["first","second"],"next_offset":24,"offset_reset":true}`
	if string(encoded) != want {
		t.Fatalf("ReadDaemonLogResponse JSON = %s, want %s", encoded, want)
	}

	request := CreateProjectRequest{Name: "demo", Description: "a project"}
	encoded, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	const wantRequest = `{"name":"demo","description":"a project"}`
	if string(encoded) != wantRequest {
		t.Fatalf("CreateProjectRequest JSON = %s, want %s", encoded, wantRequest)
	}
}

func TestBoundDaemonLogRequest(t *testing.T) {
	bounded, err := BoundDaemonLogRequest(ReadDaemonLogRequest{Offset: 8, Limit: MaxDaemonLogLines + 1})
	if err != nil {
		t.Fatal(err)
	}
	if bounded.Offset != 8 || bounded.Limit != MaxDaemonLogLines {
		t.Fatalf("bounded request = %#v", bounded)
	}

	for _, test := range []struct {
		name    string
		request ReadDaemonLogRequest
		want    error
	}{
		{name: "negative offset", request: ReadDaemonLogRequest{Offset: -1, Limit: 1}, want: ErrInvalidLogOffset},
		{name: "zero limit", request: ReadDaemonLogRequest{Limit: 0}, want: ErrInvalidLogLimit},
		{name: "negative limit", request: ReadDaemonLogRequest{Limit: -1}, want: ErrInvalidLogLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := BoundDaemonLogRequest(test.request)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.want)
			}
		})
	}
}

func TestPageDaemonLog(t *testing.T) {
	content := []byte("first\nsecond\nthird\npartial")

	page, err := PageDaemonLog(content, ReadDaemonLogRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(page.Lines, []string{"first", "second"}) || page.NextOffset != int64(len("first\nsecond\n")) {
		t.Fatalf("first page = %#v", page)
	}

	page, err = PageDaemonLog(content, ReadDaemonLogRequest{Offset: page.NextOffset, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(page.Lines, []string{"third"}) || page.NextOffset != int64(len("first\nsecond\nthird\n")) {
		t.Fatalf("second page = %#v", page)
	}

	page, err = PageDaemonLog(content, ReadDaemonLogRequest{Offset: 999, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !page.OffsetReset || page.NextOffset != int64(len("first\n")) {
		t.Fatalf("reset page = %#v", page)
	}

	longLine := append(bytes.Repeat([]byte{'x'}, MaxDaemonLogBytes+1), '\n')
	page, err = PageDaemonLog(longLine, ReadDaemonLogRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Lines) != 1 || len(page.Lines[0]) != MaxDaemonLogBytes+1 || page.NextOffset != int64(len(longLine)) {
		t.Fatalf("oversized line page = lines %d, line bytes %d, next %d", len(page.Lines), len(page.Lines[0]), page.NextOffset)
	}
}

func TestProtocolAndAPIError(t *testing.T) {
	if !CompatibleProtocol(ProtocolVersion) || CompatibleProtocol("v1.1") || CompatibleProtocol("") {
		t.Fatal("protocol compatibility must require exact v1")
	}
	if err := ValidateProtocol("v2"); !errors.Is(err, ErrInvalidProtocol) {
		t.Fatalf("ValidateProtocol(v2) = %v", err)
	}

	apiError := &APIError{Code: ErrorUnauthorized, Message: "bad token", StatusCode: 401}
	if apiError.Error() != "unauthorized: bad token" {
		t.Fatalf("APIError.Error() = %q", apiError.Error())
	}
	if !errors.Is(apiError, ErrUnauthorized) {
		t.Fatal("APIError should unwrap its HTTP status error")
	}
}
