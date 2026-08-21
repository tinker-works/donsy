package netomatic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestHTTPClientImplementsCompleteContract(t *testing.T) {
	const token = "test-token"
	var operation Operation
	var responseBody []byte
	var requestMethod string
	var requestPath string
	var requestQuery url.Values
	var requestRawQuery string
	var requestBody []byte
	var requestAuthorization string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMethod = r.Method
		requestPath = r.URL.EscapedPath()
		requestQuery = r.URL.Query()
		requestRawQuery = r.URL.RawQuery
		requestBody, _ = io.ReadAll(r.Body)
		requestAuthorization = r.Header.Get("Authorization")
		if operation.Unavailable {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = io.WriteString(w, `{"code":"feature_not_configured","detail":"registered feature is unavailable"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(operation.SuccessStatus)
		_, _ = w.Write(responseBody)
	}))
	defer server.Close()

	client, err := NewHTTPClient(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}

	for index := range Contract {
		operation = Contract[index]
		t.Run(operation.Name, func(t *testing.T) {
			path := contractTestPath(operation)
			query := contractTestQuery(operation)
			request := contractTestRequest(operation)
			responseBody = nil
			if operation.Response != "" {
				responseBody, err = contractFixtureJSON(contractDTOs[operation.Response])
				if err != nil {
					t.Fatal(err)
				}
			}
			requestMethod = ""
			requestPath = ""
			requestQuery = nil
			requestRawQuery = ""
			requestBody = nil
			requestAuthorization = ""

			got, callErr := callContractOperationParts(client, operation, path, query, request)
			if operation.Unavailable {
				var apiError *APIError
				if !errors.As(callErr, &apiError) || apiError.StatusCode != http.StatusNotImplemented || apiError.Code != ErrorFeatureNotConfigured {
					t.Fatalf("error = %#v, want feature_not_configured 501", callErr)
				}
				if !errors.Is(callErr, ErrUnavailable) {
					t.Fatalf("error = %v, want ErrUnavailable", callErr)
				}
			} else {
				if callErr != nil {
					t.Fatalf("%s(): %v", operation.Name, callErr)
				}
				wantResponse := any(nil)
				if operation.Response != "" {
					wantResponse = contractFixtureValue(contractDTOs[operation.Response])
				}
				if !reflect.DeepEqual(got, wantResponse) {
					t.Fatalf("%s() response = %#v, want %#v", operation.Name, got, wantResponse)
				}
			}

			if requestMethod != string(operation.Method) {
				t.Errorf("method = %q, want %q", requestMethod, operation.Method)
			}
			if requestPath != contractTestRoute(operation, path) {
				t.Errorf("path = %q, want %q", requestPath, contractTestRoute(operation, path))
			}
			if !operation.Unavailable && requestQuery.Encode() != query.Encode() {
				t.Errorf("query = %v, want %v", requestQuery, query)
			}
			if !operation.Unavailable && requestRawQuery != query.Encode() {
				t.Errorf("raw query = %q, want %q", requestRawQuery, query.Encode())
			}
			wantAuthorization := ""
			if operation.Authenticated {
				wantAuthorization = "Bearer " + token
			}
			if requestAuthorization != wantAuthorization {
				t.Errorf("authorization = %q, want %q", requestAuthorization, wantAuthorization)
			}

			var wantBody []byte
			if operation.Request != "" && operation.Method != MethodGet {
				wantBody, err = contractFixtureJSON(contractDTOs[operation.Request])
				if err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(requestBody, wantBody) {
				t.Errorf("request body = %s, want %s", requestBody, wantBody)
			}
		})
	}
}

func TestHTTPClientOptionalFeaturesDecode501(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = io.WriteString(w, `{"code":"feature_not_configured","detail":"feature is not configured for this process"}`)
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		call func() error
	}{
		{name: "reset issue", call: func() error {
			return client.ResetIssue(context.Background(), ResetIssuePath{ProjectID: 7, EpicID: "epic-1", PullRequestID: "pr-1"})
		}},
		{name: "open pull requests", call: func() error {
			_, err := client.OpenPullRequests(context.Background(), OpenPullRequestsPath{ProjectID: 7, EpicID: "epic-1"})
			return err
		}},
		{name: "discover organisations", call: func() error { _, err := client.DiscoverOrganisations(context.Background()); return err }},
		{name: "list repositories", call: func() error { _, err := client.ListRepositories(context.Background()); return err }},
		{name: "sync repositories", call: func() error { return client.SyncRepositories(context.Background()) }},
		{name: "run output", call: func() error {
			_, err := client.RunOutput(context.Background(), AgentRunPath{RunID: "run-1"}, url.Values{"from": {"0"}})
			return err
		}},
		{name: "activity", call: func() error {
			_, err := client.AgentActivity(context.Background(), url.Values{"runID": {"run-1"}})
			return err
		}},
		{name: "cancel run", call: func() error {
			_, err := client.CancelAgentRun(context.Background(), AgentRunPath{RunID: "run-1"})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			var apiError *APIError
			if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusNotImplemented || apiError.Code != ErrorFeatureNotConfigured {
				t.Fatalf("error = %#v, want feature_not_configured 501", err)
			}
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("error = %v, want ErrUnavailable", err)
			}
		})
	}
}

func callContractOperationParts(client any, operation Operation, path any, query url.Values, request any) (any, error) {
	method := reflect.ValueOf(client).MethodByName(operation.Name)
	if !method.IsValid() {
		return nil, errors.New("missing client method")
	}
	args := []reflect.Value{reflect.ValueOf(context.Background())}
	if operation.Path != "" {
		args = append(args, reflect.ValueOf(path))
	}
	if operation.Query != "" {
		args = append(args, reflect.ValueOf(query))
	}
	if operation.Request != "" {
		args = append(args, reflect.ValueOf(request))
	}
	results := method.Call(args)
	switch len(results) {
	case 1:
		return nil, contractCallError(results[0])
	case 2:
		if err := contractCallError(results[1]); err != nil {
			return nil, err
		}
		return results[0].Interface(), nil
	default:
		return nil, fmt.Errorf("unexpected result count %d", len(results))
	}
}

func contractCallError(value reflect.Value) error {
	if !value.IsValid() {
		return nil
	}
	if (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) && value.IsNil() {
		return nil
	}
	err, ok := value.Interface().(error)
	if !ok {
		return fmt.Errorf("result %s is not an error", value.Type())
	}
	return err
}

func contractTestRoute(operation Operation, request any) string {
	route := operation.Route
	if request == nil {
		return route
	}
	value := reflect.ValueOf(request)
	for placeholder, fieldName := range map[string]string{
		"projectID":     "ProjectID",
		"epicID":        "EpicID",
		"issueID":       "IssueID",
		"pullRequestID": "PullRequestID",
		"name":          "Name",
		"role":          "Role",
		"runID":         "RunID",
	} {
		field := value.FieldByName(fieldName)
		if !field.IsValid() {
			continue
		}
		var segment string
		switch field.Kind() {
		case reflect.String:
			segment = url.PathEscape(field.String())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			segment = strconv.FormatUint(field.Uint(), 10)
		default:
			continue
		}
		route = strings.Replace(route, "{"+placeholder+"}", segment, 1)
	}
	return route
}

func contractTestPath(operation Operation) any {
	if operation.Path == "" {
		return nil
	}
	return contractPathDTOs[operation.Path]
}

func contractTestQuery(operation Operation) url.Values {
	if operation.Query == "" {
		return nil
	}
	return contractQueryDTOs[operation.Query]
}

func contractTestRequest(operation Operation) any {
	if operation.Request == "" {
		return nil
	}
	return contractFixtureValue(contractDTOs[operation.Request])
}

func TestHTTPClientEscapesPathSegments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := APIPrefix + "/projects/7/epics/epic%2Fone"
		if r.URL.EscapedPath() != want {
			t.Errorf("escaped path = %q, want %q", r.URL.EscapedPath(), want)
		}
		_, _ = io.WriteString(w, epicFixtureJSON)
	}))
	defer server.Close()
	client, err := NewHTTPClient(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetEpic(context.Background(), EpicPath{ProjectID: 7, EpicID: "epic/one"}); err != nil {
		t.Fatal(err)
	}
}

func TestNewHTTPClientValidatesBaseURL(t *testing.T) {
	for _, baseURL := range []string{"", "localhost:8337", "ftp://localhost:8337", "http://", "http://localhost:8337?token=secret", "http://localhost:8337/#fragment"} {
		t.Run(baseURL, func(t *testing.T) {
			if _, err := NewHTTPClient(baseURL, "token"); err == nil {
				t.Fatalf("NewHTTPClient(%q) succeeded", baseURL)
			}
		})
	}
}

func TestHTTPClientErrorsAndResponseLimit(t *testing.T) {
	t.Run("malformed response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "not-json") }))
		defer server.Close()
		client, err := NewHTTPClient(server.URL, "token")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Process(context.Background()); err == nil {
			t.Fatal("malformed response succeeded")
		}
	})

	t.Run("non-2xx response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"code":"unauthorized","detail":"bad token"}`)
		}))
		defer server.Close()
		client, err := NewHTTPClient(server.URL, "token")
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Process(context.Background())
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("error = %v, want ErrUnauthorized", err)
		}
	})

	t.Run("unexpected 2xx response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) }))
		defer server.Close()
		client, err := NewHTTPClient(server.URL, "token")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Process(context.Background()); !errors.Is(err, ErrUnexpectedStatus) {
			t.Fatalf("error = %v, want ErrUnexpectedStatus", err)
		}
	})
}

func TestHTTPClientSendsRunOutputOffsetAsQuery(t *testing.T) {
	var query url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_, _ = io.WriteString(w, `{"Entries":[{"Kind":0,"Tool":"","CallID":"","Text":"next"}],"Next":256}`)
	}))
	defer server.Close()

	client, err := NewHTTPClient(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.RunOutput(context.Background(), RunOutputPath{RunID: "run-1"}, url.Values{"from": {"128"}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(query, url.Values{"from": {"128"}}) || len(response.Entries) != 1 || response.Entries[0].Text != "next" || response.Next != 256 {
		t.Fatalf("query = %v, response = %#v", query, response)
	}
}
