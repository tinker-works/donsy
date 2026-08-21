package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/application/usecases"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
	"github.com/tinker-works/donsy/netomatic"
)

func TestHTTPServerReportsDocumentedUnavailableOperations(t *testing.T) {
	server, err := New(&usecases.UseCases{CurrentUser: "octocat"}, nil, "token")
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	client, err := netomatic.NewHTTPClient(testServer.URL, "token")
	if err != nil {
		t.Fatal(err)
	}

	for _, operation := range netomatic.ContractOperations() {
		if operation.Unavailable {
			t.Run(operation.Name, func(t *testing.T) {
				err := callOperation(client, operation.Name)
				var apiError *netomatic.APIError
				if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusNotImplemented || apiError.Code != netomatic.ErrorFeatureNotConfigured {
					t.Fatalf("%s() error = %#v, want feature_not_configured", operation.Name, err)
				}
			})
		}
	}
}

func TestServerConfiguresHostAndOriginForAdvertisedEndpoint(t *testing.T) {
	server, err := New(&usecases.UseCases{CurrentUser: "octocat"}, nil, "token")
	if err != nil {
		t.Fatal(err)
	}
	if err := server.ConfigureEndpoint("http://localhost:9123"); err != nil {
		t.Fatal(err)
	}
	if got := server.HTTPServer(context.Background()).Addr; got != "localhost:9123" {
		t.Fatalf("HTTP server address = %q, want %q", got, "localhost:9123")
	}

	request := httptest.NewRequest(http.MethodPost, "http://localhost:9123/api/v1/projects", strings.NewReader(`{"name":"demo"}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://localhost:9123")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("allowed endpoint response = %d, want %d", response.Code, http.StatusNotImplemented)
	}

	request = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:9123/api/v1/process", nil)
	request.Header.Set("Authorization", "Bearer token")
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("wrong Host response = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestHTTPServerInteroperatesWithEveryContractOperation(t *testing.T) {
	available := map[string]func(*netomatic.HTTPClient) error{
		"Process":      func(c *netomatic.HTTPClient) error { _, err := c.Process(context.Background()); return err },
		"ListProjects": func(c *netomatic.HTTPClient) error { _, err := c.ListProjects(context.Background()); return err },
		"CreateProject": func(c *netomatic.HTTPClient) error {
			_, err := c.CreateProject(context.Background(), netomatic.CreateProjectRequest{Name: "extra"})
			return err
		},
		"OpenProject": func(c *netomatic.HTTPClient) error {
			return c.OpenProject(context.Background(), netomatic.ProjectPath{ProjectID: 1})
		},
		"ForgetProject": func(c *netomatic.HTTPClient) error {
			return c.ForgetProject(context.Background(), netomatic.ProjectPath{ProjectID: 1})
		},
		"ListProjectSummaries": func(c *netomatic.HTTPClient) error {
			_, err := c.ListProjectSummaries(context.Background())
			return err
		},
		"StoreSetup": func(c *netomatic.HTTPClient) error {
			_, err := c.StoreSetup(context.Background(), netomatic.ProjectPath{ProjectID: 1})
			return err
		},
		"InitialiseStore": func(c *netomatic.HTTPClient) error {
			return c.InitialiseStore(context.Background(), netomatic.ProjectPath{ProjectID: 1}, netomatic.InitialiseStoreRequest{Model: "openai/gpt"})
		},
		"ListEpics": func(c *netomatic.HTTPClient) error {
			_, err := c.ListEpics(context.Background(), netomatic.ListEpicsRequest{Project: "demo"})
			return err
		},
		"GetEpic": func(c *netomatic.HTTPClient) error {
			_, err := c.GetEpic(context.Background(), netomatic.GetEpicRequest{Project: "demo", Epic: "epic"})
			return err
		},
		"CreateEpic": func(c *netomatic.HTTPClient) error {
			_, err := c.CreateEpic(context.Background(), netomatic.CreateEpicRequest{Project: "demo", Title: "New epic"})
			return err
		},
		"PrefixEpic": func(c *netomatic.HTTPClient) error {
			_, err := c.PrefixEpic(context.Background(), netomatic.PrefixEpicRequest{Project: "demo", Epic: "epic", Prefix: "work"})
			return err
		},
		"TransitionEpic": func(c *netomatic.HTTPClient) error {
			_, err := c.TransitionEpic(context.Background(), netomatic.TransitionEpicRequest{Project: "demo", Epic: "epic", Status: string(epicpkg.EpicStateRefine)})
			return err
		},
		"CloseEpic": func(c *netomatic.HTTPClient) error {
			_, err := c.CloseEpic(context.Background(), netomatic.CloseEpicRequest{Project: "demo", Epic: "epic"})
			return err
		},
		"ListIssues": func(c *netomatic.HTTPClient) error {
			_, err := c.ListIssues(context.Background(), netomatic.ListIssuesRequest{Project: "demo", Epic: "epic"})
			return err
		},
		"GetIssue": func(c *netomatic.HTTPClient) error {
			_, err := c.GetIssue(context.Background(), netomatic.GetIssueRequest{Project: "demo", Epic: "epic", Issue: "root"})
			return err
		},
		"CreateIssue": func(c *netomatic.HTTPClient) error {
			_, err := c.CreateIssue(context.Background(), netomatic.CreateIssueRequest{Project: "demo", Epic: "epic", Title: "New issue"})
			return err
		},
		"CloseIssue": func(c *netomatic.HTTPClient) error {
			_, err := c.CloseIssue(context.Background(), netomatic.CloseIssueRequest{Project: "demo", Epic: "epic", Issue: "root"})
			return err
		},
		"CreatePullRequest": func(c *netomatic.HTTPClient) error {
			return c.CreatePullRequest(context.Background(), netomatic.CreatePullRequestPath{ProjectID: 1, EpicID: "epic"}, netomatic.CreatePullRequestRequest{IssueID: "root", Title: "New pull request", Repository: "acme/widgets", Head: "feature", Base: "main"})
		},
		"TransitionPullRequest": func(c *netomatic.HTTPClient) error {
			return c.TransitionPullRequest(context.Background(), netomatic.TransitionPullRequestPath{ProjectID: 1, EpicID: "epic", PullRequestID: "pr"}, netomatic.TransitionPullRequestRequest{Status: string(epicpkg.PullRequestClosed)})
		},
		"GrantCodingRound": func(c *netomatic.HTTPClient) error {
			return c.GrantCodingRound(context.Background(), netomatic.GrantCodingRoundPath{ProjectID: 1, EpicID: "epic", PullRequestID: "pr"})
		},
		"MergePullRequest": func(c *netomatic.HTTPClient) error {
			_, err := c.MergePullRequest(context.Background(), netomatic.MergePullRequestPath{ProjectID: 1, EpicID: "epic", PullRequestID: "pr"})
			return err
		},
		"ResetIssue": func(c *netomatic.HTTPClient) error {
			return c.ResetIssue(context.Background(), netomatic.ResetIssuePath{ProjectID: 1, EpicID: "epic", PullRequestID: "pr"})
		},
		"GetPullRequestDiff": func(c *netomatic.HTTPClient) error {
			_, err := c.GetPullRequestDiff(context.Background(), netomatic.GetPullRequestDiffPath{ProjectID: 1, EpicID: "epic", PullRequestID: "pr"})
			return err
		},
		"OpenPullRequests": func(c *netomatic.HTTPClient) error {
			_, err := c.OpenPullRequests(context.Background(), netomatic.OpenPullRequestsPath{ProjectID: 1, EpicID: "epic"})
			return err
		},
		"AddComment": func(c *netomatic.HTTPClient) error {
			return c.AddComment(context.Background(), netomatic.AddCommentPath{ProjectID: 1, EpicID: "epic"}, netomatic.AddCommentRequest{Target: netomatic.IssueCommentTarget, TargetID: "root", Body: "Note"})
		},
		"ListOrganisations": func(c *netomatic.HTTPClient) error { _, err := c.ListOrganisations(context.Background()); return err },
		"AddOrganisation": func(c *netomatic.HTTPClient) error {
			return c.AddOrganisation(context.Background(), netomatic.AddOrganisationRequest{Name: "acme"})
		},
		"RemoveOrganisation": func(c *netomatic.HTTPClient) error {
			return c.RemoveOrganisation(context.Background(), netomatic.RemoveOrganisationPath{Name: "acme"})
		},
		"DiscoverOrganisations": func(c *netomatic.HTTPClient) error {
			_, err := c.DiscoverOrganisations(context.Background())
			return err
		},
		"ListRepositories": func(c *netomatic.HTTPClient) error { _, err := c.ListRepositories(context.Background()); return err },
		"SyncRepositories": func(c *netomatic.HTTPClient) error { return c.SyncRepositories(context.Background()) },
		"ListProjectRepositories": func(c *netomatic.HTTPClient) error {
			_, err := c.ListProjectRepositories(context.Background(), netomatic.ListProjectRepositoriesPath{ProjectID: 1})
			return err
		},
		"UpdateProjectRepositories": func(c *netomatic.HTTPClient) error {
			return c.UpdateProjectRepositories(context.Background(), netomatic.UpdateProjectRepositoriesPath{ProjectID: 1}, netomatic.UpdateProjectRepositoriesRequest{Repositories: []string{"acme/widgets"}})
		},
		"GetAgentSettings": func(c *netomatic.HTTPClient) error {
			_, err := c.GetAgentSettings(context.Background(), netomatic.GetAgentSettingsRequest{Project: "demo"})
			return err
		},
		"ListAgentRuns": func(c *netomatic.HTTPClient) error {
			_, err := c.ListAgentRuns(context.Background(), netomatic.ListAgentRunsRequest{Project: "demo"})
			return err
		},
		"ListSandboxes": func(c *netomatic.HTTPClient) error {
			_, err := c.ListSandboxes(context.Background(), netomatic.ListSandboxesRequest{})
			return err
		},
		"CancelAgentRun": func(c *netomatic.HTTPClient) error {
			_, err := c.CancelAgentRun(context.Background(), netomatic.CancelAgentRunRequest{Run: "run"})
			return err
		},
		"AgentActivity": func(c *netomatic.HTTPClient) error {
			_, err := c.AgentActivity(context.Background(), netomatic.AgentActivityRequest{Run: "run"})
			return err
		},
		"RunOutput": func(c *netomatic.HTTPClient) error {
			_, err := c.RunOutput(context.Background(), netomatic.RunOutputRequest{Run: "run", Offset: 0})
			return err
		},
		"Capabilities": func(c *netomatic.HTTPClient) error { _, err := c.Capabilities(context.Background()); return err },
		"AddRepository": func(c *netomatic.HTTPClient) error {
			_, err := c.AddRepository(context.Background(), netomatic.AddRepositoryRequest{FullName: "acme/widgets"})
			return err
		},
		"GetAgentRun": func(c *netomatic.HTTPClient) error {
			_, err := c.GetAgentRun(context.Background(), netomatic.GetAgentRunRequest{Run: "run"})
			return err
		},
		"Complete": func(c *netomatic.HTTPClient) error {
			_, err := c.Complete(context.Background(), netomatic.CompleteRequest{Project: "demo", Run: "run"})
			return err
		},
		"ReviewApprovedBranches": func(c *netomatic.HTTPClient) error {
			_, err := c.ReviewApprovedBranches(context.Background(), netomatic.ReviewApprovedBranchesRequest{Project: "demo"})
			return err
		},
		"ReadDaemonLog": func(c *netomatic.HTTPClient) error { _, err := c.ReadDaemonLog(context.Background(), 0, 1); return err },
	}

	availableOperations := 0
	for _, operation := range netomatic.ContractOperations() {
		t.Run(operation.Name, func(t *testing.T) {
			client := newHTTPInteroperabilityClient(t, operation.Name != "PrefixEpic")
			if operation.Unavailable {
				assertAPIError(t, callOperation(client, operation.Name), http.StatusNotImplemented, netomatic.ErrorFeatureNotConfigured)
				return
			}
			availableOperations++
			call, ok := available[operation.Name]
			if !ok {
				t.Fatalf("available operation has no interoperability call")
			}
			if err := call(client); err != nil {
				t.Fatalf("%s() error = %v", operation.Name, err)
			}
		})
	}
	if len(available) != availableOperations {
		t.Fatalf("interoperability calls = %d, want one for every available operation", len(available))
	}
}

func callOperation(client any, name string) error {
	method := reflect.ValueOf(client).MethodByName(name)
	args := []reflect.Value{reflect.ValueOf(context.Background())}
	methodType := method.Type()
	for index := 1; index < methodType.NumIn(); index++ {
		argument := reflect.New(methodType.In(index)).Elem()
		fillOperationArgument(argument)
		args = append(args, argument)
	}
	results := method.Call(args)
	last := results[len(results)-1]
	if last.IsNil() {
		return nil
	}
	return last.Interface().(error)
}

func fillOperationArgument(value reflect.Value) {
	switch value.Kind() {
	case reflect.String:
		value.SetString("item")
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value.SetUint(1)
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if value.Field(index).CanSet() {
				fillOperationArgument(value.Field(index))
			}
		}
	}
}

func TestHandlerRejectsUnauthorizedAndCrossOriginRequests(t *testing.T) {
	server, err := New(&usecases.UseCases{}, nil, "token")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		authorize  bool
		origin     string
		wantStatus int
		wantCode   netomatic.ErrorCode
	}{
		{name: "missing token", wantStatus: http.StatusUnauthorized, wantCode: netomatic.ErrorUnauthorized},
		{name: "cross origin", authorize: true, origin: "http://malicious.example", wantStatus: http.StatusBadRequest, wantCode: netomatic.ErrorInvalidRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, netomatic.APIPrefix+"/projects", strings.NewReader(`{"name":"example"}`))
			request.Header.Set("Content-Type", "application/json")
			if test.authorize {
				request.Header.Set("Authorization", "Bearer token")
			}
			request.Header.Set("Origin", test.origin)
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, request)
			var response netomatic.APIError
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != test.wantStatus || response.Code != test.wantCode {
				t.Fatalf("response = %d %#v", recorder.Code, response)
			}
		})
	}
}

func TestNewRequiresDaemonToken(t *testing.T) {
	for _, tokens := range [][]string{nil, {""}, {"token", "other"}} {
		if _, err := New(&usecases.UseCases{}, nil, tokens...); err == nil {
			t.Fatalf("New(..., %#v) succeeded", tokens)
		}
	}
}

func TestHTTPServerCreatesEpicsAndIssues(t *testing.T) {
	workspace := &httpTestWorkspace{repositories: []string{"acme/widgets"}, epics: make(map[string]epicpkg.Epic)}
	registry := &httpTestRegistry{projects: []domain.Project{{ID: 1, Name: "demo"}}}
	useCases := usecases.NewUseCases(registry, httpTestFactory{workspace: workspace}, httpTestClock{}, nil, nil)
	useCases.CurrentUser = "octocat"
	server, err := New(useCases, nil, "token")
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	client, err := netomatic.NewHTTPClient(testServer.URL, "token")
	if err != nil {
		t.Fatal(err)
	}

	createdEpic, err := client.CreateEpic(context.Background(), netomatic.CreateEpicRequest{
		Project: "demo", Title: "Epic", Description: "Details",
	})
	if err != nil {
		t.Fatal(err)
	}
	if createdEpic.Epic.ID == "" || createdEpic.Epic.Title != "Epic" || len(createdEpic.Epic.Issues) != 1 {
		t.Fatalf("created epic = %#v", createdEpic)
	}

	createdIssue, err := client.CreateIssue(context.Background(), netomatic.CreateIssueRequest{
		Project: "demo", Epic: createdEpic.Epic.ID, Title: "Issue", Description: "Implement it",
	})
	if err != nil {
		t.Fatal(err)
	}
	if createdIssue.Issue.ID == "" || createdIssue.Issue.ParentID != createdEpic.Epic.Issues[0].ID ||
		createdIssue.Issue.Repository != "acme/widgets" {
		t.Fatalf("created issue = %#v", createdIssue)
	}
}

func TestHandlerMapsUnexpectedFailuresToInternalError(t *testing.T) {
	registry := &httpTestRegistry{listErr: errors.New("database unavailable")}
	useCases := usecases.NewUseCases(registry, httpTestFactory{workspace: &httpTestWorkspace{}}, httpTestClock{}, nil, nil)
	server, err := New(useCases, nil, "token")
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	client, err := netomatic.NewHTTPClient(testServer.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.ListProjects(context.Background())
	var response *netomatic.APIError
	if !errors.As(err, &response) || response.StatusCode != http.StatusInternalServerError ||
		response.Code != netomatic.ErrorInternal || response.Detail != "the daemon could not process the request" {
		t.Fatalf("error = %#v", err)
	}
}

func TestHandlerClassifiesValidationFailuresAsBadRequests(t *testing.T) {
	registry := &httpTestRegistry{projects: []domain.Project{{ID: 1, Name: "demo"}}}
	useCases := usecases.NewUseCases(registry, httpTestFactory{workspace: &httpTestWorkspace{epics: make(map[string]epicpkg.Epic)}}, httpTestClock{}, nil, nil)
	server, err := New(useCases, nil, "token")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "project", method: http.MethodPost, path: "/projects", body: `{"name":""}`},
		{name: "project syntax", method: http.MethodPost, path: "/projects", body: `{"name":"bad name"}`},
		{name: "store model", method: http.MethodPost, path: "/projects/1/setup", body: `{"model":""}`},
		{name: "epic", method: http.MethodPost, path: "/projects/demo/epics", body: `{"project":"demo","title":""}`},
		{name: "issue", method: http.MethodPost, path: "/projects/demo/epics/epic-1/issues", body: `{"project":"demo","epic":"epic-1","title":""}`},
		{name: "pull request status", method: http.MethodPost, path: "/projects/1/epics/epic-1/pull-requests/pr-1/state-transitions", body: `{"status":"merged"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, netomatic.APIPrefix+test.path, strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer token")
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, request)
			var response netomatic.APIError
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != http.StatusBadRequest || response.Code != netomatic.ErrorInvalidRequest {
				t.Fatalf("response = %d %#v", recorder.Code, response)
			}
		})
	}
}

