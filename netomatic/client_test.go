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

func TestHTTPClientImplementsContract(t *testing.T) {
	const token = "test-token"
	var operation Operation
	var requestBody []byte
	var responseBody []byte
	var requestMethod string
	var requestPath string
	var requestQuery url.Values
	var requestRawQuery string
	var requestStatus int
	var requestAuthorization string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMethod = r.Method
		requestPath = r.URL.EscapedPath()
		requestQuery = r.URL.Query()
		requestRawQuery = r.URL.RawQuery
		if len(requestQuery) == 0 {
			requestQuery = nil
		}
		requestStatus = 0
		requestAuthorization = r.Header.Get("Authorization")
		requestBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		requestStatus = operation.SuccessStatus
		w.WriteHeader(operation.SuccessStatus)
		_, _ = w.Write(responseBody)
	}))
	defer server.Close()

	client, err := NewHTTPClient(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}

	for _, operation = range Contract {
		t.Run(operation.Name, func(t *testing.T) {
			path := contractTestPath(operation)
			query := contractTestQuery(operation)
			request := contractTestRequest(operation)
			response := contractFixtureValue(contractDTOs[operation.Response])
			responseBody = nil
			if operation.Response != "" {
				responseBody, err = contractFixtureJSON(contractDTOs[operation.Response])
				if err != nil {
					t.Fatal(err)
				}
			}
			requestBody = nil
			requestMethod = ""
			requestPath = ""
			requestQuery = nil
			requestRawQuery = ""
			requestStatus = 0
			requestAuthorization = ""

			got, err := callContractOperationParts(client, operation, path, query, request)
			if err != nil {
				t.Fatalf("%s(): %v", operation.Name, err)
			}
			if !reflect.DeepEqual(got, response) {
				t.Fatalf("%s() response = %#v, want %#v", operation.Name, got, response)
			}

			if requestMethod != string(operation.Method) {
				t.Errorf("method = %q, want %q", requestMethod, operation.Method)
			}
			if requestPath != contractTestRoute(operation, path) {
				t.Errorf("path = %q, want %q", requestPath, contractTestRoute(operation, path))
			}
			if requestStatus != operation.SuccessStatus {
				t.Errorf("status = %d, want %d", requestStatus, operation.SuccessStatus)
			}
			if operation.Query != "" {
				if requestQuery.Encode() != query.Encode() {
					t.Errorf("query = %v, want %v", requestQuery, contractTestQuery(operation))
				}
				if requestRawQuery != query.Encode() {
					t.Errorf("raw query = %q, want %q", requestRawQuery, query.Encode())
				}
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

func TestHTTPClientPullRequestOutcomesAndCommentTargets(t *testing.T) {
	var mergeCalls int
	var commentBodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case APIPrefix + "/projects/7/epics/epic-1/pull-requests/pr-1/merge":
			outcomes := []string{"merged", "returned_to_coding"}
			if mergeCalls >= len(outcomes) {
				t.Fatalf("unexpected merge call %d", mergeCalls+1)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"outcome":"`+outcomes[mergeCalls]+`"}`)
			mergeCalls++
		case APIPrefix + "/projects/7/epics/epic-1/comments":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			commentBodies = append(commentBodies, string(body))
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path %q", r.URL.EscapedPath())
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	path := MergePullRequestPath{ProjectID: 7, EpicID: "epic-1", PullRequestID: "pr-1"}
	for _, want := range []MergeOutcome{MergeOutcomeMerged, MergeOutcomeReturnedToCoding} {
		response, err := client.MergePullRequest(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		if response.Outcome != want {
			t.Fatalf("outcome = %q, want %q", response.Outcome, want)
		}
	}

	commentPath := AddCommentPath{ProjectID: 7, EpicID: "epic-1"}
	for _, target := range []CommentTarget{IssueCommentTarget, PullRequestCommentTarget} {
		err := client.AddComment(context.Background(), commentPath, AddCommentRequest{
			TargetID: "target-1", Target: target, Body: "Please review",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	wantBodies := []string{
		`{"targetId":"target-1","target":"issue","body":"Please review"}`,
		`{"targetId":"target-1","target":"pull_request","body":"Please review"}`,
	}
	if !reflect.DeepEqual(commentBodies, wantBodies) {
		t.Fatalf("comment bodies = %v, want %v", commentBodies, wantBodies)
	}
}

func TestHTTPClientAgentQueriesUseTheURLWithoutGETBodies(t *testing.T) {
	var requestBody []byte
	var requestQuery url.Values
	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.EscapedPath()
		requestQuery = r.URL.Query()
		requestBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"Entries":[],"Next":0}`)
	}))
	defer server.Close()

	client, err := NewHTTPClient(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.RunOutput(context.Background(), RunOutputPath{RunID: "run/1"}, url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	if requestPath != APIPrefix+"/agent-runs/run%2F1/output" {
		t.Fatalf("path = %q, want escaped run path", requestPath)
	}
	if len(requestQuery) != 0 {
		t.Fatalf("query = %v, want empty query", requestQuery)
	}
	if len(requestBody) != 0 {
		t.Fatalf("GET body = %q, want empty", requestBody)
	}
}

func TestHTTPClientAgentUnavailableFeaturesDecode501(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = io.WriteString(w, `{"code":"feature_not_configured","detail":"agent feature is unavailable"}`)
	}))
	defer server.Close()

	client, err := NewHTTPClient(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		call func() error
	}{
		{name: "output", call: func() error {
			_, err := client.RunOutput(context.Background(), RunOutputPath{RunID: "run"}, url.Values{"from": {"12"}})
			return err
		}},
		{name: "cancellation", call: func() error {
			_, err := client.CancelAgentRun(context.Background(), CancelAgentRunPath{RunID: "run"})
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			var apiError *APIError
			if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusNotImplemented || apiError.Code != ErrorFeatureNotConfigured || apiError.Detail != "agent feature is unavailable" {
				t.Fatalf("error = %#v, want feature_not_configured 501", err)
			}
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("error = %v, want ErrUnavailable", err)
			}
		})
	}
}

