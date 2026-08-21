package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/tinker-works/donsy/internal/application/usecases"
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

func callOperation(client any, name string) error {
	method := reflect.ValueOf(client).MethodByName(name)
	args := []reflect.Value{reflect.ValueOf(context.Background())}
	for index := 1; index < method.Type().NumIn(); index++ {
		argument := reflect.New(method.Type().In(index)).Elem()
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
