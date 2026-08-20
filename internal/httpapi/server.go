// Package httpapi exposes donsy's application use cases over the netomatic API.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tinker-works/donsy/internal/application/usecases"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/netomatic"
)

const Address = "127.0.0.1:8337"

type Server struct {
	useCases      *usecases.UseCases
	logger        *slog.Logger
	handler       http.Handler
	token         string
	daemonLogPath string
	mutationMu    sync.Mutex
}

// New creates a loopback API server that requires a bearer token for every API
// route.
func New(useCases *usecases.UseCases, logger *slog.Logger, tokens ...string) (*Server, error) {
	if useCases == nil {
		return nil, errors.New("HTTP API requires use cases")
	}
	if len(tokens) != 1 || strings.TrimSpace(tokens[0]) == "" {
		return nil, errors.New("HTTP API requires a non-empty daemon token")
	}
	if logger == nil {
		logger = slog.Default()
	}
	server := &Server{useCases: useCases, logger: logger, token: tokens[0]}
	mux := http.NewServeMux()
	server.registerRoutes(mux)
	server.handler = server.middleware(mux)
	return server, nil
}

// NewWithDaemonLog creates a server that can serve bounded daemon-log pages.
func NewWithDaemonLog(useCases *usecases.UseCases, logger *slog.Logger, daemonLogPath string, tokens ...string) (*Server, error) {
	server, err := New(useCases, logger, tokens...)
	if err != nil {
		return nil, err
	}
	server.daemonLogPath = daemonLogPath
	return server, nil
}

func (s *Server) Handler() http.Handler { return s.handler }

func (s *Server) HTTPServer(baseContext context.Context) *http.Server {
	if baseContext == nil {
		baseContext = context.Background()
	}
	return &http.Server{
		Addr:              Address,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       time.Minute,
		ErrorLog:          slog.NewLogLogger(s.logger.Handler(), slog.LevelError),
		BaseContext: func(net.Listener) context.Context {
			return baseContext
		},
	}
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/process", s.process)
	mux.HandleFunc("GET /api/v1/capabilities", s.capabilities)
	mux.HandleFunc("GET /api/v1/projects", s.listProjects)
	mux.HandleFunc("POST /api/v1/projects", s.createProject)
	mux.HandleFunc("GET /api/v1/projects/summaries", s.listProjectSummaries)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/open", s.openProject)
	mux.HandleFunc("DELETE /api/v1/projects/{projectID}", s.forgetProject)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/setup", s.storeSetup)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/setup", s.initialiseStore)
	mux.HandleFunc("GET /api/v1/projects/{project}/epics", s.listEpics)
	mux.HandleFunc("POST /api/v1/projects/{project}/epics", s.createEpic)
	mux.HandleFunc("GET /api/v1/projects/{project}/epics/{epic}", s.getEpic)
	mux.HandleFunc("POST /api/v1/projects/{project}/epics/{epic}/prefix", s.prefixEpic)
	mux.HandleFunc("POST /api/v1/projects/{project}/epics/{epic}/transition", s.transitionEpic)
	mux.HandleFunc("POST /api/v1/projects/{project}/epics/{epic}/close", s.closeEpic)
	mux.HandleFunc("GET /api/v1/projects/{project}/epics/{epic}/issues", s.listIssues)
	mux.HandleFunc("POST /api/v1/projects/{project}/epics/{epic}/issues", s.createIssue)
	mux.HandleFunc("GET /api/v1/projects/{project}/epics/{epic}/issues/{issue}", s.getIssue)
	mux.HandleFunc("PUT /api/v1/projects/{project}/epics/{epic}/issues/{issue}", s.unavailable("updating issues"))
	mux.HandleFunc("POST /api/v1/projects/{project}/epics/{epic}/issues/{issue}/transition", s.unavailable("transitioning issues"))
	mux.HandleFunc("POST /api/v1/projects/{project}/epics/{epic}/issues/{issue}/close", s.closeIssue)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/epics/{epicID}/pull-requests", s.createPullRequest)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/state-transitions", s.transitionPullRequest)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/coding-rounds", s.grantCodingRound)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/merge", s.mergePullRequest)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/reset", s.resetIssue)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/diff", s.getPullRequestDiff)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/epics/{epicID}/open-pull-requests", s.openPullRequests)
	mux.HandleFunc("POST /api/v1/projects/{projectID}/epics/{epicID}/comments", s.addComment)
	mux.HandleFunc("GET /api/v1/organisations", s.listOrganisations)
	mux.HandleFunc("POST /api/v1/organisations", s.addOrganisation)
	mux.HandleFunc("DELETE /api/v1/organisations/{name}", s.removeOrganisation)
	mux.HandleFunc("POST /api/v1/organisations/discovery", s.discoverOrganisations)
	mux.HandleFunc("GET /api/v1/repositories", s.listRepositories)
	mux.HandleFunc("POST /api/v1/repositories", s.addRepository)
	mux.HandleFunc("POST /api/v1/repositories/sync", s.syncRepositories)
	mux.HandleFunc("GET /api/v1/projects/{projectID}/repositories", s.listProjectRepositories)
	mux.HandleFunc("PUT /api/v1/projects/{projectID}/repositories", s.updateProjectRepositories)
	mux.HandleFunc("GET /api/v1/projects/{project}/agent-settings", s.getAgentSettings)
	mux.HandleFunc("GET /api/v1/projects/{project}/agent-runs", s.listAgentRuns)
	mux.HandleFunc("GET /api/v1/sandboxes", s.listSandboxes)
	mux.HandleFunc("POST /api/v1/agent-runs/{run}/cancel", s.cancelAgentRun)
	mux.HandleFunc("GET /api/v1/agent-runs/{run}/activity", s.agentActivity)
	mux.HandleFunc("GET /api/v1/agent-runs/{run}/output", s.runOutput)
	mux.HandleFunc("GET /api/v1/agent-runs/{run}", s.getAgentRun)
	mux.HandleFunc("POST /api/v1/complete", s.completeEpic)
	mux.HandleFunc("POST /api/v1/review-approved-branches", s.reviewApprovedBranches)
	mux.HandleFunc("POST /api/v1/runs/epic", s.runEpic)
	mux.HandleFunc("POST /api/v1/runs/issue", s.runIssue)
	mux.HandleFunc("POST /api/v1/reconcile", s.unavailable("sandbox reconciliation"))
	mux.HandleFunc("POST /api/v1/purge", s.unavailable("finished-work purge"))
	mux.HandleFunc("GET /api/v1/daemon-log", s.readDaemonLog)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, netomatic.APIPrefix+"/") && r.Header.Get("Authorization") != "Bearer "+s.token {
			s.writeError(w, http.StatusUnauthorized, netomatic.ErrorUnauthorized, "a valid daemon token is required")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			origin := r.Header.Get("Origin")
			if origin != "" && origin != "http://127.0.0.1:8337" && origin != "http://localhost:8337" {
				s.writeError(w, http.StatusBadRequest, netomatic.ErrorInvalidRequest, "Origin is not allowed")
				return
			}
			s.mutationMu.Lock()
			defer s.mutationMu.Unlock()
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) decode(r *http.Request, value any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errInvalidRequest("Content-Type must be application/json")
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return errInvalidRequest("request body must be valid JSON")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errInvalidRequest("request body must contain one JSON value")
	}
	return nil
}