func TestHTTPClientPullRequestUnavailableFeaturesDecode501(t *testing.T) {
	var requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = io.WriteString(w, `{"code":"feature_not_configured","detail":"pull-request feature is unavailable"}`)
	}))
	defer server.Close()

	client, err := NewHTTPClient(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}

	path := MergePullRequestPath{ProjectID: 7, EpicID: "epic-1", PullRequestID: "pr-1"}
	for _, test := range []struct {
		name     string
		wantPath string
		call     func() error
	}{
		{name: "reset issue", wantPath: APIPrefix + "/projects/7/epics/epic-1/pull-requests/pr-1/reset", call: func() error {
			return client.ResetIssue(context.Background(), ResetIssuePath(path))
		}},
		{name: "open pull requests", wantPath: APIPrefix + "/projects/7/epics/epic-1/open-pull-requests", call: func() error {
			_, err := client.OpenPullRequests(context.Background(), OpenPullRequestsPath{ProjectID: path.ProjectID, EpicID: path.EpicID})
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			requestPath = ""
			err := test.call()
			if requestPath != test.wantPath {
				t.Fatalf("path = %q, want %q", requestPath, test.wantPath)
			}
			var apiError *APIError
			if !errors.As(err, &apiError) || apiError.StatusCode != http.StatusNotImplemented || apiError.Code != ErrorFeatureNotConfigured || apiError.Detail != "pull-request feature is unavailable" {
				t.Fatalf("error = %#v, want feature_not_configured 501", err)
			}
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("error = %v, want ErrUnavailable", err)
			}
		})
	}
}

func callContractOperationParts(client any, operation Operation, path any, query url.Values, request any) (any, error) {
	ctx := context.Background()
	method := reflect.ValueOf(client).MethodByName(operation.Name)
	if !method.IsValid() {
		return nil, errors.New("missing client method")
	}

	args := []reflect.Value{reflect.ValueOf(ctx)}
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
		if err := contractCallError(results[0]); err != nil {
			return nil, err
		}
		return nil, nil
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
		"project":       "Project",
		"projectID":     "ProjectID",
		"epic":          "Epic",
		"epicID":        "EpicID",
		"issue":         "Issue",
		"issueID":       "IssueID",
		"pull_request":  "PullRequest",
		"pullRequestID": "PullRequestID",
		"repository":    "Repository",
		"organisation":  "Organisation",
		"run":           "Run",
		"runID":         "RunID",
		"role":          "Role",
		"name":          "Name",
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
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			segment = strconv.FormatInt(field.Int(), 10)
		default:
			continue
		}
		route = strings.Replace(route, "{"+placeholder+"}", segment, 1)
	}
	return route
}

