package netomatic

import (
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
	if Contract[ClientOperationCount-1].Name != "ListSandboxes" {
		t.Fatalf("last client operation = %q, want ListSandboxes", Contract[ClientOperationCount-1].Name)
	}
	if Contract[ClientOperationCount].Name != "Capabilities" || !Contract[ClientOperationCount].Authenticated {
		t.Fatal("authenticated capabilities must follow the client operation inventory")
	}
	if Contract[ClientOperationCount+1].Name != "AddRepository" || !Contract[ClientOperationCount+1].Authenticated {
		t.Fatal("daemon mutation operations should remain authenticated")
	}
	if Contract[ClientOperationCount+2].Name != "GetAgentRun" || !Contract[ClientOperationCount+2].Authenticated {
		t.Fatal("agent run lookup must remain authenticated")
	}
}

func TestOperationValidationAllowsNoContentRows(t *testing.T) {
	if err := validateOperation(Operation{
		Name: "Close", Method: MethodDelete, Route: APIPrefix + "/items/{item}", SuccessStatus: 204,
	}); err != nil {
		t.Fatal(err)
	}
	if err := validateOperation(Operation{Name: "Unavailable", Method: MethodPost, Route: APIPrefix + "/items/{item}", Unavailable: true}); err != nil {
		t.Fatal(err)
	}
	if err := validateOperation(Operation{Name: "Unavailable", Method: MethodPost, Route: APIPrefix + "/items/{item}", SuccessStatus: 204, Unavailable: true}); err == nil {
		t.Fatal("unavailable operation accepted a success status")
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
	"ListEpicsResponse": contractFixture{
		value: ListEpicsResponse{contractEpic},
		json:  `[` + epicFixtureJSON + `]`,
	},
	"Epic": contractFixture{
		value: contractEpic,
		json:  epicFixtureJSON,
	},
	"CreateEpicRequest": CreateEpicRequest{
		Title: "First epic", Assignee: "alice", Body: "Plan the release",
		Repositories: []string{"origin", "secondary"}, BranchPrefix: "jira-123",
	},
	"TransitionEpicStateRequest": TransitionEpicStateRequest{State: "Review", Force: true},
	"SetBranchPrefixRequest":     SetBranchPrefixRequest{Prefix: "EP-1"},
	"CompleteEpicResponse":       CompleteEpicResponse{Completed: true},
	"ListIssuesRequest":          ListIssuesRequest{Project: "demo", Epic: "epic-1"},
	"ListIssuesResponse":         contractListFixture(ListIssuesResponse{Issues: []Issue{contractIssue}}, "issues", issueFixtureJSON),
	"GetIssueRequest":            GetIssueRequest{Project: "demo", Epic: "epic-1", Issue: "issue-1"},
	"GetIssueResponse":           contractObjectFixture(GetIssueResponse{Issue: contractIssue}, "issue", issueFixtureJSON),
	"CreateIssueRequest": CreateIssueRequest{
		Project: "demo", Epic: "epic-1", Title: "First issue", Description: "Implement it",
	},
	"CreateIssueResponse": contractObjectFixture(CreateIssueResponse{Issue: contractIssue}, "issue", issueFixtureJSON),
	"UpdateIssueRequest": UpdateIssueRequest{
		Project: "demo", Epic: "epic-1", Issue: "issue-1", Title: "Updated issue", Description: "Updated details",
	},
	"UpdateIssueResponse":      contractObjectFixture(UpdateIssueResponse{Issue: contractIssue}, "issue", issueFixtureJSON),
	"TransitionIssueRequest":   TransitionIssueRequest{Project: "demo", Epic: "epic-1", Issue: "issue-1", Status: "in_progress"},
	"TransitionIssueResponse":  contractObjectFixture(TransitionIssueResponse{Issue: contractIssue}, "issue", issueFixtureJSON),
	"CloseIssueRequest":        CloseIssueRequest{Project: "demo", Epic: "epic-1", Issue: "issue-1"},
	"CloseIssueResponse":       contractObjectFixture(CloseIssueResponse{Issue: contractIssue}, "issue", issueFixtureJSON),
	"ListPullRequestsRequest":  ListPullRequestsRequest{Project: "demo", Epic: "epic-1", Issue: "issue-1"},
	"ListPullRequestsResponse": contractListFixture(ListPullRequestsResponse{PullRequests: []PullRequest{contractPullRequest}}, "pull_requests", pullRequestFixtureJSON),
	"CreatePullRequestRequest": CreatePullRequestRequest{
		Project: "demo", Epic: "epic-1", Issue: "issue-1", Title: "Implement issue", Description: "The implementation", Branch: "feature/issue-1",
	},
	"CreatePullRequestResponse":  contractObjectFixture(CreatePullRequestResponse{PullRequest: contractPullRequest}, "pull_request", pullRequestFixtureJSON),
	"CommentPullRequestRequest":  CommentPullRequestRequest{Project: "demo", PullRequest: "pr-1", Body: "Please review"},
	"CommentPullRequestResponse": contractObjectFixture(CommentPullRequestResponse{Comment: contractComment}, "comment", commentFixtureJSON),
	"MergePullRequestRequest":    MergePullRequestRequest{Project: "demo", PullRequest: "pr-1"},
	"MergePullRequestResponse":   contractObjectFixture(MergePullRequestResponse{PullRequest: contractPullRequest}, "pull_request", pullRequestFixtureJSON),
	"ClosePullRequestRequest":    ClosePullRequestRequest{Project: "demo", PullRequest: "pr-1"},
	"ClosePullRequestResponse":   contractObjectFixture(ClosePullRequestResponse{PullRequest: contractPullRequest}, "pull_request", pullRequestFixtureJSON),
	"ResetPullRequestRequest":    ResetPullRequestRequest{Project: "demo", PullRequest: "pr-1"},
	"ResetPullRequestResponse":   contractObjectFixture(ResetPullRequestResponse{PullRequest: contractPullRequest}, "pull_request", pullRequestFixtureJSON),
	"GrantPullRequestRequest":    GrantPullRequestRequest{Project: "demo", PullRequest: "pr-1", Branch: "feature/issue-1"},
	"GrantPullRequestResponse":   contractObjectFixture(GrantPullRequestResponse{PullRequest: contractPullRequest}, "pull_request", pullRequestFixtureJSON),
	"PullRequestDiffRequest":     PullRequestDiffRequest{Project: "demo", PullRequest: "pr-1"},
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
	"AgentSettings": contractFixture{
		value: AgentSettings{SetupScript: "agents/setup.sh", Roles: map[string]AgentProfile{
			"coding": {Agent: "coder", Variant: "fast", MaxRounds: 3},
		}},
		json: `{"SetupScript":"agents/setup.sh","Roles":{"coding":{"Agent":"coder","Variant":"fast","MaxRounds":3}}}`,
	},
	"SetAgentRoleRequest": SetAgentRoleRequest{Agent: "coder", Variant: "fast"},
	"ListAgentRunsResponse": contractFixture{
		value: ListAgentRunsResponse{{
			ID: "run-1", ProjectID: 7, SandboxID: "sandbox-1", Role: "coding", Subject: AgentSubject{Kind: "issue", ID: "issue-1"}, Engine: "opencode", Agent: "coder", Variant: "fast", SessionMode: "fresh", Status: "failed", Round: 2, Error: "agent exited", Usage: RunUsage{TokensIn: 12, TokensOut: 34, CostUSD: 0.12}, CreatedAt: "2026-08-19T12:00:00Z", StartedAt: stringPointer("2026-08-19T12:00:01Z"), FinishedAt: stringPointer("2026-08-19T12:02:00Z"),
		}},
		json: `[{"ID":"run-1","ProjectID":7,"SandboxID":"sandbox-1","Role":"coding","Subject":{"Kind":"issue","ID":"issue-1"},"Engine":"opencode","Agent":"coder","Variant":"fast","SessionMode":"fresh","Status":"failed","Round":2,"Error":"agent exited","Usage":{"TokensIn":12,"TokensOut":34,"CostUSD":0.12},"CreatedAt":"2026-08-19T12:00:00Z","StartedAt":"2026-08-19T12:00:01Z","FinishedAt":"2026-08-19T12:02:00Z"}]`,
	},
	"AgentRun": contractFixture{
		value: AgentRun{ID: "run-1", ProjectID: 7, SandboxID: "sandbox-1", Role: "coding", Subject: AgentSubject{Kind: "issue", ID: "issue-1"}, Engine: "opencode", Agent: "coder", Variant: "fast", SessionMode: "fresh", Status: "running", Round: 2, Usage: RunUsage{TokensIn: 12, TokensOut: 34, CostUSD: 0.12}, CreatedAt: "2026-08-19T12:00:00Z", StartedAt: stringPointer("2026-08-19T12:00:01Z")},
		json:  `{"ID":"run-1","ProjectID":7,"SandboxID":"sandbox-1","Role":"coding","Subject":{"Kind":"issue","ID":"issue-1"},"Engine":"opencode","Agent":"coder","Variant":"fast","SessionMode":"fresh","Status":"running","Round":2,"Error":"","Usage":{"TokensIn":12,"TokensOut":34,"CostUSD":0.12},"CreatedAt":"2026-08-19T12:00:00Z","StartedAt":"2026-08-19T12:00:01Z","FinishedAt":null}`,
	},
	"RunOutputPage": contractFixture{
		value: RunOutputPage{Entries: []TranscriptEntry{{Kind: 0, Tool: "", CallID: "call-1", Text: "finished step"}}, Next: 128},
		json:  `{"Entries":[{"Kind":0,"Tool":"","CallID":"call-1","Text":"finished step"}],"Next":128}`,
	},
	"AgentActivityResponse": contractFixture{
		value: AgentActivityResponse{Sizes: map[string]int64{"run-1": 128, "run-2": 256}},
		json:  `{"sizes":{"run-1":128,"run-2":256}}`,
	},
	"CancelAgentRunResponse": CancelAgentRunResponse{Cancelled: true},
	"ListSandboxesResponse": contractFixture{
		value: ListSandboxesResponse{{ID: "sandbox-1", ProjectID: 7, Name: "demo-sandbox", Role: "coding", Subject: AgentSubject{Kind: "issue", ID: "issue-1"}, Status: "running", CreatedAt: "2026-08-19T12:00:00Z", UpdatedAt: "2026-08-19T12:01:00Z"}},
		json:  `[{"ID":"sandbox-1","ProjectID":7,"Name":"demo-sandbox","Role":"coding","Subject":{"Kind":"issue","ID":"issue-1"},"Status":"running","CreatedAt":"2026-08-19T12:00:00Z","UpdatedAt":"2026-08-19T12:01:00Z"}]`,
	},
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
	"RunIssueRequest": RunIssueRequest{Project: "demo", Epic: "epic-1", Issue: "issue-1"},
	"RunIssueResponse": RunIssueResponse{Run: AgentRun{
		ID: "run-1", ProjectID: 7, Role: "coding", Subject: AgentSubject{Kind: "issue", ID: "issue-1"}, Engine: "opencode", Agent: "coder", Variant: "fast", SessionMode: "fresh", Status: "queued", Round: 1, CreatedAt: "2026-08-19T12:00:00Z", StartedAt: stringPointer("2026-08-19T12:00:01Z"),
	}},
	"OpenPullRequestsRequest":       OpenPullRequestsRequest{Project: "demo"},
	"OpenPullRequestsResponse":      contractListFixture(OpenPullRequestsResponse{PullRequests: []PullRequest{contractPullRequest}}, "pull_requests", pullRequestFixtureJSON),
	"TransitionPullRequestRequest":  TransitionPullRequestRequest{Project: "demo", PullRequest: "pr-1", Status: "approved"},
	"TransitionPullRequestResponse": contractObjectFixture(TransitionPullRequestResponse{PullRequest: contractPullRequest}, "pull_request", pullRequestFixtureJSON),
	"ReconcileRequest":              ReconcileRequest{Project: "demo"},
	"ReconcileResponse":             ReconcileResponse{Reconciled: 4},
	"PurgeRequest":                  PurgeRequest{Project: "demo"},
	"PurgeResponse":                 PurgeResponse{Purged: 2},
	"ShapeBody":                     contractBodyFixture{Name: "new"},
}

var contractPathDTOs = map[string]any{
	"ShapePath":            contractPathFixture{Project: "demo/blue"},
	"ProjectPath":          ProjectPath{ProjectID: 42},
	"EpicPath":             EpicPath{ProjectID: 42, EpicID: "epic/one"},
	"GetAgentSettingsPath": GetAgentSettingsPath{ProjectID: 7},
	"SetAgentRolePath":     SetAgentRolePath{ProjectID: 7, Role: "coding/reviewer"},
	"ListAgentRunsPath":    ListAgentRunsPath{ProjectID: 7},
	"GetAgentRunPath":      GetAgentRunPath{RunID: "run/1"},
	"RunOutputPath":        RunOutputPath{RunID: "run/1"},
	"CancelAgentRunPath":   CancelAgentRunPath{RunID: "run/1"},
	"ListSandboxesPath":    ListSandboxesPath{ProjectID: 7},
}

var contractQueryDTOs = map[string]url.Values{
	"ShapeQuery":         {"runID": {"run-1", "run-2"}, "from": {"12"}},
	"RunOutputQuery":     {"from": {"12"}},
	"AgentActivityQuery": {"runID": {"run-1", "run-2"}},
}

func stringPointer(value string) *string { return &value }

func TestRepresentativeDTOJSON(t *testing.T) {
	request := CreateProjectRequest{Name: "demo", Description: "a project"}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	const wantRequest = `{"name":"demo","description":"a project"}`
	if string(encoded) != wantRequest {
		t.Fatalf("CreateProjectRequest JSON = %s, want %s", encoded, wantRequest)
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
