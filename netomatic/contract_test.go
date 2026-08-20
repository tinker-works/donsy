package netomatic

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"testing"
)

func TestContractMatchesGoMergeRouteInventory(t *testing.T) {
	want := []Operation{
		{Name: "Process", Method: MethodGet, Route: APIPrefix + "/process", Response: "ProcessResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "Capabilities", Method: MethodGet, Route: APIPrefix + "/capabilities", Response: "CapabilitiesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "ListProjects", Method: MethodGet, Route: APIPrefix + "/projects", Response: "ListProjectsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "CreateProject", Method: MethodPost, Route: APIPrefix + "/projects", Request: "CreateProjectRequest", Response: "CreateProjectResponse", SuccessStatus: http.StatusCreated, Authenticated: true},
		{Name: "ListProjectSummaries", Method: MethodGet, Route: APIPrefix + "/projects/summaries", Response: "ListProjectSummariesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "OpenProject", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/open", Path: "ProjectPath", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "ForgetProject", Method: MethodDelete, Route: APIPrefix + "/projects/{projectID}", Path: "ProjectPath", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "StoreSetup", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/setup", Path: "ProjectPath", Response: "SetupState", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "InitialiseStore", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/setup", Path: "ProjectPath", Request: "InitialiseStoreRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "ListEpics", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/epics", Path: "ProjectPath", Response: "ListEpicsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "GetEpic", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}", Path: "EpicPath", Response: "Epic", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "CreateEpic", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics", Path: "EpicPath", Request: "CreateEpicRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "CloseEpic", Method: MethodDelete, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}", Path: "EpicPath", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "TransitionEpicState", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/state-transitions", Path: "EpicPath", Request: "TransitionEpicStateRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "SetBranchPrefix", Method: MethodPut, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/branch-prefix", Path: "EpicPath", Request: "SetBranchPrefixRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "CompleteEpic", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/complete", Path: "EpicPath", Response: "CompleteEpicResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "ReviewApprovedBranches", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/review-approved-branches", Path: "EpicPath", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "RunEpicAgent", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/agent-runs", Path: "EpicPath", Unavailable: true, Authenticated: true},
		{Name: "CreateIssue", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/issues", Path: "CreateIssuePath", Request: "CreateIssueRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "CloseIssue", Method: MethodDelete, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/issues/{issueID}", Path: "CloseIssuePath", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "RunIssueAgent", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/issues/{issueID}/agent-runs", Path: "RunIssueAgentPath", Unavailable: true, Authenticated: true},
		{Name: "CreatePullRequest", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests", Path: "CreatePullRequestPath", Request: "CreatePullRequestRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "TransitionPullRequest", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/state-transitions", Path: "TransitionPullRequestPath", Request: "TransitionPullRequestRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "GrantCodingRound", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/coding-rounds", Path: "GrantCodingRoundPath", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "MergePullRequest", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/merge", Path: "MergePullRequestPath", Response: "MergePullRequestResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "ResetIssue", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/reset", Path: "ResetIssuePath", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "GetPullRequestDiff", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/diff", Path: "GetPullRequestDiffPath", Response: "PullRequestDiffResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "OpenPullRequests", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/open-pull-requests", Path: "OpenPullRequestsPath", Response: "OpenPullRequestsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "AddComment", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/comments", Path: "AddCommentPath", Request: "AddCommentRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "ListOrganisations", Method: MethodGet, Route: APIPrefix + "/organisations", Response: "ListOrganisationsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "AddOrganisation", Method: MethodPost, Route: APIPrefix + "/organisations", Request: "AddOrganisationRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "RemoveOrganisation", Method: MethodDelete, Route: APIPrefix + "/organisations/{name}", Path: "RemoveOrganisationPath", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "DiscoverOrganisations", Method: MethodPost, Route: APIPrefix + "/organisations/discovery", Response: "DiscoverOrganisationsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "ListRepositories", Method: MethodGet, Route: APIPrefix + "/repositories", Response: "ListRepositoriesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "AddRepository", Method: MethodPost, Route: APIPrefix + "/repositories", Request: "AddRepositoryRequest", Response: "Repository", SuccessStatus: http.StatusCreated, Authenticated: true},
		{Name: "SyncRepositories", Method: MethodPost, Route: APIPrefix + "/repositories/sync", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "ListProjectRepositories", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/repositories", Path: "ListProjectRepositoriesPath", Response: "ListProjectRepositoriesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "UpdateProjectRepositories", Method: MethodPut, Route: APIPrefix + "/projects/{projectID}/repositories", Path: "UpdateProjectRepositoriesPath", Request: "UpdateProjectRepositoriesRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "GetAgentSettings", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/agent-settings", Path: "GetAgentSettingsPath", Response: "AgentSettings", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "SetAgentRole", Method: MethodPut, Route: APIPrefix + "/projects/{projectID}/agent-settings/roles/{role}", Path: "SetAgentRolePath", Request: "SetAgentRoleRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
		{Name: "ListAgentRuns", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/agent-runs", Path: "ListAgentRunsPath", Response: "ListAgentRunsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "GetAgentRun", Method: MethodGet, Route: APIPrefix + "/agent-runs/{runID}", Path: "GetAgentRunPath", Response: "AgentRun", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "RunOutput", Method: MethodGet, Route: APIPrefix + "/agent-runs/{runID}/output", Path: "RunOutputPath", Query: "RunOutputQuery", Response: "RunOutputPage", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "AgentActivity", Method: MethodGet, Route: APIPrefix + "/agent-runs/activity", Query: "AgentActivityQuery", Response: "AgentActivityResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "CancelAgentRun", Method: MethodPost, Route: APIPrefix + "/agent-runs/{runID}/cancel", Path: "CancelAgentRunPath", Response: "CancelAgentRunResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "ListSandboxes", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/sandboxes", Path: "ListSandboxesPath", Response: "ListSandboxesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
		{Name: "ReconcileSandboxes", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/maintenance/reconcile", Path: "ProjectPath", Unavailable: true, Authenticated: true},
		{Name: "PurgeFinishedWork", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/maintenance/purge", Path: "ProjectPath", Unavailable: true, Authenticated: true},
	}
	if !reflect.DeepEqual(Contract, want) {
		t.Fatalf("contract differs from reviewed Go Merge inventory:\n got: %#v\nwant: %#v", Contract, want)
	}
	if err := ValidateContract(); err != nil {
		t.Fatal(err)
	}
}

func TestContractOperationsReturnsCopy(t *testing.T) {
	operations := ContractOperations()
	operations[0].Name = "changed"
	if Contract[0].Name == "changed" {
		t.Fatal("ContractOperations returned the contract backing array")
	}
}

func TestOperationValidation(t *testing.T) {
	valid := Operation{Name: "Close", Method: MethodDelete, Route: APIPrefix + "/items/{item}", SuccessStatus: http.StatusNoContent}
	if err := validateOperation(valid); err != nil {
		t.Fatal(err)
	}
	if err := validateOperation(Operation{Name: "Unavailable", Method: MethodPost, Route: APIPrefix + "/items", Unavailable: true}); err != nil {
		t.Fatal(err)
	}
	for _, operation := range []Operation{
		{Method: MethodGet, Route: APIPrefix + "/items", SuccessStatus: http.StatusOK},
		{Name: "MissingMethod", Route: APIPrefix + "/items", SuccessStatus: http.StatusOK},
		{Name: "MissingRoute", Method: MethodGet, SuccessStatus: http.StatusOK},
		{Name: "BadStatus", Method: MethodGet, Route: APIPrefix + "/items", SuccessStatus: http.StatusMultipleChoices},
		{Name: "UnavailableStatus", Method: MethodGet, Route: APIPrefix + "/items", Unavailable: true, SuccessStatus: http.StatusOK},
	} {
		if err := validateOperation(operation); err == nil {
			t.Fatalf("validateOperation accepted %#v", operation)
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

const (
	commentFixtureJSON      = `{"ID":"comment-1","Author":"reviewer","CreatedAt":"2026-08-19T12:01:00Z","Body":"Please review"}`
	issueFixtureJSON        = `{"ID":"issue-1","Title":"First issue","ParentID":"parent-1","Repository":"origin","State":"Open","CreatedAt":"2026-08-19T12:00:00Z","Body":"Implement it","Comments":[` + commentFixtureJSON + `],"BlockedBy":["issue-0"]}`
	pullRequestFixtureJSON  = `{"ID":"pr-1","IssueID":"issue-1","Title":"Implement issue","Status":"open","Repository":"origin","Number":7,"URL":"https://example.test/pr/7","Head":"feature/issue-1","Base":"main","Flags":["stale","human-needed"],"ReviewedHead":"abc123","ReviewedBase":"def456","Rounds":2,"Reviews":1,"RoundsGranted":1,"CodingRounds":2,"Approved":true,"CreatedAt":"2026-08-19T12:02:00Z","Comments":[` + commentFixtureJSON + `]}`
	epicFixtureJSON         = `{"ID":"epic-1","Title":"First epic","Assignee":"alice","Repositories":["origin","secondary"],"Body":"Plan the release","State":"concept","BranchPrefix":"jira-123","Issues":[` + issueFixtureJSON + `],"PullRequests":[` + pullRequestFixtureJSON + `],"DraftingPasses":2}`
	organisationFixtureJSON = `{"Name":"acme"}`
	repositoryFixtureJSON   = `{"Name":"donsy","FullName":"acme/donsy","HTTPURL":"https://example.test/donsy","SSHURL":"git@example.test:acme/donsy.git","Organisation":"acme"}`
	agentSubjectFixtureJSON = `{"Kind":"epic","ID":"epic-1"}`
	agentRunFixtureJSON     = `{"ID":"run-1","ProjectID":7,"SandboxID":"sandbox-1","Role":"coding","Subject":` + agentSubjectFixtureJSON + `,"Engine":"opencode","Agent":"coder","Variant":"fast","SessionMode":"fresh","Status":"running","Round":2,"Error":"","Usage":{"TokensIn":12,"TokensOut":34,"CostUSD":0.25},"CreatedAt":"2026-08-19T12:00:00Z","StartedAt":"2026-08-19T12:01:00Z","FinishedAt":null}`
	sandboxFixtureJSON      = `{"ID":"sandbox-1","ProjectID":7,"Name":"coding-epic-1","Role":"coding","Subject":` + agentSubjectFixtureJSON + `,"Status":"running","CreatedAt":"2026-08-19T11:59:00Z","UpdatedAt":"2026-08-19T12:01:00Z"}`
)

var (
	contractComment     = Comment{ID: "comment-1", Author: "reviewer", CreatedAt: "2026-08-19T12:01:00Z", Body: "Please review"}
	contractIssue       = Issue{ID: "issue-1", Title: "First issue", ParentID: "parent-1", Repository: "origin", State: "Open", CreatedAt: "2026-08-19T12:00:00Z", Body: "Implement it", Comments: []Comment{contractComment}, BlockedBy: []string{"issue-0"}}
	contractPullRequest = PullRequest{ID: "pr-1", IssueID: "issue-1", Title: "Implement issue", Status: "open", Repository: "origin", Number: 7, URL: "https://example.test/pr/7", Head: "feature/issue-1", Base: "main", Flags: []string{"stale", "human-needed"}, ReviewedHead: "abc123", ReviewedBase: "def456", Rounds: 2, Reviews: 1, RoundsGranted: 1, CodingRounds: 2, Approved: true, CreatedAt: "2026-08-19T12:02:00Z", Comments: []Comment{contractComment}}
	contractEpic        = Epic{ID: "epic-1", Title: "First epic", Assignee: "alice", Repositories: []string{"origin", "secondary"}, Body: "Plan the release", State: "concept", BranchPrefix: "jira-123", Issues: []Issue{contractIssue}, PullRequests: []PullRequest{contractPullRequest}, DraftingPasses: 2}
)

var contractDTOs = map[string]any{
	"ProcessResponse":                  contractFixture{value: ProcessResponse{CurrentUser: "octocat", Protocol: ProtocolVersion}, json: `{"currentUser":"octocat","protocol":"v1"}`},
	"CapabilitiesResponse":             contractFixture{value: CapabilitiesResponse{"runEpicAgent": false, "readRunOutput": true}, json: `{"readRunOutput":true,"runEpicAgent":false}`},
	"ListProjectsResponse":             contractFixture{value: ListProjectsResponse{{ID: 7, Name: "demo", LastOpenedAt: "2026-08-19T12:00:00Z"}}, json: `[{"ID":7,"Name":"demo","LastOpenedAt":"2026-08-19T12:00:00Z"}]`},
	"CreateProjectRequest":             CreateProjectRequest{Name: "demo"},
	"CreateProjectResponse":            contractFixture{value: CreateProjectResponse{ID: 7, Name: "demo", LastOpenedAt: "2026-08-19T12:00:00Z"}, json: `{"ID":7,"Name":"demo","LastOpenedAt":"2026-08-19T12:00:00Z"}`},
	"ListProjectSummariesResponse":     contractFixture{value: ListProjectSummariesResponse{{Project: Project{ID: 7, Name: "demo", LastOpenedAt: "2026-08-19T12:00:00Z"}, Epics: 2, Running: 1}}, json: `[{"project":{"ID":7,"Name":"demo","LastOpenedAt":"2026-08-19T12:00:00Z"},"epics":2,"running":1}]`},
	"SetupState":                       contractFixture{value: SetupState{Organisations: 1, Repositories: 2, RolesSet: 4, RolesTotal: 5}, json: `{"Organisations":1,"Repositories":2,"RolesSet":4,"RolesTotal":5}`},
	"InitialiseStoreRequest":           InitialiseStoreRequest{Model: "small", Variant: "fast", Repositories: []string{"acme/donsy", "acme/other"}},
	"ListEpicsResponse":                contractFixture{value: ListEpicsResponse{contractEpic}, json: `[` + epicFixtureJSON + `]`},
	"Epic":                             contractFixture{value: contractEpic, json: epicFixtureJSON},
	"CreateEpicRequest":                CreateEpicRequest{Title: "First epic", Assignee: "alice", Body: "Plan the release", Repositories: []string{"origin"}, BranchPrefix: "jira-123"},
	"TransitionEpicStateRequest":       TransitionEpicStateRequest{State: "review", Force: true},
	"SetBranchPrefixRequest":           SetBranchPrefixRequest{Prefix: "EP-1"},
	"CompleteEpicResponse":             CompleteEpicResponse{Completed: true},
	"CreateIssueRequest":               CreateIssueRequest{ParentID: "parent-1", Title: "First issue", Body: "Implement it", Repository: "origin"},
	"CreatePullRequestRequest":         contractFixture{value: CreatePullRequestRequest{IssueID: "issue-1", Title: "Implement issue", Repository: "origin", Head: "feature/issue-1", Base: "main"}, json: `{"issueId":"issue-1","title":"Implement issue","repository":"origin","head":"feature/issue-1","base":"main"}`},
	"TransitionPullRequestRequest":     contractFixture{value: TransitionPullRequestRequest{Status: "closed"}, json: `{"status":"closed"}`},
	"MergePullRequestResponse":         contractFixture{value: MergePullRequestResponse{Outcome: MergeOutcomeMerged}, json: `{"outcome":"merged"}`},
	"PullRequestDiffResponse":          contractFixture{value: PullRequestDiffResponse{Diff: "@@ -1 +1 @@\n-old\n+new\n"}, json: `{"diff":"@@ -1 +1 @@\n-old\n+new\n"}`},
	"OpenPullRequestsResponse":         contractFixture{value: OpenPullRequestsResponse{Opened: 2}, json: `{"opened":2}`},
	"AddCommentRequest":                AddCommentRequest{TargetID: "issue-1", Target: IssueCommentTarget, Body: "Please review"},
	"ListOrganisationsResponse":        contractFixture{value: ListOrganisationsResponse{{Name: "acme"}}, json: `[` + organisationFixtureJSON + `]`},
	"AddOrganisationRequest":           AddOrganisationRequest{Name: "acme"},
	"DiscoverOrganisationsResponse":    contractFixture{value: DiscoverOrganisationsResponse{{Name: "acme"}}, json: `[` + organisationFixtureJSON + `]`},
	"ListRepositoriesResponse":         contractFixture{value: ListRepositoriesResponse{{Name: "donsy", FullName: "acme/donsy", HTTPURL: "https://example.test/donsy", SSHURL: "git@example.test:acme/donsy.git", Organisation: "acme"}}, json: `[` + repositoryFixtureJSON + `]`},
	"AddRepositoryRequest":             AddRepositoryRequest{FullName: "acme/donsy"},
	"Repository":                       contractFixture{value: Repository{Name: "donsy", FullName: "acme/donsy", HTTPURL: "https://example.test/donsy", SSHURL: "git@example.test:acme/donsy.git", Organisation: "acme"}, json: repositoryFixtureJSON},
	"ListProjectRepositoriesResponse":  contractFixture{value: ListProjectRepositoriesResponse{"acme/donsy", "acme/other"}, json: `["acme/donsy","acme/other"]`},
	"UpdateProjectRepositoriesRequest": UpdateProjectRepositoriesRequest{Repositories: []string{"acme/donsy", "acme/other"}},
	"AgentSettings":                    contractFixture{value: AgentSettings{SetupScript: "setup.sh", Roles: map[string]AgentProfile{"coding": {Agent: "coder", Variant: "fast", MaxRounds: 3}}}, json: `{"SetupScript":"setup.sh","Roles":{"coding":{"Agent":"coder","Variant":"fast","MaxRounds":3}}}`},
	"SetAgentRoleRequest":              SetAgentRoleRequest{Agent: "coder", Variant: "fast"},
	"ListAgentRunsResponse":            contractFixture{value: ListAgentRunsResponse{contractAgentRun}, json: `[` + agentRunFixtureJSON + `]`},
	"AgentRun":                         contractFixture{value: contractAgentRun, json: agentRunFixtureJSON},
	"RunOutputPage":                    contractFixture{value: RunOutputPage{Entries: []TranscriptEntry{{Kind: 0, Tool: "", CallID: "", Text: "finished"}}, Next: 128}, json: `{"Entries":[{"Kind":0,"Tool":"","CallID":"","Text":"finished"}],"Next":128}`},
	"AgentActivityResponse":            contractFixture{value: AgentActivityResponse{Sizes: map[string]int64{"run-1": 128}}, json: `{"sizes":{"run-1":128}}`},
	"CancelAgentRunResponse":           contractFixture{value: CancelAgentRunResponse{Cancelled: true}, json: `{"cancelled":true}`},
	"ListSandboxesResponse":            contractFixture{value: ListSandboxesResponse{contractSandbox}, json: `[` + sandboxFixtureJSON + `]`},
}

var contractAgentRun = AgentRun{ID: "run-1", ProjectID: 7, SandboxID: "sandbox-1", Role: "coding", Subject: AgentSubject{Kind: "epic", ID: "epic-1"}, Engine: "opencode", Agent: "coder", Variant: "fast", SessionMode: "fresh", Status: "running", Round: 2, Usage: RunUsage{TokensIn: 12, TokensOut: 34, CostUSD: 0.25}, CreatedAt: "2026-08-19T12:00:00Z", StartedAt: stringPointer("2026-08-19T12:01:00Z")}
var contractSandbox = Sandbox{ID: "sandbox-1", ProjectID: 7, Name: "coding-epic-1", Role: "coding", Subject: AgentSubject{Kind: "epic", ID: "epic-1"}, Status: "running", CreatedAt: "2026-08-19T11:59:00Z", UpdatedAt: "2026-08-19T12:01:00Z"}

func stringPointer(value string) *string { return &value }

var contractPathDTOs = map[string]any{
	"ProjectPath":                   ProjectPath{ProjectID: 7},
	"EpicPath":                      EpicPath{ProjectID: 7, EpicID: "epic-1"},
	"IssuePath":                     IssuePath{ProjectID: 7, EpicID: "epic-1", IssueID: "issue-1"},
	"CreateIssuePath":               EpicPath{ProjectID: 7, EpicID: "epic-1"},
	"CloseIssuePath":                IssuePath{ProjectID: 7, EpicID: "epic-1", IssueID: "issue-1"},
	"RunIssueAgentPath":             IssuePath{ProjectID: 7, EpicID: "epic-1", IssueID: "issue-1"},
	"CreatePullRequestPath":         CreatePullRequestPath{ProjectID: 7, EpicID: "epic-1"},
	"TransitionPullRequestPath":     TransitionPullRequestPath{ProjectID: 7, EpicID: "epic-1", PullRequestID: "pr-1"},
	"GrantCodingRoundPath":          GrantCodingRoundPath{ProjectID: 7, EpicID: "epic-1", PullRequestID: "pr-1"},
	"MergePullRequestPath":          MergePullRequestPath{ProjectID: 7, EpicID: "epic-1", PullRequestID: "pr-1"},
	"ResetIssuePath":                ResetIssuePath{ProjectID: 7, EpicID: "epic-1", PullRequestID: "pr-1"},
	"GetPullRequestDiffPath":        GetPullRequestDiffPath{ProjectID: 7, EpicID: "epic-1", PullRequestID: "pr-1"},
	"OpenPullRequestsPath":          OpenPullRequestsPath{ProjectID: 7, EpicID: "epic-1"},
	"AddCommentPath":                AddCommentPath{ProjectID: 7, EpicID: "epic-1"},
	"RemoveOrganisationPath":        RemoveOrganisationPath{Name: "acme/team"},
	"ListProjectRepositoriesPath":   ProjectPath{ProjectID: 7},
	"UpdateProjectRepositoriesPath": ProjectPath{ProjectID: 7},
	"SetAgentRolePath":              SetAgentRolePath{ProjectID: 7, Role: "coding"},
	"GetAgentSettingsPath":          ProjectPath{ProjectID: 7},
	"ListAgentRunsPath":             ProjectPath{ProjectID: 7},
	"ListSandboxesPath":             ProjectPath{ProjectID: 7},
	"AgentRunPath":                  AgentRunPath{RunID: "run-1"},
	"GetAgentRunPath":               AgentRunPath{RunID: "run-1"},
	"RunOutputPath":                 AgentRunPath{RunID: "run-1"},
	"CancelAgentRunPath":            AgentRunPath{RunID: "run-1"},
}

var contractQueryDTOs = map[string]url.Values{
	"RunOutputQuery":     {"from": {"128"}},
	"AgentActivityQuery": {"runID": {"run-1", "run-2"}},
}

func TestProtocolAndAPIError(t *testing.T) {
	if !CompatibleProtocol(ProtocolVersion) || CompatibleProtocol("v1.1") || CompatibleProtocol("") {
		t.Fatal("protocol compatibility must require exact v1")
	}
	if err := ValidateProtocol("v2"); !errors.Is(err, ErrInvalidProtocol) {
		t.Fatalf("ValidateProtocol(v2) = %v", err)
	}
	apiError := &APIError{Code: ErrorUnauthorized, Detail: "bad token", StatusCode: http.StatusUnauthorized}
	if apiError.Error() != "bad token" || !errors.Is(apiError, ErrUnauthorized) {
		t.Fatal("APIError should expose its detail and status error")
	}
}