func (s *Server) projectID(r *http.Request) (domain.Project, error) {
	id, err := strconv.ParseUint(r.PathValue("projectID"), 10, 64)
	if err != nil || id == 0 {
		return domain.Project{}, errInvalidRequest("projectID must be a positive integer")
	}
	return s.findProject(func(project domain.Project) bool { return project.ID == uint(id) })
}

func (s *Server) projectName(r *http.Request) (domain.Project, error) {
	return s.projectNameValue(r.PathValue("project"))
}

func (s *Server) projectNameValue(name string) (domain.Project, error) {
	if strings.TrimSpace(name) == "" {
		return domain.Project{}, errInvalidRequest("project is required")
	}
	return s.findProject(func(project domain.Project) bool { return project.Name == name })
}

func (s *Server) findProject(match func(domain.Project) bool) (domain.Project, error) {
	if s.useCases.ListProjects == nil {
		return domain.Project{}, errUnavailable("listing projects")
	}
	projects, err := s.useCases.ListProjects.Handle(usecases.ListProjectsQuery{})
	if err != nil {
		return domain.Project{}, err
	}
	for _, project := range projects {
		if match(project) {
			return project, nil
		}
	}
	return domain.Project{}, errNotFound("project")
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		s.logger.Error("write HTTP API response", "error", err)
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, code netomatic.ErrorCode, detail string) {
	s.writeJSON(w, status, netomatic.ErrorResponse{Code: code, Detail: detail})
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	var invalid invalidRequest
	if errors.As(err, &invalid) {
		s.writeError(w, http.StatusBadRequest, netomatic.ErrorInvalidRequest, invalid.Error())
		return
	}
	var unavailable featureUnavailable
	if errors.As(err, &unavailable) {
		s.writeError(w, http.StatusNotImplemented, netomatic.ErrorFeatureNotConfigured, unavailable.Error())
		return
	}
	var notFound resourceNotFound
	if errors.As(err, &notFound) {
		s.writeError(w, http.StatusNotFound, netomatic.ErrorNotFound, notFound.Error())
		return
	}
	if errors.Is(err, os.ErrNotExist) {
		s.writeError(w, http.StatusNotFound, netomatic.ErrorNotFound, err.Error())
		return
	}
	s.logger.Error("HTTP API request failed", "error", err)
	s.writeError(w, http.StatusInternalServerError, netomatic.ErrorInternal, "the daemon could not process the request")
}

type invalidRequest string

func (e invalidRequest) Error() string      { return string(e) }
func errInvalidRequest(detail string) error { return invalidRequest(detail) }

type featureUnavailable string

func (e featureUnavailable) Error() string { return string(e) + " is not configured for this process" }
func errUnavailable(feature string) error  { return featureUnavailable(feature) }

type resourceNotFound string

func (e resourceNotFound) Error() string { return string(e) + " was not found" }
func errNotFound(resource string) error  { return resourceNotFound(resource) }

func (s *Server) unavailable(feature string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		s.fail(w, errUnavailable(feature))
	}
}
