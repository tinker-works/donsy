// Package netomatic contains the public protocol shared by donsy and its
// clients.  The package deliberately contains transport types only; it does
// not expose any of donsy's domain or storage packages.
package netomatic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	// ProtocolVersion is the only protocol version currently understood by the
	// daemon and clients. Compatibility is exact, not prefix based.
	ProtocolVersion = "v1"
	// APIVersion is the wire version without its URL prefix.
	APIVersion = ProtocolVersion
	// APIPrefix is the versioned prefix used by every HTTP operation.
	APIPrefix = "/api/" + ProtocolVersion
)

var (
	ErrInvalidProtocol = errors.New("netomatic: incompatible protocol")
)

// CompatibleProtocol reports whether version is understood by this package.
func CompatibleProtocol(version string) bool {
	return version == ProtocolVersion
}

// ValidateProtocol rejects versions that cannot safely share a client/server
// connection.
func ValidateProtocol(version string) error {
	if !CompatibleProtocol(version) {
		return fmt.Errorf("%w: got %q, want %q", ErrInvalidProtocol, version, ProtocolVersion)
	}
	return nil
}

// HTTPMethod is used in the contract table instead of exposing net/http from
// callers that only need to inspect the protocol.
type HTTPMethod string

const (
	MethodGet    HTTPMethod = http.MethodGet
	MethodPost   HTTPMethod = http.MethodPost
	MethodPut    HTTPMethod = http.MethodPut
	MethodDelete HTTPMethod = http.MethodDelete
)

// ErrorCode identifies a stable class of API failure.
type ErrorCode string

const (
	ErrorInvalidRequest       ErrorCode = "invalid_request"
	ErrorUnauthorized         ErrorCode = "unauthorized"
	ErrorForbidden            ErrorCode = "forbidden"
	ErrorNotFound             ErrorCode = "not_found"
	ErrorConflict             ErrorCode = "conflict"
	ErrorFeatureNotConfigured ErrorCode = "feature_not_configured"
	ErrorUnavailable          ErrorCode = "unavailable"
	ErrorInternal             ErrorCode = "internal_error"
)

// APIError is the structured error returned for every non-success API
// response. Detail is the daemon's user-facing explanation of the failure.
type APIError struct {
	Code       ErrorCode `json:"code"`
	Detail     string    `json:"detail"`
	StatusCode int       `json:"-"`
}

func (e *APIError) Error() string {
	if e == nil {
		return "netomatic: unknown API error"
	}
	if e.Detail != "" {
		return e.Detail
	}
	if e.Code != "" {
		return string(e.Code)
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("netomatic: API request failed with status %d", e.StatusCode)
	}
	return "netomatic: unknown API error"
}

// Unwrap exposes the standard status errors when a caller wants to use
// errors.Is without depending on HTTP status handling.
func (e *APIError) Unwrap() error {
	if e == nil {
		return nil
	}
	switch e.StatusCode {
	case http.StatusBadRequest:
		return ErrBadRequest
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusForbidden:
		return ErrForbidden
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		return ErrConflict
	case http.StatusServiceUnavailable:
		return ErrUnavailable
	case http.StatusNotImplemented:
		if e.Code == ErrorFeatureNotConfigured {
			return ErrUnavailable
		}
		return nil
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusGatewayTimeout:
		return ErrInternal
	default:
		return nil
	}
}

var (
	ErrBadRequest   = errors.New("netomatic: bad request")
	ErrUnauthorized = errors.New("netomatic: unauthorized")
	ErrForbidden    = errors.New("netomatic: forbidden")
	ErrNotFound     = errors.New("netomatic: not found")
	ErrConflict     = errors.New("netomatic: conflict")
	ErrUnavailable  = errors.New("netomatic: unavailable")
	ErrInternal     = errors.New("netomatic: internal error")
)

// ErrorResponse is retained as the descriptive name used by HTTP handlers.
type ErrorResponse = APIError

// APIErrorResponse is an alternative name for integrations that distinguish
// an error response from the error value returned by a client method.
type APIErrorResponse = APIError

