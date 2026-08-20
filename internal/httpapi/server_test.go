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

	"github.com/tinker-works/donsy/internal/application/usecases"
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
