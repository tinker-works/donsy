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
		t.Run(operation.Name, func(t *testing.T) {
			if !operation.Unavailable {
				t.Skip("configured operations are covered by fake-use-case interoperability tests")
			}
			err := callOperation(client, operation.Name)
			var apiError *netomatic.APIError
			if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusNotImplemented || apiError.Code != netomatic.ErrorFeatureNotConfigured {
				t.Fatalf("%s() error = %#v, want feature_not_configured", operation.Name, err)
			}
		})
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
		name string
		path string
		body string
	}{
		{name: "project", path: "/projects", body: `{"name":""}`},
		{name: "epic", path: "/projects/demo/epics", body: `{"project":"demo","title":""}`},
		{name: "issue", path: "/projects/demo/epics/epic-1/issues", body: `{"project":"demo","epic":"epic-1","title":""}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, netomatic.APIPrefix+test.path, strings.NewReader(test.body))
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

type httpTestClock struct{}

func (httpTestClock) Now() time.Time { return time.Unix(0, 0) }

type httpTestFactory struct{ workspace application.Workspace }

func (f httpTestFactory) Open(string) application.Workspace { return f.workspace }

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