func TestHandlerDoesNotMutateWithoutResponseReader(t *testing.T) {
	epic, err := epicpkg.CreateEpic("Epic", "octocat", "")
	if err != nil {
		t.Fatal(err)
	}
	workspace := &httpTestWorkspace{epics: map[string]epicpkg.Epic{epic.ID: epic}}
	registry := &httpTestRegistry{projects: []domain.Project{{ID: 1, Name: "demo"}}}
	useCases := usecases.NewUseCases(registry, httpTestFactory{workspace: workspace}, httpTestClock{}, nil, nil)
	useCases.GetEpic = nil
	server, err := New(useCases, nil, "token")
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, netomatic.APIPrefix+"/projects/demo/epics/"+epic.ID+"/prefix", strings.NewReader(`{"project":"demo","epic":"`+epic.ID+`","prefix":"work"}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotImplemented)
	}
	if workspace.epics[epic.ID].BranchPrefix != "" {
		t.Fatalf("epic was mutated: %#v", workspace.epics[epic.ID])
	}
}

func TestCapabilitiesExcludeUnavailableOperations(t *testing.T) {
	useCases := usecases.NewUseCases(&httpTestRegistry{}, httpTestFactory{workspace: &httpTestWorkspace{}}, httpTestClock{}, nil, &usecases.EpicAgentDependencies{
		Output: &httpTestRunOutput{}, Builder: httpTestCommandBuilder{},
	})
	server, err := New(useCases, nil, "token")
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	client, err := netomatic.NewHTTPClient(testServer.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	capabilities, err := client.Capabilities(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"runEpicAgent", "runIssueAgent", "reconcileSandboxes"} {
		if capabilities[name] {
			t.Fatalf("%s capability is advertised for an unavailable operation", name)
		}
	}
}

func TestHandlerExecutesRetainedAgentOperations(t *testing.T) {
	epic, err := epicpkg.CreateEpic("Epic", "octocat", "")
	if err != nil {
		t.Fatal(err)
	}
	epic.PullRequests = []epicpkg.PullRequest{{ID: "pr-1", Head: "feature/one", Status: epicpkg.PullRequestOpen, Approved: true}}
	workspace := &httpTestWorkspace{
		epics: map[string]epicpkg.Epic{epic.ID: epic},
		agentSettings: agent.AgentSettings{Roles: map[agent.AgentRole]agent.AgentProfile{
			agent.AgentRoleCoding:  {Agent: "coder", Variant: "high", MaxRounds: 3},
			agent.AgentRoleRefiner: {Agent: "refiner", Variant: "low", MaxRounds: 1},
		}},
	}
	output := &httpTestRunOutput{pages: map[int64][]string{8: {"next"}}, next: map[int64]int64{8: 13}, sizes: map[string]int64{"run-1": 42}}
	registry := &httpTestRegistry{
		projects:  []domain.Project{{ID: 1, Name: "demo"}},
		sandboxes: []agent.Sandbox{{ID: "sandbox-1", Name: "demo-coder", Status: agent.SandboxStatusRunning}},
		agentRuns: map[string]agent.AgentRun{"run-1": {ID: "run-1", ProjectID: 1, Subject: agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: epic.ID}}},
	}
	useCases := usecases.NewUseCases(registry, httpTestFactory{workspace: workspace}, httpTestClock{}, nil, &usecases.EpicAgentDependencies{
		Output: output, Builder: httpTestCommandBuilder{},
	})
	server, err := New(useCases, nil, "token")
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	client, err := netomatic.NewHTTPClient(testServer.URL, "token")
	if err != nil {
		t.Fatal(err)
	}

	sandboxes, err := client.ListSandboxes(context.Background(), netomatic.ListSandboxesRequest{})
	if err != nil || len(sandboxes.Sandboxes) != 1 || sandboxes.Sandboxes[0].ID != "sandbox-1" {
		t.Fatalf("sandboxes = %#v, error = %v", sandboxes, err)
	}
	settings, err := client.GetAgentSettings(context.Background(), netomatic.GetAgentSettingsRequest{Project: "demo"})
	if err != nil || len(settings.Settings) != len(agent.Roles()) ||
		settings.Settings[0].Values["role"] != string(agent.AgentRoleRefiner) ||
		settings.Settings[2].Agent != "coder" || settings.Settings[2].Values["maxRounds"] != "3" {
		t.Fatalf("settings = %#v, error = %v", settings, err)
	}
	activity, err := client.AgentActivity(context.Background(), netomatic.AgentActivityRequest{Run: "run-1"})
	if err != nil || len(activity.Activity) != 1 || activity.Activity[0].Size != 42 {
		t.Fatalf("activity = %#v, error = %v", activity, err)
	}
	gotRun, err := client.GetAgentRun(context.Background(), netomatic.GetAgentRunRequest{Run: "run-1"})
	if err != nil || gotRun.Run.Project != "demo" {
		t.Fatalf("agent run = %#v, error = %v", gotRun, err)
	}
	cancelled, err := client.CancelAgentRun(context.Background(), netomatic.CancelAgentRunRequest{Run: "run-1"})
	if err != nil || cancelled.Run.Project != "demo" {
		t.Fatalf("cancelled run = %#v, error = %v", cancelled, err)
	}
	page, err := client.RunOutput(context.Background(), netomatic.RunOutputRequest{Run: "run-1", Offset: 8})
	if err != nil || page.Output.Output != "next\n" || page.Output.Next != 13 || !reflect.DeepEqual(output.asked, []int64{8}) {
		t.Fatalf("page = %#v, output offsets = %v, error = %v", page, output.asked, err)
	}
	if next := int64(8 + len(page.Output.Output)); next != page.Output.Next {
		t.Fatalf("length-derived next offset = %d, want %d", next, page.Output.Next)
	}
	completed, err := client.Complete(context.Background(), netomatic.CompleteRequest{Project: "demo", Run: "run-1"})
	if err != nil || completed.Complete || workspace.readEpicCalls == 0 {
		t.Fatalf("completed = %#v, reads = %d, error = %v", completed, workspace.readEpicCalls, err)
	}
	review, err := client.ReviewApprovedBranches(context.Background(), netomatic.ReviewApprovedBranchesRequest{Project: "demo"})
	if err != nil || !reflect.DeepEqual(review.Branches, []string{"feature/one"}) {
		t.Fatalf("review = %#v, error = %v", review, err)
	}
}

func TestHandlerClassifiesDomainFailures(t *testing.T) {
	epic, err := epicpkg.CreateEpic("Epic", "octocat", "")
	if err != nil {
		t.Fatal(err)
	}
	workspace := &httpTestWorkspace{epics: map[string]epicpkg.Epic{epic.ID: epic}}
	registry := &httpTestRegistry{projects: []domain.Project{{ID: 1, Name: "demo"}}}
	useCases := usecases.NewUseCases(registry, httpTestFactory{workspace: workspace}, httpTestClock{}, nil, nil)
	server, err := New(useCases, nil, "token")
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	client, err := netomatic.NewHTTPClient(testServer.URL, "token")
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.TransitionEpic(context.Background(), netomatic.TransitionEpicRequest{
		Project: "demo", Epic: epic.ID, Status: string(epicpkg.EpicStateDone),
	})
	assertAPIError(t, err, http.StatusBadRequest, netomatic.ErrorInvalidRequest)

	_, err = client.GetIssue(context.Background(), netomatic.GetIssueRequest{
		Project: "demo", Epic: epic.ID, Issue: "missing",
	})
	assertAPIError(t, err, http.StatusNotFound, netomatic.ErrorNotFound)
}

func assertAPIError(t *testing.T, err error, status int, code netomatic.ErrorCode) {
	t.Helper()
	var response *netomatic.APIError
	if !errors.As(err, &response) || response.StatusCode != status || response.Code != code {
		t.Fatalf("error = %#v, want HTTP %d %s", err, status, code)
	}
}

func TestHandlerBoundsActivityResponse(t *testing.T) {
	output := &httpTestRunOutput{sizes: map[string]int64{"run-1": 16 << 20}}
	registry := &httpTestRegistry{}
	useCases := usecases.NewUseCases(registry, httpTestFactory{workspace: &httpTestWorkspace{}}, httpTestClock{}, nil, &usecases.EpicAgentDependencies{
		Output: output, Builder: httpTestCommandBuilder{},
	})
	server, err := New(useCases, nil, "token")
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	client, err := netomatic.NewHTTPClient(testServer.URL, "token")
	if err != nil {
		t.Fatal(err)
	}

	activity, err := client.AgentActivity(context.Background(), netomatic.AgentActivityRequest{Run: "run-1"})
	if err != nil || len(activity.Activity) != 1 || activity.Activity[0].Size != 16<<20 {
		t.Fatalf("activity = %#v, error = %v", activity, err)
	}
}

func TestHandlerRejectsMalformedRepositoryNames(t *testing.T) {
	useCases := usecases.NewUseCases(&httpTestRegistry{}, httpTestFactory{workspace: &httpTestWorkspace{}}, httpTestClock{}, nil, nil)
	server, err := New(useCases, nil, "token")
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, netomatic.APIPrefix+"/repositories", strings.NewReader(`{"fullName":"widgets"}`))
	request.Header.Set("Authorization", "Bearer token")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)

	var response netomatic.APIError
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusBadRequest || response.Code != netomatic.ErrorInvalidRequest {
		t.Fatalf("response = %d %#v", recorder.Code, response)
	}
}

func TestHandlerPagesDaemonLog(t *testing.T) {
	logFile := t.TempDir() + "/donsy.log"
	if err := os.WriteFile(logFile, []byte("first\nsecond\npartial"), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := NewWithDaemonLog(&usecases.UseCases{}, nil, logFile, "token")
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	client, err := netomatic.NewHTTPClient(testServer.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.ReadDaemonLog(context.Background(), 0, netomatic.MaxDaemonLogLines+1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(page.Lines, []string{"first", "second"}) || page.NextOffset != int64(len("first\nsecond\n")) {
		t.Fatalf("log page = %#v", page)
	}

}

func TestHandlerPagesDaemonLogAcrossOversizedRecordsAndResetOffsets(t *testing.T) {
	logFile := t.TempDir() + "/donsy.log"
	content := strings.Repeat("x", netomatic.MaxDaemonLogBytes+1) + "\nnext\n"
	if err := os.WriteFile(logFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := NewWithDaemonLog(&usecases.UseCases{}, nil, logFile, "token")
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(server.Handler())
	defer testServer.Close()
	client, err := netomatic.NewHTTPClient(testServer.URL, "token")
	if err != nil {
		t.Fatal(err)
	}

	page, err := client.ReadDaemonLog(context.Background(), 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(page.Lines, []string{"next"}) || page.NextOffset != int64(len(content)) || page.OffsetReset {
		t.Fatalf("oversized-record page = %#v", page)
	}

	page, err = client.ReadDaemonLog(context.Background(), int64(len(content)+1), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(page.Lines, []string{"next"}) || !page.OffsetReset {
		t.Fatalf("reset page = %#v", page)
	}
}

func newHTTPInteroperabilityClient(t *testing.T, withPullRequest bool) *netomatic.HTTPClient {
	t.Helper()
	epic, err := epicpkg.CreateEpic("Epic", "octocat", "")
	if err != nil {
		t.Fatal(err)
	}
	epic.ID = "epic"
	epic.Issues[0].ID = "root"
	epic.Repositories = []string{"acme/widgets"}
	child, err := epicpkg.CreateRepositoryIssue("Child", "", "acme/widgets")
	if err != nil {
		t.Fatal(err)
	}
	child.ID = "child"
	if err := epic.AddIssue("root", child); err != nil {
		t.Fatal(err)
	}
	if withPullRequest {
		pullRequest, err := epicpkg.CreatePullRequest("child", "Pull request", "acme/widgets", "feature", "main")
		if err != nil {
			t.Fatal(err)
		}
		pullRequest.ID = "pr"
		pullRequest.CodingRounds = epicpkg.MaxCodingRounds
		pullRequest.Rounds = epicpkg.MaxCodingRounds
		if err := epic.AddPullRequest("child", pullRequest); err != nil {
			t.Fatal(err)
		}
		for index := range epic.Issues {
			if epic.Issues[index].ID == "child" {
				epic.Issues[index].State = epicpkg.IssueStatePR
			}
		}
	}

	workspace := &httpTestWorkspace{
		repositories: []string{"acme/widgets"},
		epics:        map[string]epicpkg.Epic{"epic": epic},
		agentSettings: agent.AgentSettings{Roles: map[agent.AgentRole]agent.AgentProfile{
			agent.AgentRoleRefiner: {Agent: "openai/gpt"},
		}},
	}
	registry := &httpTestRegistry{
		projects: []domain.Project{{ID: 1, Name: "demo"}},
		agentRuns: map[string]agent.AgentRun{
			"run": {ID: "run", ProjectID: 1, Subject: agent.AgentSubject{Kind: agent.AgentSubjectEpic, ID: "epic"}},
		},
	}
	useCases := usecases.NewUseCases(registry, httpTestFactory{workspace: workspace}, httpTestClock{}, httpTestGitHub{}, &usecases.EpicAgentDependencies{
		Sandboxes: httpTestSandbox{},
		Code:      httpTestCode{},
		Differ:    httpTestCode{},
		Output:    &httpTestRunOutput{sizes: map[string]int64{"run": 1}},
		Builder:   httpTestCommandBuilder{},
	})
	useCases.CurrentUser = "octocat"
	logFile := t.TempDir() + "/donsy.log"
	if err := os.WriteFile(logFile, []byte("entry\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server, err := NewWithDaemonLog(useCases, nil, logFile, "token")
	if err != nil {
		t.Fatal(err)
	}
	testServer := httptest.NewServer(server.Handler())
	t.Cleanup(testServer.Close)
	client, err := netomatic.NewHTTPClient(testServer.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

type httpTestClock struct{}

func (httpTestClock) Now() time.Time { return time.Unix(0, 0) }

type httpTestFactory struct{ workspace application.Workspace }

func (f httpTestFactory) Open(string) application.Workspace { return f.workspace }

type httpTestGitHub struct{}

func (httpTestGitHub) CheckAuth(context.Context) error { return nil }
func (httpTestGitHub) CurrentUser(context.Context) (string, error) {
	return "octocat", nil
}
func (httpTestGitHub) ListOrganisations(context.Context) ([]domain.Organisation, error) {
	return nil, nil
}
func (httpTestGitHub) ListRepositories(context.Context, string) ([]application.GitHubRepository, error) {
	return nil, nil
}
func (httpTestGitHub) ListUserRepositories(context.Context) ([]application.GitHubRepository, error) {
	return nil, nil
}

type httpTestWorkspace struct {
	repositories  []string
	epics         map[string]epicpkg.Epic
	agentSettings agent.AgentSettings
	readEpicCalls int
}

func (w *httpTestWorkspace) ListEpics() ([]epicpkg.Epic, error) {
	epics := make([]epicpkg.Epic, 0, len(w.epics))
	for _, epic := range w.epics {
		epics = append(epics, epic)
	}
	return epics, nil
}
func (w *httpTestWorkspace) ReadEpic(id string) (epicpkg.Epic, error) {
	w.readEpicCalls++
	epic, ok := w.epics[id]
	if !ok {
		return epicpkg.Epic{}, os.ErrNotExist
	}
	return epic, nil
}
func (w *httpTestWorkspace) AgentSettings() (agent.AgentSettings, error) {
	return w.agentSettings, nil
}
func (w *httpTestWorkspace) UpdateAgentSettings(func(*agent.AgentSettings) error) error {
	return nil
}
func (w *httpTestWorkspace) UpdateRepositorySettings(string, func(*agent.RepositorySettings) error) error {
	return nil
}
func (w *httpTestWorkspace) RepositorySettings(string) (agent.RepositorySettings, error) {
	return agent.RepositorySettings{}, nil
}
func (w *httpTestWorkspace) ReadFile(string) (string, error)    { return "", os.ErrNotExist }
func (w *httpTestWorkspace) WriteFile(string, string) error     { return nil }
func (w *httpTestWorkspace) Repositories() ([]string, error)    { return w.repositories, nil }
func (w *httpTestWorkspace) UpdateRepositories([]string) error  { return nil }
func (w *httpTestWorkspace) CreateEpic(epic epicpkg.Epic) error { w.epics[epic.ID] = epic; return nil }
func (w *httpTestWorkspace) UpdateEpic(id string, change func(*epicpkg.Epic) error) error {
	epic, ok := w.epics[id]
	if !ok {
		return os.ErrNotExist
	}
	if err := change(&epic); err != nil {
		return err
	}
	w.epics[id] = epic
	return nil
}

type httpTestRegistry struct {
	projects  []domain.Project
	listErr   error
	sandboxes []agent.Sandbox
	agentRuns map[string]agent.AgentRun
}

func (r *httpTestRegistry) List() ([]domain.Project, error) { return r.projects, r.listErr }
func (r *httpTestRegistry) Create(project *domain.Project) error {
	project.ID = uint(len(r.projects) + 1)
	r.projects = append(r.projects, *project)
	return nil
}
func (*httpTestRegistry) Touch(uint) error                                      { return nil }
func (*httpTestRegistry) Delete(uint) error                                     { return nil }
func (*httpTestRegistry) Close() error                                          { return nil }
func (*httpTestRegistry) ListOrganisations() ([]domain.Organisation, error)     { return nil, nil }
func (*httpTestRegistry) CreateOrganisation(*domain.Organisation) error         { return nil }
func (*httpTestRegistry) DeleteOrganisation(string) error                       { return nil }
func (*httpTestRegistry) ListRepositories() ([]domain.Repository, error)        { return nil, nil }
func (*httpTestRegistry) ReplaceRepositories(string, []domain.Repository) error { return nil }
func (*httpTestRegistry) SaveRepository(domain.Repository) error                { return nil }
func (*httpTestRegistry) SaveSandbox(agent.Sandbox) error                       { return nil }
func (r *httpTestRegistry) ListSandboxes(uint) ([]agent.Sandbox, error)         { return r.sandboxes, nil }
func (*httpTestRegistry) SaveAgentRun(agent.AgentRun) error                     { return nil }
func (*httpTestRegistry) ListAgentRuns(uint, agent.AgentSubject) ([]agent.AgentRun, error) {
	return nil, nil
}
func (*httpTestRegistry) ListProjectAgentRuns(uint) ([]agent.AgentRun, error) { return nil, nil }
func (r *httpTestRegistry) GetAgentRun(id string) (agent.AgentRun, error) {
	run, ok := r.agentRuns[id]
	if !ok {
		return agent.AgentRun{}, os.ErrNotExist
	}
	return run, nil
}
func (*httpTestRegistry) DeleteSubjectRuntime(uint, agent.AgentSubject) error { return nil }
func (*httpTestRegistry) DeleteProjectRuntime(uint) error                     { return nil }

var _ application.Registry = (*httpTestRegistry)(nil)
var _ agent_runtime.AgentRegistry = (*httpTestRegistry)(nil)

type httpTestRunOutput struct {
	pages map[int64][]string
	next  map[int64]int64
	sizes map[string]int64
	asked []int64
}

func (o *httpTestRunOutput) Tail(_ string, from int64) ([]string, int64, error) {
	o.asked = append(o.asked, from)
	return o.pages[from], o.next[from], nil
}
func (o *httpTestRunOutput) Size(runID string) (int64, error) { return o.sizes[runID], nil }
func (*httpTestRunOutput) Discard(string) error               { return nil }

type httpTestCommandBuilder struct{}

func (httpTestCommandBuilder) Command(application.AgentInvocation) ([]string, error) { return nil, nil }
func (httpTestCommandBuilder) ExtractAnswer(string) string                           { return "" }
func (httpTestCommandBuilder) ParseTranscript(value string) []agent.TranscriptEntry {
	return []agent.TranscriptEntry{{Text: value}}
}
func (httpTestCommandBuilder) ParseUsage(string) agent.RunUsage { return agent.RunUsage{} }
func (httpTestCommandBuilder) ReviewApproved(string) bool       { return false }

type httpTestSandbox struct{}

func (httpTestSandbox) Ensure(context.Context, agent_runtime.SandboxSpec) (bool, error) {
	return false, nil
}
func (httpTestSandbox) Start(context.Context, agent.SandboxRef) error   { return nil }
func (httpTestSandbox) Stop(context.Context, agent.SandboxRef) error    { return nil }
func (httpTestSandbox) StopNow(context.Context, agent.SandboxRef) error { return nil }
func (httpTestSandbox) Delete(context.Context, agent.SandboxRef) error  { return nil }
func (httpTestSandbox) Reserve(context.Context, agent_runtime.SandboxSpec) (func(), bool, error) {
	return func() {}, true, nil
}

type httpTestCode struct{}

func (httpTestCode) DefaultBranch(context.Context, string) (string, error) { return "main", nil }
func (httpTestCode) PurgeEpic(string) error                                { return nil }
func (httpTestCode) Checkout(context.Context, agent_runtime.CodeCheckout, string, string) (string, error) {
	return "/checkout", nil
}
func (httpTestCode) Resolve(agent_runtime.CodeCheckout, string) (string, error) { return "head", nil }
func (httpTestCode) CommitAll(agent_runtime.CodeCheckout, string) (string, error) {
	return "head", nil
}
func (httpTestCode) CommitsSince(agent_runtime.CodeCheckout, string) ([]agent_runtime.CommitInfo, error) {
	return nil, nil
}
func (httpTestCode) DescendsFrom(agent_runtime.CodeCheckout, string) (bool, error)  { return true, nil }
func (httpTestCode) Push(context.Context, agent_runtime.CodeCheckout, string) error { return nil }
func (httpTestCode) DeleteBranch(context.Context, agent_runtime.CodeCheckout, string) error {
	return nil
}
func (httpTestCode) Merge(context.Context, agent_runtime.CodeCheckout, string, string) error {
	return nil
}
func (httpTestCode) InspectBranches(context.Context, string, string, string, []string) (map[string]agent_runtime.BranchState, error) {
	return map[string]agent_runtime.BranchState{}, nil
}
func (httpTestCode) Diff(context.Context, string, string, string, string) (string, error) {
	return "diff", nil
}