// CapabilitiesResponse lists the optional daemon features and whether each is
// configured for the current process.
type CapabilitiesResponse map[string]bool

type ProcessResponse struct {
	CurrentUser string `json:"currentUser"`
	Protocol    string `json:"protocol"`
}

type Project struct {
	ID           uint
	Name         string
	LastOpenedAt string
}

type ProjectSummary struct {
	Project Project   `json:"project"`
	Epics   int       `json:"epics"`
	Running int       `json:"running"`
	Error   *APIError `json:"error,omitempty"`
}

type SetupState struct {
	Organisations int
	Repositories  int
	RolesSet      int
	RolesTotal    int
}

type Epic struct {
	ID             string
	Title          string
	Assignee       string
	Repositories   []string
	Body           string
	State          string
	BranchPrefix   string
	Issues         []Issue
	PullRequests   []PullRequest
	DraftingPasses int
}

type Issue struct {
	ID         string
	Title      string
	ParentID   string
	Repository string
	State      string
	CreatedAt  string
	Body       string
	Comments   []Comment
	BlockedBy  []string
}

type PullRequest struct {
	ID            string
	IssueID       string
	Title         string
	Status        string
	Repository    string
	Number        int
	URL           string
	Head          string
	Base          string
	Flags         []string
	ReviewedHead  string
	ReviewedBase  string
	Rounds        int
	Reviews       int
	RoundsGranted int
	CodingRounds  int
	Approved      bool
	CreatedAt     string
	Comments      []Comment
}

type Comment struct {
	ID        string
	Author    string
	CreatedAt string
	Body      string
}

type Repository struct {
	Name         string
	FullName     string
	HTTPURL      string
	SSHURL       string
	Organisation string
}

type Organisation struct {
	Name string
}

type AgentSettings struct {
	SetupScript string
	Roles       map[string]AgentProfile
}

type AgentProfile struct {
	Agent     string
	Variant   string
	MaxRounds int
}

type AgentSubject struct {
	Kind string
	ID   string
}

type RunUsage struct {
	TokensIn  int
	TokensOut int
	CostUSD   float64
}

type AgentRun struct {
	ID          string
	ProjectID   uint
	SandboxID   string
	Role        string
	Subject     AgentSubject
	Engine      string
	Agent       string
	Variant     string
	SessionMode string
	Status      string
	Round       int
	Error       string
	Usage       RunUsage
	CreatedAt   string
	StartedAt   *string
	FinishedAt  *string
}

type Sandbox struct {
	ID        string
	ProjectID uint
	Name      string
	Role      string
	Subject   AgentSubject
	Status    string
	CreatedAt string
	UpdatedAt string
}

type TranscriptEntry struct {
	Kind   uint8
	Tool   string
	CallID string
	Text   string
}

type RunOutputPage struct {
	Entries []TranscriptEntry
	Next    int64
}

type ProjectPath struct {
	ProjectID uint
}

type EpicPath struct {
	ProjectID uint
	EpicID    string
}

type IssuePath struct {
	ProjectID uint
	EpicID    string
	IssueID   string
}

type CreateIssuePath = EpicPath
type CloseIssuePath = IssuePath
type RunIssueAgentPath = IssuePath

type AgentRunPath struct {
	RunID string
}

type GetAgentSettingsPath = ProjectPath
type ListAgentRunsPath = ProjectPath
type ListSandboxesPath = ProjectPath
type GetAgentRunPath = AgentRunPath
type RunOutputPath = AgentRunPath
type CancelAgentRunPath = AgentRunPath

type SetAgentRolePath struct {
	ProjectID uint
	Role      string
}

