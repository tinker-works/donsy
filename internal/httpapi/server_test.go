package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tinker-works/donsy/internal/application/usecases"
	"github.com/tinker-works/donsy/netomatic"
)

func TestServerRegistersEveryNetomaticContractRoute(t *testing.T) {
	server, err := New(&usecases.UseCases{}, nil, "token")
	if err != nil {
		t.Fatal(err)
	}

	for _, operation := range netomatic.ContractOperations() {
		t.Run(operation.Name, func(t *testing.T) {
			path := strings.NewReplacer(
				"{projectID}", "1",
				"{epicID}", "epic",
				"{issueID}", "issue",
				"{pullRequestID}", "pull-request",
				"{runID}", "run",
				"{role}", "coding",
				"{name}", "acme",
			).Replace(operation.Route)
			request := httptest.NewRequest(string(operation.Method), path, nil)
			request.Header.Set("Authorization", "Bearer token")
			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(recorder, request)

			if recorder.Code == http.StatusNotFound {
				t.Fatalf("%s %s is not registered", operation.Method, operation.Route)
			}
		})
	}
}

func TestServerDoesNotRegisterLegacyRoutes(t *testing.T) {
	server, err := New(&usecases.UseCases{}, nil, "token")
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"/api/v1/health",
		"/api/v1/daemon-log",
		"/api/v1/complete",
		"/api/v1/repositories/acme/widgets",
		"/api/v1/agent-runs/run/activity",
	} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Authorization", "Bearer token")
			recorder := httptest.NewRecorder()

			server.Handler().ServeHTTP(recorder, request)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("GET %s returned %d, want 404", path, recorder.Code)
			}
		})
	}
}

func TestServerRequiresBearerToken(t *testing.T) {
	server, err := New(&usecases.UseCases{}, nil, "token")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, netomatic.APIPrefix+"/process", nil)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}
