package netomatic

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"testing"
)

func TestContractIsComplete(t *testing.T) {
	if err := ValidateContract(); err != nil {
		t.Fatal(err)
	}
	if len(Contract) != ClientOperationCount+10 {
		t.Fatalf("contract rows = %d, want %d", len(Contract), ClientOperationCount+10)
	}
	if Contract[ClientOperationCount-1].Name != "RunOutput" {
		t.Fatalf("last client operation = %q, want RunOutput", Contract[ClientOperationCount-1].Name)
	}
	if Contract[ClientOperationCount].Name != "Capabilities" || !Contract[ClientOperationCount].Authenticated {
		t.Fatal("authenticated capabilities must follow the client operation inventory")
	}
	if Contract[ClientOperationCount+1].Name != "AddRepository" || !Contract[ClientOperationCount+1].Authenticated {
		t.Fatal("daemon mutation operations should remain authenticated")
	}
	if Contract[len(Contract)-1].Name != "ReadDaemonLog" || !Contract[len(Contract)-1].Authenticated {
		t.Fatal("daemon log must be the authenticated final operation")
	}
}

func TestOperationValidationAllowsNoContentRows(t *testing.T) {
	if err := validateOperation(Operation{
		Name: "Close", Method: MethodDelete, Route: APIPrefix + "/items/{item}", SuccessStatus: 204,
	}); err != nil {
		t.Fatal(err)
	}
	for _, status := range []int{0, 199, 300, 500} {
		t.Run(fmt.Sprintf("status-%d", status), func(t *testing.T) {
			err := validateOperation(Operation{
				Name: "Close", Method: MethodDelete, Route: APIPrefix + "/items/{item}", SuccessStatus: status,
			})
			if err == nil {
				t.Fatalf("validateOperation accepted status %d", status)
			}
		})
	}

	if err := validateOperation(Operation{Name: "MissingResponse", Method: MethodGet, Route: APIPrefix + "/items", SuccessStatus: 200}); err != nil {
		t.Fatalf("empty response rejected: %v", err)
	}
	for _, operation := range []Operation{
		{Method: MethodGet, Route: APIPrefix + "/items", SuccessStatus: 200},
		{Name: "MissingMethod", Route: APIPrefix + "/items", SuccessStatus: 200},
		{Name: "MissingRoute", Method: MethodGet, SuccessStatus: 200},
	} {
		if err := validateOperation(operation); err == nil {
			t.Fatalf("incomplete operation accepted: %#v", operation)
		}
	}
}