func contractTestPath(operation Operation) any {
	if operation.Path == "" {
		return contractDTOs[operation.Request]
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
	if operation.Request != "" {
		return contractFixtureValue(contractDTOs[operation.Request])
	}
	return nil
}

func TestHTTPClientEscapesPathSegments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := APIPrefix + "/projects/demo%2Fblue/epics/epic%20one/issues/issue%3F1"
		if r.URL.EscapedPath() != want {
			t.Errorf("escaped path = %q, want %q", r.URL.EscapedPath(), want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"issue":{"id":"issue-1","title":"title","status":"open"}}`)
	}))
	defer server.Close()

	client, err := NewHTTPClient(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetIssue(context.Background(), GetIssueRequest{
		Project: "demo/blue",
		Epic:    "epic one",
		Issue:   "issue?1",
	})
	if err != nil {
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
	for _, baseURL := range []string{"http://localhost:8337", "https://localhost:8337/api"} {
		if _, err := NewHTTPClient(baseURL, "token"); err != nil {
			t.Fatalf("NewHTTPClient(%q) = %v", baseURL, err)
		}
	}
}

func TestHTTPClientErrorsAndResponseLimit(t *testing.T) {
	t.Run("malformed response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "not-json")
		}))
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
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"code":"unauthorized","detail":"bad token"}`)
		}))
		defer server.Close()
		client, err := NewHTTPClient(server.URL, "token")
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Process(context.Background())
		var apiError *APIError
		if !errors.As(err, &apiError) || apiError.Code != ErrorUnauthorized || apiError.StatusCode != http.StatusUnauthorized {
			t.Fatalf("error = %#v, want unauthorized APIError", err)
		}
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatal("API error did not unwrap to ErrUnauthorized")
		}
	})

	t.Run("internal error response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"code":"internal_error","detail":"database unavailable"}`)
		}))
		defer server.Close()
		client, err := NewHTTPClient(server.URL, "token")
		if err != nil {
			t.Fatal(err)
		}

		_, err = client.Process(context.Background())
		var apiError *APIError
		if !errors.As(err, &apiError) {
			t.Fatalf("error = %v, want APIError", err)
		}
		if apiError.StatusCode != http.StatusInternalServerError || apiError.Code != ErrorInternal || apiError.Detail != "database unavailable" {
			t.Fatalf("API error = %#v", apiError)
		}
		if !errors.Is(err, ErrInternal) {
			t.Fatalf("error = %v, want ErrInternal", err)
		}
	})

	t.Run("unavailable feature response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.EscapedPath() != APIPrefix+"/projects" {
				t.Fatalf("path = %q, want %q", r.URL.EscapedPath(), APIPrefix+"/projects")
			}
			if r.Header.Get("Authorization") != "Bearer token" {
				t.Fatalf("authorization = %q, want bearer token", r.Header.Get("Authorization"))
			}
			w.WriteHeader(http.StatusNotImplemented)
			_, _ = io.WriteString(w, `{"code":"feature_not_configured","detail":"repository discovery is unavailable"}`)
		}))
		defer server.Close()
		client, err := NewHTTPClient(server.URL, "token")
		if err != nil {
			t.Fatal(err)
		}

		_, err = client.ListProjects(context.Background())
		var apiError *APIError
		if !errors.As(err, &apiError) {
			t.Fatalf("error = %v, want APIError", err)
		}
		if apiError.StatusCode != http.StatusNotImplemented || apiError.Code != ErrorFeatureNotConfigured || apiError.Detail != "repository discovery is unavailable" {
			t.Fatalf("API error = %#v", apiError)
		}
		if apiError.Error() != apiError.Detail {
			t.Fatalf("API error string = %q, want daemon detail %q", apiError.Error(), apiError.Detail)
		}
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("error = %v, want ErrUnavailable", err)
		}
	})

	t.Run("unexpected 2xx response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"status":"running"}`)
		}))
		defer server.Close()
		client, err := NewHTTPClient(server.URL, "token")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Process(context.Background()); !errors.Is(err, ErrUnexpectedStatus) {
			t.Fatalf("error = %v, want ErrUnexpectedStatus", err)
		}
	})

	t.Run("oversized response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(w, bytes.NewReader(bytes.Repeat([]byte{'x'}, MaxResponseBytes+1)))
		}))
		defer server.Close()
		client, err := NewHTTPClient(server.URL, "token")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.Process(context.Background()); !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("error = %v, want ErrResponseTooLarge", err)
		}
	})
}

type contractPathFixture struct {
	Project string
}

type contractBodyFixture struct {
	Name string `json:"name"`
}

type contractShapeResponse struct {
	OK bool `json:"ok"`
}

type contractShapeClient struct {
	client *HTTPClient
}

func (c contractShapeClient) PathOnly(ctx context.Context, path contractPathFixture) error {
	route := APIPrefix + "/shape/" + escapePathSegment(path.Project)
	return c.client.do(ctx, MethodPost, route, false, nil, nil, nil, http.StatusNoContent)
}

func (c contractShapeClient) PathAndBody(ctx context.Context, path contractPathFixture, body contractBodyFixture) (contractShapeResponse, error) {
	var response contractShapeResponse
	route := APIPrefix + "/shape/" + escapePathSegment(path.Project) + "/body"
	err := c.client.do(ctx, MethodPost, route, false, nil, body, &response, http.StatusCreated)
	return response, err
}

