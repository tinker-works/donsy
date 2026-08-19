package netomatic

import (
	"bytes"
	"context"
	"encoding/json"
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
			responseBody, err = contractFixtureJSON(contractDTOs[operation.Response])
			if err != nil {
				t.Fatal(err)
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
				wantBody, err = contractFixtureJSON(request)
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

func contractFixtureValue(value any) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	decoded := reflect.New(reflect.TypeOf(value))
	if err := json.Unmarshal(encoded, decoded.Interface()); err != nil {
		panic(err)
	}
	return decoded.Elem().Interface()
}

func contractFixtureJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func callContractOperationParts(client any, operation Operation, path any, query url.Values, request any) (any, error) {
	ctx := context.Background()
	method := reflect.ValueOf(client).MethodByName(operation.Name)
	if !method.IsValid() {
		return nil, errors.New("missing client method")
	}

	args := []reflect.Value{reflect.ValueOf(ctx)}
	if operation.Name == "ReadDaemonLog" {
		logRequest := request.(ReadDaemonLogRequest)
		args = append(args, reflect.ValueOf(logRequest.Offset), reflect.ValueOf(logRequest.Limit))
	} else {
		if operation.Path != "" {
			args = append(args, reflect.ValueOf(path))
		}
		if operation.Query != "" {
			args = append(args, reflect.ValueOf(query))
		}
		if operation.Request != "" {
			args = append(args, reflect.ValueOf(request))
		}
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
		if field.IsValid() && field.Kind() == reflect.String {
			route = strings.Replace(route, "{"+placeholder+"}", url.PathEscape(field.String()), 1)
		}
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
		return contractDTOs[operation.Request]
	}
	if operation.Name == "ReadDaemonLog" {
		return contractDTOs["ReadDaemonLogRequest"]
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
			_, _ = io.WriteString(w, `{"code":"unauthorized","message":"bad token","details":{"source":"test"}}`)
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

func TestHTTPClientBoundsDaemonLogRequest(t *testing.T) {
	var query url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		if r.ContentLength != 0 {
			t.Errorf("GET content length = %d, want 0", r.ContentLength)
		}
		_, _ = io.WriteString(w, `{"lines":["line"],"next_offset":12,"offset_reset":false}`)
	}))
	defer server.Close()

	client, err := NewHTTPClient(server.URL, "token")
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.ReadDaemonLog(context.Background(), 8, MaxDaemonLogLines+1)
	if err != nil {
		t.Fatal(err)
	}
	wantQuery := url.Values{"offset": {"8"}, "limit": {strconv.Itoa(MaxDaemonLogLines)}}
	if !reflect.DeepEqual(query, wantQuery) || !reflect.DeepEqual(response.Lines, []string{"line"}) {
		t.Fatalf("query = %v, response = %#v", query, response)
	}

	for _, test := range []struct {
		name   string
		offset int64
		limit  int
		want   error
	}{
		{name: "negative offset", offset: -1, limit: 1, want: ErrInvalidLogOffset},
		{name: "zero limit", offset: 0, limit: 0, want: ErrInvalidLogLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := client.ReadDaemonLog(context.Background(), test.offset, test.limit)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want errors.Is(_, %v)", err, test.want)
			}
		})
	}
}
