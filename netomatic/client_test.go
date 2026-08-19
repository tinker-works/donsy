package netomatic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
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
	var requestAuthorization string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestMethod = r.Method
		requestPath = r.URL.EscapedPath()
		requestAuthorization = r.Header.Get("Authorization")
		requestBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(responseBody)
	}))
	defer server.Close()

	client, err := NewHTTPClient(server.URL, token)
	if err != nil {
		t.Fatal(err)
	}

	for _, operation = range Contract {
		t.Run(operation.Name, func(t *testing.T) {
			request := contractFixtureValue(contractDTOs[operation.Request])
			response := contractFixtureValue(contractDTOs[operation.Response])
			responseBody, err = contractFixtureJSON(contractDTOs[operation.Response])
			if err != nil {
				t.Fatal(err)
			}
			requestBody = nil
			requestMethod = ""
			requestPath = ""
			requestAuthorization = ""

			got, err := callContractOperation(client, operation, request)
			if err != nil {
				t.Fatalf("%s(): %v", operation.Name, err)
			}
			if !reflect.DeepEqual(got, response) {
				t.Fatalf("%s() response = %#v, want %#v", operation.Name, got, response)
			}

			if requestMethod != string(operation.Method) {
				t.Errorf("method = %q, want %q", requestMethod, operation.Method)
			}
			if requestPath != contractTestRoute(operation, request) {
				t.Errorf("path = %q, want %q", requestPath, contractTestRoute(operation, request))
			}
			wantAuthorization := ""
			if operation.Authenticated {
				wantAuthorization = "Bearer " + token
			}
			if requestAuthorization != wantAuthorization {
				t.Errorf("authorization = %q, want %q", requestAuthorization, wantAuthorization)
			}

			var wantBody []byte
			if operation.Request != "" {
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

func callContractOperation(client Client, operation Operation, request any) (any, error) {
	ctx := context.Background()
	method := reflect.ValueOf(client).MethodByName(operation.Name)
	if !method.IsValid() {
		return nil, errors.New("missing client method")
	}

	args := []reflect.Value{reflect.ValueOf(ctx)}
	if operation.Name == "ReadDaemonLog" {
		logRequest := request.(ReadDaemonLogRequest)
		args = append(args, reflect.ValueOf(logRequest.Offset), reflect.ValueOf(logRequest.Limit))
	} else if operation.Request != "" {
		args = append(args, reflect.ValueOf(request))
	}
	results := method.Call(args)
	if errValue := results[1].Interface(); errValue != nil {
		return nil, errValue.(error)
	}
	return results[0].Interface(), nil
}

func contractTestRoute(operation Operation, request any) string {
	route := operation.Route
	if request == nil {
		return route
	}
	value := reflect.ValueOf(request)
	for placeholder, fieldName := range map[string]string{
		"project":      "Project",
		"epic":         "Epic",
		"issue":        "Issue",
		"pull_request": "PullRequest",
		"repository":   "Repository",
		"organisation": "Organisation",
		"run":          "Run",
	} {
		field := value.FieldByName(fieldName)
		if field.IsValid() && field.Kind() == reflect.String {
			route = strings.Replace(route, "{"+placeholder+"}", url.PathEscape(field.String()), 1)
		}
	}
	return route
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

func TestHTTPClientBoundsDaemonLogRequest(t *testing.T) {
	var body ReadDaemonLogRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encoded, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if err := json.Unmarshal(encoded, &body); err != nil {
			t.Error(err)
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
	if body.Offset != 8 || body.Limit != MaxDaemonLogLines || !reflect.DeepEqual(response.Lines, []string{"line"}) {
		t.Fatalf("request = %#v, response = %#v", body, response)
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