func (c contractShapeClient) Query(ctx context.Context, query url.Values) (contractShapeResponse, error) {
	var response contractShapeResponse
	err := c.client.do(ctx, MethodGet, APIPrefix+"/shape/query", false, query, nil, &response, http.StatusOK)
	return response, err
}

func TestContractHarnessSeparatesPathQueryAndBody(t *testing.T) {
	var requests []struct {
		method      string
		path        string
		query       url.Values
		rawQuery    string
		contentType string
		body        []byte
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		requests = append(requests, struct {
			method      string
			path        string
			query       url.Values
			rawQuery    string
			contentType string
			body        []byte
		}{method: r.Method, path: r.URL.EscapedPath(), query: r.URL.Query(), rawQuery: r.URL.RawQuery, contentType: r.Header.Get("Content-Type"), body: body})

		switch r.URL.EscapedPath() {
		case APIPrefix + "/shape/demo%2Fblue":
			w.WriteHeader(http.StatusNoContent)
		case APIPrefix + "/shape/demo%2Fblue/body":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"ok":true}`)
		case APIPrefix + "/shape/query":
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"ok":true}`)
		default:
			t.Errorf("unexpected path %q", r.URL.EscapedPath())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewHTTPClient(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	shapeClient := contractShapeClient{client: client}

	pathOnly := Operation{
		Name: "PathOnly", Method: MethodPost, Route: APIPrefix + "/shape/{project}", Path: "ShapePath", SuccessStatus: http.StatusNoContent,
	}
	path := contractPathDTOs["ShapePath"]
	if _, err := callContractOperationParts(shapeClient, pathOnly, path, nil, nil); err != nil {
		t.Fatal(err)
	}

	pathAndBody := Operation{
		Name: "PathAndBody", Method: MethodPost, Route: APIPrefix + "/shape/{project}/body", Path: "ShapePath", Request: "ShapeBody", SuccessStatus: http.StatusCreated,
	}
	body := contractDTOs["ShapeBody"]
	response, err := callContractOperationParts(shapeClient, pathAndBody, path, nil, body)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response, contractShapeResponse{OK: true}) {
		t.Fatalf("response = %#v", response)
	}

	queryOperation := Operation{
		Name: "Query", Method: MethodGet, Route: APIPrefix + "/shape/query", Query: "ShapeQuery", SuccessStatus: http.StatusOK,
	}
	query := contractQueryDTOs["ShapeQuery"]
	response, err = callContractOperationParts(shapeClient, queryOperation, nil, query, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response, contractShapeResponse{OK: true}) {
		t.Fatalf("response = %#v", response)
	}
	emptyQuery := url.Values{}
	response, err = callContractOperationParts(shapeClient, queryOperation, nil, emptyQuery, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response, contractShapeResponse{OK: true}) {
		t.Fatalf("empty query response = %#v", response)
	}

	if len(requests) != 4 {
		t.Fatalf("request count = %d, want 4", len(requests))
	}
	if requests[0].method != http.MethodPost || requests[1].method != http.MethodPost || requests[2].method != http.MethodGet || requests[3].method != http.MethodGet {
		t.Fatalf("methods = %q, %q, %q, %q", requests[0].method, requests[1].method, requests[2].method, requests[3].method)
	}
	if len(requests[0].body) != 0 || len(requests[2].body) != 0 || len(requests[3].body) != 0 {
		t.Fatalf("path-only/query bodies = %q, %q, and %q, want empty", requests[0].body, requests[2].body, requests[3].body)
	}
	if requests[0].contentType != "" || requests[2].contentType != "" || requests[3].contentType != "" {
		t.Fatalf("path-only/query content types = %q, %q, and %q, want empty", requests[0].contentType, requests[2].contentType, requests[3].contentType)
	}
	wantBody := `{"name":"new"}`
	if string(requests[1].body) != wantBody {
		t.Fatalf("path-plus-body body = %s, want %s", requests[1].body, wantBody)
	}
	if requests[1].contentType != "application/json" {
		t.Fatalf("path-plus-body content type = %q, want application/json", requests[1].contentType)
	}
	if !reflect.DeepEqual(requests[2].query, query) {
		t.Fatalf("query = %v, want %v", requests[2].query, query)
	}
	if requests[2].rawQuery != query.Encode() {
		t.Fatalf("raw query = %q, want %q", requests[2].rawQuery, query.Encode())
	}
	if len(requests[3].query) != 0 {
		t.Fatalf("empty query = %v, want empty", requests[3].query)
	}
	if requests[3].rawQuery != "" {
		t.Fatalf("empty raw query = %q, want empty", requests[3].rawQuery)
	}
}