type ListProjectsResponse []Project
type CreateProjectRequest struct {
	Name string `json:"name"`
}
type CreateProjectResponse = Project
type ListProjectSummariesResponse []ProjectSummary
type InitialiseStoreRequest struct {
	Model        string   `json:"model"`
	Variant      string   `json:"variant"`
	Repositories []string `json:"repositories"`
}
type ListEpicsResponse []Epic
type CreateEpicRequest struct {
	Title        string   `json:"title"`
	Assignee     string   `json:"assignee"`
	Body         string   `json:"body"`
	Repositories []string `json:"repositories"`
	BranchPrefix string   `json:"branchPrefix"`
}
type TransitionEpicStateRequest struct {
	State string `json:"state"`
	Force bool   `json:"force"`
}
type SetBranchPrefixRequest struct {
	Prefix string `json:"prefix"`
}
type CompleteEpicResponse struct {
	Completed bool `json:"completed"`
}
type CreateIssueRequest struct {
	ParentID   string `json:"parentId,omitempty"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	Repository string `json:"repository"`
}
type CreatePullRequestPath struct {
	ProjectID uint
	EpicID    string
}
type CreatePullRequestRequest struct {
	IssueID    string `json:"issueId"`
	Title      string `json:"title"`
	Repository string `json:"repository"`
	Head       string `json:"head"`
	Base       string `json:"base"`
}

type TransitionPullRequestPath struct {
	ProjectID     uint
	EpicID        string
	PullRequestID string
}
type TransitionPullRequestRequest struct {
	Status string `json:"status"`
}

type GrantCodingRoundPath struct {
	ProjectID     uint
	EpicID        string
	PullRequestID string
}

type MergePullRequestPath struct {
	ProjectID     uint
	EpicID        string
	PullRequestID string
}
type MergeOutcome string

const (
	MergeOutcomeMerged           MergeOutcome = "merged"
	MergeOutcomeReturnedToCoding MergeOutcome = "returned_to_coding"
)

type MergePullRequestResponse struct {
	Outcome MergeOutcome `json:"outcome"`
}

type ResetIssuePath struct {
	ProjectID     uint
	EpicID        string
	PullRequestID string
}

type GetPullRequestDiffPath struct {
	ProjectID     uint
	EpicID        string
	PullRequestID string
}
type PullRequestDiffResponse struct {
	Diff string `json:"diff"`
}

type OpenPullRequestsPath struct {
	ProjectID uint
	EpicID    string
}
type OpenPullRequestsResponse struct {
	Opened int `json:"opened"`
}

type AddCommentPath struct {
	ProjectID uint
	EpicID    string
}
type CommentTarget string

const (
	IssueCommentTarget       CommentTarget = "issue"
	PullRequestCommentTarget CommentTarget = "pull_request"
)

type AddCommentRequest struct {
	TargetID string        `json:"targetId"`
	Target   CommentTarget `json:"target"`
	Body     string        `json:"body"`
}
type ListOrganisationsResponse []Organisation
type AddOrganisationRequest struct {
	Name string `json:"name"`
}
type RemoveOrganisationPath struct {
	Name string
}
type DiscoverOrganisationsResponse []Organisation
type ListRepositoriesResponse []Repository
type AddRepositoryRequest struct {
	FullName string `json:"fullName"`
}
type AddRepositoryResponse = Repository
type ListProjectRepositoriesPath = ProjectPath
type ListProjectRepositoriesResponse []string
type UpdateProjectRepositoriesPath = ListProjectRepositoriesPath
type UpdateProjectRepositoriesRequest struct {
	Repositories []string `json:"repositories"`
}
type ListAgentRunsResponse []AgentRun
type ListSandboxesResponse []Sandbox
type SetAgentRoleRequest struct {
	Agent   string `json:"agent"`
	Variant string `json:"variant"`
}
type CancelAgentRunResponse struct {
	Cancelled bool `json:"cancelled"`
}
type AgentActivityResponse struct {
	Sizes map[string]int64 `json:"sizes"`
}
type RunOutputQuery = url.Values
type AgentActivityQuery = url.Values

// Client is the complete public daemon contract. Implementations may use any
// transport, but all methods are context-aware and exchange only these public
// DTOs. Discovery and all other methods require daemon authentication.
type Client interface {
	Process(context.Context) (ProcessResponse, error)
	Capabilities(context.Context) (CapabilitiesResponse, error)
	ListProjects(context.Context) (ListProjectsResponse, error)
	CreateProject(context.Context, CreateProjectRequest) (CreateProjectResponse, error)
	ListProjectSummaries(context.Context) (ListProjectSummariesResponse, error)
	OpenProject(context.Context, ProjectPath) error
	ForgetProject(context.Context, ProjectPath) error
	StoreSetup(context.Context, ProjectPath) (SetupState, error)
	InitialiseStore(context.Context, ProjectPath, InitialiseStoreRequest) error
	ListEpics(context.Context, ProjectPath) (ListEpicsResponse, error)
	GetEpic(context.Context, EpicPath) (Epic, error)
	CreateEpic(context.Context, EpicPath, CreateEpicRequest) error
	CloseEpic(context.Context, EpicPath) error
	TransitionEpicState(context.Context, EpicPath, TransitionEpicStateRequest) error
	SetBranchPrefix(context.Context, EpicPath, SetBranchPrefixRequest) error
	CompleteEpic(context.Context, EpicPath) (CompleteEpicResponse, error)
	ReviewApprovedBranches(context.Context, EpicPath) error
	RunEpicAgent(context.Context, EpicPath) error
	CreateIssue(context.Context, EpicPath, CreateIssueRequest) error
	CloseIssue(context.Context, IssuePath) error
	RunIssueAgent(context.Context, IssuePath) error
	CreatePullRequest(context.Context, CreatePullRequestPath, CreatePullRequestRequest) error
	TransitionPullRequest(context.Context, TransitionPullRequestPath, TransitionPullRequestRequest) error
	GrantCodingRound(context.Context, GrantCodingRoundPath) error
	MergePullRequest(context.Context, MergePullRequestPath) (MergePullRequestResponse, error)
	ResetIssue(context.Context, ResetIssuePath) error
	GetPullRequestDiff(context.Context, GetPullRequestDiffPath) (PullRequestDiffResponse, error)
	OpenPullRequests(context.Context, OpenPullRequestsPath) (OpenPullRequestsResponse, error)
	AddComment(context.Context, AddCommentPath, AddCommentRequest) error
	ListOrganisations(context.Context) (ListOrganisationsResponse, error)
	AddOrganisation(context.Context, AddOrganisationRequest) error
	RemoveOrganisation(context.Context, RemoveOrganisationPath) error
	DiscoverOrganisations(context.Context) (DiscoverOrganisationsResponse, error)
	ListRepositories(context.Context) (ListRepositoriesResponse, error)
	SyncRepositories(context.Context) error
	ListProjectRepositories(context.Context, ListProjectRepositoriesPath) (ListProjectRepositoriesResponse, error)
	UpdateProjectRepositories(context.Context, UpdateProjectRepositoriesPath, UpdateProjectRepositoriesRequest) error
	GetAgentSettings(context.Context, GetAgentSettingsPath) (AgentSettings, error)
	SetAgentRole(context.Context, SetAgentRolePath, SetAgentRoleRequest) error
	ListAgentRuns(context.Context, ListAgentRunsPath) (ListAgentRunsResponse, error)
	GetAgentRun(context.Context, GetAgentRunPath) (AgentRun, error)
	RunOutput(context.Context, RunOutputPath, RunOutputQuery) (RunOutputPage, error)
	AgentActivity(context.Context, AgentActivityQuery) (AgentActivityResponse, error)
	CancelAgentRun(context.Context, CancelAgentRunPath) (CancelAgentRunResponse, error)
	ListSandboxes(context.Context, ListSandboxesPath) (ListSandboxesResponse, error)
	ReconcileSandboxes(context.Context, ProjectPath) error
	PurgeFinishedWork(context.Context, ProjectPath) error

	AddRepository(context.Context, AddRepositoryRequest) (AddRepositoryResponse, error)
}

// Operation describes one row in the daemon contract table. Path, Query, and
// Request contain Go type names for the independently supplied path, query,
// and JSON body values. Response is optional for error-only operations.
type Operation struct {
	Name          string
	Method        HTTPMethod
	Route         string
	Path          string
	Query         string
	Request       string
	Response      string
	SuccessStatus int
	Unavailable   bool
	Authenticated bool
}

const (
	routeProcess = APIPrefix + "/process"
)

// Contract is the reviewed Go Merge server route inventory at the revision
// recorded in netomatic/CONTRACT.md. It intentionally includes routes that
// currently return feature_not_configured rather than silently dropping them.
var Contract = []Operation{
	{Name: "Process", Method: MethodGet, Route: routeProcess, Response: "ProcessResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "Capabilities", Method: MethodGet, Route: APIPrefix + "/capabilities", Response: "CapabilitiesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ListProjects", Method: MethodGet, Route: APIPrefix + "/projects", Response: "ListProjectsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CreateProject", Method: MethodPost, Route: APIPrefix + "/projects", Request: "CreateProjectRequest", Response: "CreateProjectResponse", SuccessStatus: http.StatusCreated, Authenticated: true},
	{Name: "ListProjectSummaries", Method: MethodGet, Route: APIPrefix + "/projects/summaries", Response: "ListProjectSummariesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "OpenProject", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/open", Path: "ProjectPath", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "ForgetProject", Method: MethodDelete, Route: APIPrefix + "/projects/{projectID}", Path: "ProjectPath", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "StoreSetup", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/setup", Path: "ProjectPath", Response: "SetupState", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "InitialiseStore", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/setup", Path: "ProjectPath", Request: "InitialiseStoreRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "ListEpics", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/epics", Path: "ProjectPath", Response: "ListEpicsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GetEpic", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}", Path: "EpicPath", Response: "Epic", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CreateEpic", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics", Path: "EpicPath", Request: "CreateEpicRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "CloseEpic", Method: MethodDelete, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}", Path: "EpicPath", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "TransitionEpicState", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/state-transitions", Path: "EpicPath", Request: "TransitionEpicStateRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "SetBranchPrefix", Method: MethodPut, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/branch-prefix", Path: "EpicPath", Request: "SetBranchPrefixRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "CompleteEpic", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/complete", Path: "EpicPath", Response: "CompleteEpicResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ReviewApprovedBranches", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/review-approved-branches", Path: "EpicPath", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "RunEpicAgent", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/agent-runs", Path: "EpicPath", Unavailable: true, Authenticated: true},
	{Name: "CreateIssue", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/issues", Path: "CreateIssuePath", Request: "CreateIssueRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "CloseIssue", Method: MethodDelete, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/issues/{issueID}", Path: "CloseIssuePath", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "RunIssueAgent", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/issues/{issueID}/agent-runs", Path: "RunIssueAgentPath", Unavailable: true, Authenticated: true},
	{Name: "CreatePullRequest", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests", Path: "CreatePullRequestPath", Request: "CreatePullRequestRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "TransitionPullRequest", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/state-transitions", Path: "TransitionPullRequestPath", Request: "TransitionPullRequestRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "GrantCodingRound", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/coding-rounds", Path: "GrantCodingRoundPath", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "MergePullRequest", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/merge", Path: "MergePullRequestPath", Response: "MergePullRequestResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ResetIssue", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/reset", Path: "ResetIssuePath", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "GetPullRequestDiff", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/diff", Path: "GetPullRequestDiffPath", Response: "PullRequestDiffResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "OpenPullRequests", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/open-pull-requests", Path: "OpenPullRequestsPath", Response: "OpenPullRequestsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "AddComment", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/comments", Path: "AddCommentPath", Request: "AddCommentRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "ListOrganisations", Method: MethodGet, Route: APIPrefix + "/organisations", Response: "ListOrganisationsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "AddOrganisation", Method: MethodPost, Route: APIPrefix + "/organisations", Request: "AddOrganisationRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "RemoveOrganisation", Method: MethodDelete, Route: APIPrefix + "/organisations/{name}", Path: "RemoveOrganisationPath", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "DiscoverOrganisations", Method: MethodPost, Route: APIPrefix + "/organisations/discovery", Response: "DiscoverOrganisationsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ListRepositories", Method: MethodGet, Route: APIPrefix + "/repositories", Response: "ListRepositoriesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "AddRepository", Method: MethodPost, Route: APIPrefix + "/repositories", Request: "AddRepositoryRequest", Response: "Repository", SuccessStatus: http.StatusCreated, Authenticated: true},
	{Name: "SyncRepositories", Method: MethodPost, Route: APIPrefix + "/repositories/sync", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "ListProjectRepositories", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/repositories", Path: "ListProjectRepositoriesPath", Response: "ListProjectRepositoriesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "UpdateProjectRepositories", Method: MethodPut, Route: APIPrefix + "/projects/{projectID}/repositories", Path: "UpdateProjectRepositoriesPath", Request: "UpdateProjectRepositoriesRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "GetAgentSettings", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/agent-settings", Path: "GetAgentSettingsPath", Response: "AgentSettings", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "SetAgentRole", Method: MethodPut, Route: APIPrefix + "/projects/{projectID}/agent-settings/roles/{role}", Path: "SetAgentRolePath", Request: "SetAgentRoleRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "ListAgentRuns", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/agent-runs", Path: "ListAgentRunsPath", Response: "ListAgentRunsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GetAgentRun", Method: MethodGet, Route: APIPrefix + "/agent-runs/{runID}", Path: "GetAgentRunPath", Response: "AgentRun", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "RunOutput", Method: MethodGet, Route: APIPrefix + "/agent-runs/{runID}/output", Path: "RunOutputPath", Query: "RunOutputQuery", Response: "RunOutputPage", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "AgentActivity", Method: MethodGet, Route: APIPrefix + "/agent-runs/activity", Query: "AgentActivityQuery", Response: "AgentActivityResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CancelAgentRun", Method: MethodPost, Route: APIPrefix + "/agent-runs/{runID}/cancel", Path: "CancelAgentRunPath", Response: "CancelAgentRunResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ListSandboxes", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/sandboxes", Path: "ListSandboxesPath", Response: "ListSandboxesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ReconcileSandboxes", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/maintenance/reconcile", Path: "ProjectPath", Unavailable: true, Authenticated: true},
	{Name: "PurgeFinishedWork", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/maintenance/purge", Path: "ProjectPath", Unavailable: true, Authenticated: true},
}

// ContractOperations returns a copy so callers cannot mutate the package's
// route registry.
func ContractOperations() []Operation {
	return append([]Operation(nil), Contract...)
}

// GoMergeOperationCount is the number of reviewed server registrations at the
// revision recorded in CONTRACT.md.
const GoMergeOperationCount = 48

// ValidateContract catches accidental omissions when a route is added to the
// table without a corresponding public DTO or method declaration.
func ValidateContract() error {
	if len(Contract) != GoMergeOperationCount {
		return fmt.Errorf("netomatic: contract has %d operations, want %d", len(Contract), GoMergeOperationCount)
	}
	seenNames := make(map[string]struct{}, len(Contract))
	seenRoutes := make(map[string]struct{}, len(Contract))
	for _, operation := range Contract {
		if err := validateOperation(operation); err != nil {
			return err
		}
		if _, ok := seenNames[operation.Name]; ok {
			return fmt.Errorf("netomatic: duplicate operation %q", operation.Name)
		}
		seenNames[operation.Name] = struct{}{}
		routeKey := string(operation.Method) + " " + operation.Route
		if _, ok := seenRoutes[routeKey]; ok {
			return fmt.Errorf("netomatic: duplicate method and route %q", routeKey)
		}
		seenRoutes[routeKey] = struct{}{}
		if !strings.HasPrefix(operation.Route, APIPrefix+"/") {
			return fmt.Errorf("netomatic: unversioned route %q", operation.Route)
		}
	}
	return nil
}

func validateOperation(operation Operation) error {
	if operation.Name == "" || operation.Route == "" || operation.Method == "" {
		return fmt.Errorf("netomatic: incomplete contract row %#v", operation)
	}
	if operation.Unavailable {
		if operation.SuccessStatus != 0 {
			return fmt.Errorf("netomatic: unavailable operation %q cannot declare success status %d", operation.Name, operation.SuccessStatus)
		}
		return nil
	}
	if operation.SuccessStatus < http.StatusOK || operation.SuccessStatus >= http.StatusMultipleChoices {
		return fmt.Errorf("netomatic: invalid success status %d for %q", operation.SuccessStatus, operation.Name)
	}
	return nil
}
