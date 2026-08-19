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
	"ProcessResponse":      ProcessResponse{},
	"ListProjectsResponse": ListProjectsResponse{}, "CreateProjectRequest": CreateProjectRequest{}, "CreateProjectResponse": CreateProjectResponse{},
	"OpenProjectRequest": OpenProjectRequest{}, "OpenProjectResponse": OpenProjectResponse{}, "ForgetProjectRequest": ForgetProjectRequest{}, "ForgetProjectResponse": ForgetProjectResponse{},
	"ProjectSummariesRequest": ProjectSummariesRequest{}, "ProjectSummariesResponse": ProjectSummariesResponse{}, "GetSetupRequest": GetSetupRequest{}, "GetSetupResponse": GetSetupResponse{},
	"SaveSetupRequest": SaveSetupRequest{}, "SaveSetupResponse": SaveSetupResponse{}, "ListEpicsRequest": ListEpicsRequest{}, "ListEpicsResponse": ListEpicsResponse{},
	"GetEpicRequest": GetEpicRequest{}, "GetEpicResponse": GetEpicResponse{}, "CreateEpicRequest": CreateEpicRequest{}, "CreateEpicResponse": CreateEpicResponse{},
	"PrefixEpicRequest": PrefixEpicRequest{}, "PrefixEpicResponse": PrefixEpicResponse{}, "TransitionEpicRequest": TransitionEpicRequest{}, "TransitionEpicResponse": TransitionEpicResponse{},
	"CloseEpicRequest": CloseEpicRequest{}, "CloseEpicResponse": CloseEpicResponse{}, "ListIssuesRequest": ListIssuesRequest{}, "ListIssuesResponse": ListIssuesResponse{},
	"GetIssueRequest": GetIssueRequest{}, "GetIssueResponse": GetIssueResponse{}, "CreateIssueRequest": CreateIssueRequest{}, "CreateIssueResponse": CreateIssueResponse{},
	"UpdateIssueRequest": UpdateIssueRequest{}, "UpdateIssueResponse": UpdateIssueResponse{}, "TransitionIssueRequest": TransitionIssueRequest{}, "TransitionIssueResponse": TransitionIssueResponse{},
	"CloseIssueRequest": CloseIssueRequest{}, "CloseIssueResponse": CloseIssueResponse{}, "ListPullRequestsRequest": ListPullRequestsRequest{}, "ListPullRequestsResponse": ListPullRequestsResponse{},
	"CreatePullRequestRequest": CreatePullRequestRequest{}, "CreatePullRequestResponse": CreatePullRequestResponse{}, "CommentPullRequestRequest": CommentPullRequestRequest{}, "CommentPullRequestResponse": CommentPullRequestResponse{},
	"MergePullRequestRequest": MergePullRequestRequest{}, "MergePullRequestResponse": MergePullRequestResponse{}, "ClosePullRequestRequest": ClosePullRequestRequest{}, "ClosePullRequestResponse": ClosePullRequestResponse{},
	"ResetPullRequestRequest": ResetPullRequestRequest{}, "ResetPullRequestResponse": ResetPullRequestResponse{}, "GrantPullRequestRequest": GrantPullRequestRequest{}, "GrantPullRequestResponse": GrantPullRequestResponse{},
	"PullRequestDiffRequest": PullRequestDiffRequest{}, "PullRequestDiffResponse": PullRequestDiffResponse{}, "ListRepositoriesRequest": ListRepositoriesRequest{}, "ListRepositoriesResponse": ListRepositoriesResponse{},
	"GetRepositoryRequest": GetRepositoryRequest{}, "GetRepositoryResponse": GetRepositoryResponse{}, "ListOrganisationsRequest": ListOrganisationsRequest{}, "ListOrganisationsResponse": ListOrganisationsResponse{},
	"GetOrganisationRequest": GetOrganisationRequest{}, "GetOrganisationResponse": GetOrganisationResponse{}, "GetAgentSettingsRequest": GetAgentSettingsRequest{}, "GetAgentSettingsResponse": GetAgentSettingsResponse{},
	"ListAgentRunsRequest": ListAgentRunsRequest{}, "ListAgentRunsResponse": ListAgentRunsResponse{}, "ListSandboxesRequest": ListSandboxesRequest{}, "ListSandboxesResponse": ListSandboxesResponse{},
	"CancelAgentRunRequest": CancelAgentRunRequest{}, "CancelAgentRunResponse": CancelAgentRunResponse{}, "AgentActivityRequest": AgentActivityRequest{}, "AgentActivityResponse": AgentActivityResponse{},
	"RunOutputRequest": RunOutputRequest{}, "RunOutputResponse": RunOutputResponse{}, "HealthResponse": HealthResponse{}, "CapabilitiesResponse": CapabilitiesResponse{},
	"AddRepositoryRequest": AddRepositoryRequest{}, "AddRepositoryResponse": AddRepositoryResponse{}, "GetAgentRunRequest": GetAgentRunRequest{}, "GetAgentRunResponse": GetAgentRunResponse{},
	"CompleteRequest": CompleteRequest{}, "CompleteResponse": CompleteResponse{}, "ReviewApprovedBranchesRequest": ReviewApprovedBranchesRequest{}, "ReviewApprovedBranchesResponse": ReviewApprovedBranchesResponse{},
	"RunEpicRequest": RunEpicRequest{}, "RunEpicResponse": RunEpicResponse{}, "RunIssueRequest": RunIssueRequest{}, "RunIssueResponse": RunIssueResponse{},
	"OpenPullRequestsRequest": OpenPullRequestsRequest{}, "OpenPullRequestsResponse": OpenPullRequestsResponse{}, "TransitionPullRequestRequest": TransitionPullRequestRequest{}, "TransitionPullRequestResponse": TransitionPullRequestResponse{},
	"ReconcileRequest": ReconcileRequest{}, "ReconcileResponse": ReconcileResponse{}, "PurgeRequest": PurgeRequest{}, "PurgeResponse": PurgeResponse{},
	"ReadDaemonLogRequest": ReadDaemonLogRequest{}, "ReadDaemonLogResponse": ReadDaemonLogResponse{},
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
