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

func TestHTTPServerInteroperatesWithEveryNetomaticOperation(t *testing.T) {
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
			err := callOperation(client, operation.Name)
			if operation.Name == "Process" || operation.Name == "Capabilities" {
				if err != nil {
					t.Fatalf("%s() error = %v", operation.Name, err)
				}
				return
			}
			var apiError *netomatic.APIError
			if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusNotImplemented || apiError.Code != netomatic.ErrorFeatureNotConfigured {
				t.Fatalf("%s() error = %#v, want documented unavailable response", operation.Name, err)
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
	repositories []string
	epics        map[string]epicpkg.Epic
}

func (w *httpTestWorkspace) ListEpics() ([]epicpkg.Epic, error) {
	epics := make([]epicpkg.Epic, 0, len(w.epics))
	for _, epic := range w.epics {
		epics = append(epics, epic)
	}
	return epics, nil
}
func (w *httpTestWorkspace) ReadEpic(id string) (epicpkg.Epic, error) {
	epic, ok := w.epics[id]
	if !ok {
		return epicpkg.Epic{}, os.ErrNotExist
	}
	return epic, nil
}
func (w *httpTestWorkspace) AgentSettings() (agent.AgentSettings, error) {
	return agent.AgentSettings{}, nil
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
	projects []domain.Project
	listErr  error
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
func (*httpTestRegistry) ListSandboxes(uint) ([]agent.Sandbox, error)           { return nil, nil }
func (*httpTestRegistry) SaveAgentRun(agent.AgentRun) error                     { return nil }
func (*httpTestRegistry) ListAgentRuns(uint, agent.AgentSubject) ([]agent.AgentRun, error) {
	return nil, nil
}
func (*httpTestRegistry) ListProjectAgentRuns(uint) ([]agent.AgentRun, error) { return nil, nil }
func (*httpTestRegistry) GetAgentRun(string) (agent.AgentRun, error) {
	return agent.AgentRun{}, os.ErrNotExist
}
func (*httpTestRegistry) DeleteSubjectRuntime(uint, agent.AgentSubject) error { return nil }
func (*httpTestRegistry) DeleteProjectRuntime(uint) error                     { return nil }

var _ application.Registry = (*httpTestRegistry)(nil)
var _ agent_runtime.AgentRegistry = (*httpTestRegistry)(nil)