func TestEveryContractDTOJSONRoundTrips(t *testing.T) {
	for _, operation := range Contract {
		if operation.Path != "" {
			if _, ok := contractPathDTOs[operation.Path]; !ok {
				t.Errorf("%s names unknown path fixture %q", operation.Name, operation.Path)
			}
		}
		if operation.Query != "" {
			if _, ok := contractQueryDTOs[operation.Query]; !ok {
				t.Errorf("%s names unknown query fixture %q", operation.Name, operation.Query)
			}
		}
		for _, typeName := range []string{operation.Request, operation.Response} {
			if typeName == "" {
				continue
			}
			fixture, ok := contractDTOs[typeName]
			if !ok {
				t.Errorf("%s names unknown DTO %q", operation.Name, typeName)
				continue
			}
			value := contractFixtureValue(fixture)
			encoded, err := contractFixtureJSON(fixture)
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

type contractFixture struct {
	value any
	json  string
}

func contractFixtureValue(fixture any) any {
	if fixture, ok := fixture.(contractFixture); ok {
		return fixture.value
	}
	return fixture
}

func contractFixtureJSON(fixture any) ([]byte, error) {
	if fixture, ok := fixture.(contractFixture); ok {
		return []byte(fixture.json), nil
	}
	return json.Marshal(fixture)
}

func contractObjectFixture(value any, field, object string) contractFixture {
	return contractFixture{value: value, json: `{"` + field + `":` + object + `}`}
}

func contractListFixture(value any, field, object string) contractFixture {
	return contractFixture{value: value, json: `{"` + field + `":[` + object + `]}`}
}

const (
	commentFixtureJSON     = `{"ID":"comment-1","Author":"reviewer","CreatedAt":"2026-08-19T12:01:00Z","Body":"Please review"}`
	issueFixtureJSON       = `{"ID":"issue-1","Title":"First issue","ParentID":"parent-1","Repository":"origin","State":"Coding","CreatedAt":"2026-08-19T12:00:00Z","Body":"Implement it","Comments":[` + commentFixtureJSON + `],"BlockedBy":["issue-0"]}`
	pullRequestFixtureJSON = `{"ID":"pr-1","IssueID":"issue-1","Title":"Implement issue","Status":"open","Repository":"origin","Number":7,"URL":"https://example.test/pr/7","Head":"feature/issue-1","Base":"main","Flags":["stale","human-needed"],"ReviewedHead":"abc123","ReviewedBase":"def456","Rounds":2,"Reviews":1,"RoundsGranted":1,"CodingRounds":2,"Approved":true,"CreatedAt":"2026-08-19T12:02:00Z","Comments":[` + commentFixtureJSON + `]}`
	epicFixtureJSON        = `{"ID":"epic-1","Title":"First epic","Assignee":"alice","Repositories":["origin","secondary"],"Body":"Plan the release","State":"Review","BranchPrefix":"jira-123","Issues":[` + issueFixtureJSON + `],"PullRequests":[` + pullRequestFixtureJSON + `],"DraftingPasses":2}`
)

var (
	contractComment = Comment{
		ID: "comment-1", Author: "reviewer", CreatedAt: "2026-08-19T12:01:00Z", Body: "Please review",
	}
	contractIssue = Issue{
		ID: "issue-1", Title: "First issue", ParentID: "parent-1", Repository: "origin", State: "Coding",
		CreatedAt: "2026-08-19T12:00:00Z", Body: "Implement it", Comments: []Comment{contractComment}, BlockedBy: []string{"issue-0"},
	}
	contractPullRequest = PullRequest{
		ID: "pr-1", IssueID: "issue-1", Title: "Implement issue", Status: "open", Repository: "origin", Number: 7,
		URL: "https://example.test/pr/7", Head: "feature/issue-1", Base: "main", Flags: []string{"stale", "human-needed"},
		ReviewedHead: "abc123", ReviewedBase: "def456", Rounds: 2, Reviews: 1, RoundsGranted: 1, CodingRounds: 2,
		Approved: true, CreatedAt: "2026-08-19T12:02:00Z", Comments: []Comment{contractComment},
	}
	contractEpic = Epic{
		ID: "epic-1", Title: "First epic", Assignee: "alice", Repositories: []string{"origin", "secondary"},
		Body: "Plan the release", State: "Review", BranchPrefix: "jira-123", Issues: []Issue{contractIssue},
		PullRequests: []PullRequest{contractPullRequest}, DraftingPasses: 2,
	}
)

var contractDTOs = map[string]any{
	"ProcessResponse": contractFixture{
		value: ProcessResponse{CurrentUser: "octocat", Protocol: ProtocolVersion},
		json:  `{"currentUser":"octocat","protocol":"v1"}`,
	},
	"ListProjectsResponse": contractFixture{
		value: ListProjectsResponse{{ID: 7, Name: "demo", LastOpenedAt: "2026-08-19T12:00:00Z"}},
		json:  `[{"ID":7,"Name":"demo","LastOpenedAt":"2026-08-19T12:00:00Z"}]`,
	},
	"CreateProjectRequest":  CreateProjectRequest{Name: "demo"},
	"CreateProjectResponse": contractFixture{value: CreateProjectResponse{ID: 7, Name: "demo", LastOpenedAt: "2026-08-19T12:00:00Z"}, json: `{"ID":7,"Name":"demo","LastOpenedAt":"2026-08-19T12:00:00Z"}`},
	"ListProjectSummariesResponse": contractFixture{
		value: ListProjectSummariesResponse{{Project: Project{ID: 7, Name: "demo", LastOpenedAt: "2026-08-19T12:00:00Z"}, Epics: 2, Running: 1}},
		json:  `[{"project":{"ID":7,"Name":"demo","LastOpenedAt":"2026-08-19T12:00:00Z"},"epics":2,"running":1}]`,
	},
	"SetupState": contractFixture{
		value: SetupState{Organisations: 1, Repositories: 2, RolesSet: 3, RolesTotal: 3},
		json:  `{"Organisations":1,"Repositories":2,"RolesSet":3,"RolesTotal":3}`,
	},
	"InitialiseStoreRequest": InitialiseStoreRequest{
		Model: "small", Variant: "fast", Repositories: []string{"acme/donsy", "acme/other"},
	},
	"ListEpicsRequest":        ListEpicsRequest{Project: "demo"},
	"ListEpicsResponse":       contractListFixture(ListEpicsResponse{Epics: []Epic{contractEpic}}, "epics", epicFixtureJSON),
	"GetEpicRequest":          GetEpicRequest{Project: "demo", Epic: "epic-1"},
	"GetEpicResponse":         contractObjectFixture(GetEpicResponse{Epic: contractEpic}, "epic", epicFixtureJSON),
	"CreateEpicRequest":       CreateEpicRequest{Project: "demo", Title: "First epic", Description: "Plan the release"},
	"CreateEpicResponse":      contractObjectFixture(CreateEpicResponse{Epic: contractEpic}, "epic", epicFixtureJSON),
	"PrefixEpicRequest":       PrefixEpicRequest{Project: "demo", Epic: "epic-1", Prefix: "EP-1"},
	"PrefixEpicResponse":      contractObjectFixture(PrefixEpicResponse{Epic: contractEpic}, "epic", epicFixtureJSON),
	"TransitionEpicRequest":   TransitionEpicRequest{Project: "demo", Epic: "epic-1", Status: "in_progress"},
	"TransitionEpicResponse":  contractObjectFixture(TransitionEpicResponse{Epic: contractEpic}, "epic", epicFixtureJSON),
	"CloseEpicRequest":        CloseEpicRequest{Project: "demo", Epic: "epic-1"},
	"CloseEpicResponse":       contractObjectFixture(CloseEpicResponse{Epic: contractEpic}, "epic", epicFixtureJSON),
	"ListIssuesRequest":       ListIssuesRequest{Project: "demo", Epic: "epic-1"},
	"ListIssuesResponse":      contractListFixture(ListIssuesResponse{Issues: []Issue{contractIssue}}, "issues", issueFixtureJSON),
	"GetIssueRequest":         GetIssueRequest{Project: "demo", Epic: "epic-1", Issue: "issue-1"},
	"GetIssueResponse":        contractObjectFixture(GetIssueResponse{Issue: contractIssue}, "issue", issueFixtureJSON),
	"CreateIssueRequest":      CreateIssueRequest{Project: "demo", Epic: "epic-1", Title: "First issue", Description: "Implement it"},
	"CreateIssueResponse":     contractObjectFixture(CreateIssueResponse{Issue: contractIssue}, "issue", issueFixtureJSON),
	"UpdateIssueRequest":      UpdateIssueRequest{Project: "demo", Epic: "epic-1", Issue: "issue-1", Title: "Updated issue", Description: "Updated details"},
	"UpdateIssueResponse":     contractObjectFixture(UpdateIssueResponse{Issue: contractIssue}, "issue", issueFixtureJSON),
	"TransitionIssueRequest":  TransitionIssueRequest{Project: "demo", Epic: "epic-1", Issue: "issue-1", Status: "in_progress"},
	"TransitionIssueResponse": contractObjectFixture(TransitionIssueResponse{Issue: contractIssue}, "issue", issueFixtureJSON),
	"CloseIssueRequest":       CloseIssueRequest{Project: "demo", Epic: "epic-1", Issue: "issue-1"},
	"CloseIssueResponse":      contractObjectFixture(CloseIssueResponse{Issue: contractIssue}, "issue", issueFixtureJSON),
	"CreatePullRequestRequest": contractFixture{
		value: CreatePullRequestRequest{IssueID: "issue-1", Title: "Implement issue", Repository: "origin", Head: "feature/issue-1", Base: "main"},
		json:  `{"issueId":"issue-1","title":"Implement issue","repository":"origin","head":"feature/issue-1","base":"main"}`,
	},
	"TransitionPullRequestRequest": contractFixture{value: TransitionPullRequestRequest{Status: "closed"}, json: `{"status":"closed"}`},
	"MergePullRequestResponse":     contractFixture{value: MergePullRequestResponse{Outcome: MergeOutcomeMerged}, json: `{"outcome":"merged"}`},
	"PullRequestDiffResponse":      contractFixture{value: PullRequestDiffResponse{Diff: "@@ -1 +1 @@\n-old\n+new\n"}, json: `{"diff":"@@ -1 +1 @@\n-old\n+new\n"}`},
	"OpenPullRequestsResponse":     contractFixture{value: OpenPullRequestsResponse{Opened: 2}, json: `{"opened":2}`},
	"AddCommentRequest":            AddCommentRequest{TargetID: "issue-1", Target: IssueCommentTarget, Body: "Please review"},
	"ListRepositoriesRequest":      ListRepositoriesRequest{Organisation: "acme"},
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
	"CapabilitiesResponse": contractFixture{
		value: CapabilitiesResponse{
			"cancelAgentRun":        true,
			"discoverOrganisations": true,
			"listRepositories":      true,
			"readRunOutput":         true,
			"reconcileSandboxes":    true,
			"resetIssue":            true,
			"runEpicAgent":          true,
			"runIssueAgent":         true,
			"syncRepositories":      true,
		},
		json: `{"cancelAgentRun":true,"discoverOrganisations":true,"listRepositories":true,"readRunOutput":true,"reconcileSandboxes":true,"resetIssue":true,"runEpicAgent":true,"runIssueAgent":true,"syncRepositories":true}`,
	},
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
	"ReconcileRequest":     ReconcileRequest{Project: "demo"},
	"ReconcileResponse":    ReconcileResponse{Reconciled: 4},
	"PurgeRequest":         PurgeRequest{Project: "demo"},
	"PurgeResponse":        PurgeResponse{Purged: 2},
	"ReadDaemonLogRequest": ReadDaemonLogRequest{Offset: 32, Limit: 4},
	"ReadDaemonLogResponse": ReadDaemonLogResponse{
		Lines: []string{"first", "second"}, NextOffset: 44, OffsetReset: true,
	},
	"ShapeBody": contractBodyFixture{Name: "new"},
}

var contractPathDTOs = map[string]any{
	"ShapePath":                 contractPathFixture{Project: "demo/blue"},
	"ProjectPath":               ProjectPath{ProjectID: 7},
	"CreatePullRequestPath":     CreatePullRequestPath{ProjectID: 7, EpicID: "epic-1"},
	"TransitionPullRequestPath": TransitionPullRequestPath{ProjectID: 7, EpicID: "epic-1", PullRequestID: "pr-1"},
	"GrantCodingRoundPath":      GrantCodingRoundPath{ProjectID: 7, EpicID: "epic-1", PullRequestID: "pr-1"},
	"MergePullRequestPath":      MergePullRequestPath{ProjectID: 7, EpicID: "epic-1", PullRequestID: "pr-1"},
	"ResetIssuePath":            ResetIssuePath{ProjectID: 7, EpicID: "epic-1", PullRequestID: "pr-1"},
	"GetPullRequestDiffPath":    GetPullRequestDiffPath{ProjectID: 7, EpicID: "epic-1", PullRequestID: "pr-1"},
	"OpenPullRequestsPath":      OpenPullRequestsPath{ProjectID: 7, EpicID: "epic-1"},
	"AddCommentPath":            AddCommentPath{ProjectID: 7, EpicID: "epic-1"},
}

var contractQueryDTOs = map[string]url.Values{
	"ShapeQuery": {"runID": {"run-1", "run-2"}, "from": {"12"}},
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

	request := CreateProjectRequest{Name: "demo"}
	encoded, err = json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	const wantRequest = `{"name":"demo"}`
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
	if len(page.Lines) != 0 || page.NextOffset != int64(len(longLine)) {
		t.Fatalf("oversized line page = lines %d, next %d", len(page.Lines), page.NextOffset)
	}

	content = append(longLine, []byte("kept\n")...)
	page, err = PageDaemonLog(content, ReadDaemonLogRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(page.Lines, []string{"kept"}) || page.NextOffset != int64(len(content)) {
		t.Fatalf("page after oversized line = %#v", page)
	}
}

func TestProtocolAndAPIError(t *testing.T) {
	if !CompatibleProtocol(ProtocolVersion) || CompatibleProtocol("v1.1") || CompatibleProtocol("") {
		t.Fatal("protocol compatibility must require exact v1")
	}
	if err := ValidateProtocol("v2"); !errors.Is(err, ErrInvalidProtocol) {
		t.Fatalf("ValidateProtocol(v2) = %v", err)
	}

	apiError := &APIError{Code: ErrorUnauthorized, Detail: "bad token", StatusCode: 401}
	if apiError.Error() != "bad token" {
		t.Fatalf("APIError.Error() = %q", apiError.Error())
	}
	if !errors.Is(apiError, ErrUnauthorized) {
		t.Fatal("APIError should unwrap its HTTP status error")
	}
	featureError := &APIError{Code: ErrorFeatureNotConfigured, Detail: "repository discovery is unavailable", StatusCode: 501}
	if !errors.Is(featureError, ErrUnavailable) {
		t.Fatal("feature_not_configured 501 should unwrap to ErrUnavailable")
	}
}
