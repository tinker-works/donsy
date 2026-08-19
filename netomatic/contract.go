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
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Open        bool   `json:"open,omitempty"`
}

type ProjectSummary struct {
	Project          string `json:"project"`
	EpicCount        int    `json:"epic_count"`
	IssueCount       int    `json:"issue_count"`
	PullRequestCount int    `json:"pull_request_count"`
}

type Setup struct {
	Project      string   `json:"project"`
	Repository   string   `json:"repository,omitempty"`
	Organisation string   `json:"organisation,omitempty"`
	Agent        string   `json:"agent,omitempty"`
	Variants     []string `json:"variants,omitempty"`
	Complete     bool     `json:"complete"`
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
	ID      string `json:"id,omitempty"`
	Name    string `json:"name"`
	Owner   string `json:"owner,omitempty"`
	URL     string `json:"url,omitempty"`
	Default string `json:"default_branch,omitempty"`
}

type Organisation struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
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

type Diff struct {
	FilesChanged int    `json:"files_changed,omitempty"`
	Additions    int    `json:"additions,omitempty"`
	Deletions    int    `json:"deletions,omitempty"`
	Patch        string `json:"patch,omitempty"`
}

// ListProjectsResponse and the other response wrappers keep the wire shape
// explicit. This avoids coupling clients to a server's choice of JSON array
// representation and leaves room for pagination metadata later.
type ListProjectsResponse struct {
	Projects []Project `json:"projects"`
}
type CreateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}
type CreateProjectResponse struct {
	Project Project `json:"project"`
}
type OpenProjectRequest struct {
	Project string `json:"project"`
}
type OpenProjectResponse struct {
	Project Project `json:"project"`
}
type ForgetProjectRequest struct {
	Project string `json:"project"`
}
type ForgetProjectResponse struct {
	Forgotten bool `json:"forgotten"`
}
type ProjectSummariesRequest struct {
	Project string `json:"project"`
}
type ProjectSummariesResponse struct {
	Summaries []ProjectSummary `json:"summaries"`
}
type GetSetupRequest struct {
	Project string `json:"project"`
}
type GetSetupResponse struct {
	Setup Setup `json:"setup"`
}
type SaveSetupRequest struct {
	Project string `json:"project"`
	Setup   Setup  `json:"setup"`
}
type SaveSetupResponse struct {
	Setup Setup `json:"setup"`
}
type ProjectPath struct {
	ProjectID uint
}
type EpicPath struct {
	ProjectID uint
	EpicID    string
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
type ListIssuesRequest struct {
	Project string `json:"project"`
	Epic    string `json:"epic"`
}
type ListIssuesResponse struct {
	Issues []Issue `json:"issues"`
}
type GetIssueRequest struct {
	Project string `json:"project"`
	Epic    string `json:"epic"`
	Issue   string `json:"issue"`
}
type GetIssueResponse struct {
	Issue Issue `json:"issue"`
}
type CreateIssueRequest struct {
	Project     string `json:"project"`
	Epic        string `json:"epic"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}
type CreateIssueResponse struct {
	Issue Issue `json:"issue"`
}
type UpdateIssueRequest struct {
	Project     string `json:"project"`
	Epic        string `json:"epic"`
	Issue       string `json:"issue"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}
type UpdateIssueResponse struct {
	Issue Issue `json:"issue"`
}
type TransitionIssueRequest struct {
	Project string `json:"project"`
	Epic    string `json:"epic"`
	Issue   string `json:"issue"`
	Status  string `json:"status"`
}
type TransitionIssueResponse struct {
	Issue Issue `json:"issue"`
}
type CloseIssueRequest struct {
	Project string `json:"project"`
	Epic    string `json:"epic"`
	Issue   string `json:"issue"`
}
type CloseIssueResponse struct {
	Issue Issue `json:"issue"`
}
type ListPullRequestsRequest struct {
	Project string `json:"project"`
	Epic    string `json:"epic"`
	Issue   string `json:"issue"`
}
type ListPullRequestsResponse struct {
	PullRequests []PullRequest `json:"pull_requests"`
}
type CreatePullRequestRequest struct {
	Project     string `json:"project"`
	Epic        string `json:"epic"`
	Issue       string `json:"issue"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Branch      string `json:"branch,omitempty"`
}
type CreatePullRequestResponse struct {
	PullRequest PullRequest `json:"pull_request"`
}
type CommentPullRequestRequest struct {
	Project     string `json:"project"`
	PullRequest string `json:"pull_request"`
	Body        string `json:"body"`
}
type CommentPullRequestResponse struct {
	Comment Comment `json:"comment"`
}
type MergePullRequestRequest struct {
	Project     string `json:"project"`
	PullRequest string `json:"pull_request"`
}
type MergePullRequestResponse struct {
	PullRequest PullRequest `json:"pull_request"`
}
type ClosePullRequestRequest struct {
	Project     string `json:"project"`
	PullRequest string `json:"pull_request"`
}
type ClosePullRequestResponse struct {
	PullRequest PullRequest `json:"pull_request"`
}
type ResetPullRequestRequest struct {
	Project     string `json:"project"`
	PullRequest string `json:"pull_request"`
}
type ResetPullRequestResponse struct {
	PullRequest PullRequest `json:"pull_request"`
}
type GrantPullRequestRequest struct {
	Project     string `json:"project"`
	PullRequest string `json:"pull_request"`
	Branch      string `json:"branch"`
}
type GrantPullRequestResponse struct {
	PullRequest PullRequest `json:"pull_request"`
}
type PullRequestDiffRequest struct {
	Project     string `json:"project"`
	PullRequest string `json:"pull_request"`
}
type PullRequestDiffResponse struct {
	Diff Diff `json:"diff"`
}
type ListRepositoriesRequest struct {
	Organisation string `json:"organisation,omitempty"`
}
type ListRepositoriesResponse struct {
	Repositories []Repository `json:"repositories"`
}
type GetRepositoryRequest struct {
	Organisation string `json:"organisation"`
	Repository   string `json:"repository"`
}
type GetRepositoryResponse struct {
	Repository Repository `json:"repository"`
}
type ListOrganisationsRequest struct{}
type ListOrganisationsResponse struct {
	Organisations []Organisation `json:"organisations"`
}
type GetOrganisationRequest struct {
	Organisation string `json:"organisation"`
}
type GetOrganisationResponse struct {
	Organisation Organisation `json:"organisation"`
}
type GetAgentSettingsPath struct {
	ProjectID uint
}

type SetAgentRolePath struct {
	ProjectID uint
	Role      string
}

type SetAgentRoleRequest struct {
	Agent   string `json:"agent"`
	Variant string `json:"variant"`
}

type ListAgentRunsPath struct {
	ProjectID uint
}

type GetAgentRunPath struct {
	RunID string
}

type RunOutputPath struct {
	RunID string
}

type RunOutputQuery = url.Values

type AgentActivityQuery = url.Values

type ListSandboxesPath struct {
	ProjectID uint
}

type CancelAgentRunPath struct {
	RunID string
}

type CancelAgentRunResponse struct {
	Cancelled bool `json:"cancelled"`
}

type AgentActivityResponse struct {
	Sizes map[string]int64 `json:"sizes"`
}

type ListAgentRunsResponse []AgentRun
type ListSandboxesResponse []Sandbox

type AddRepositoryRequest struct {
	Project string `json:"project"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Branch  string `json:"branch,omitempty"`
}
type AddRepositoryResponse struct {
	Repository Repository `json:"repository"`
}
type RunIssueRequest struct {
	Project string `json:"project"`
	Epic    string `json:"epic"`
	Issue   string `json:"issue"`
}
type RunIssueResponse struct {
	Run AgentRun `json:"run"`
}
type OpenPullRequestsRequest struct {
	Project string `json:"project"`
}
type OpenPullRequestsResponse struct {
	PullRequests []PullRequest `json:"pull_requests"`
}
type TransitionPullRequestRequest struct {
	Project     string `json:"project"`
	PullRequest string `json:"pull_request"`
	Status      string `json:"status"`
}
type TransitionPullRequestResponse struct {
	PullRequest PullRequest `json:"pull_request"`
}
type ReconcileRequest struct {
	Project string `json:"project,omitempty"`
}
type ReconcileResponse struct {
	Reconciled int `json:"reconciled"`
}
type PurgeRequest struct {
	Project string `json:"project,omitempty"`
}
type PurgeResponse struct {
	Purged int `json:"purged"`
}

// Client is the complete public daemon contract. Implementations may use any
// transport, but all methods are context-aware and exchange only these public
// DTOs. Discovery and all other methods require daemon authentication.
type Client interface {
	Process(context.Context) (ProcessResponse, error)
	ListProjects(context.Context) (ListProjectsResponse, error)
	CreateProject(context.Context, CreateProjectRequest) (CreateProjectResponse, error)
	OpenProject(context.Context, OpenProjectRequest) (OpenProjectResponse, error)
	ForgetProject(context.Context, ForgetProjectRequest) (ForgetProjectResponse, error)
	ProjectSummaries(context.Context, ProjectSummariesRequest) (ProjectSummariesResponse, error)
	GetSetup(context.Context, GetSetupRequest) (GetSetupResponse, error)
	SaveSetup(context.Context, SaveSetupRequest) (SaveSetupResponse, error)
	ListEpics(context.Context, ProjectPath) (ListEpicsResponse, error)
	GetEpic(context.Context, EpicPath) (Epic, error)
	CreateEpic(context.Context, ProjectPath, CreateEpicRequest) error
	CloseEpic(context.Context, EpicPath) error
	TransitionEpicState(context.Context, EpicPath, TransitionEpicStateRequest) error
	SetBranchPrefix(context.Context, EpicPath, SetBranchPrefixRequest) error
	CompleteEpic(context.Context, EpicPath) (CompleteEpicResponse, error)
	ReviewApprovedBranches(context.Context, EpicPath) error
	RunEpicAgent(context.Context, EpicPath) error
	ListIssues(context.Context, ListIssuesRequest) (ListIssuesResponse, error)
	GetIssue(context.Context, GetIssueRequest) (GetIssueResponse, error)
	CreateIssue(context.Context, CreateIssueRequest) (CreateIssueResponse, error)
	UpdateIssue(context.Context, UpdateIssueRequest) (UpdateIssueResponse, error)
	TransitionIssue(context.Context, TransitionIssueRequest) (TransitionIssueResponse, error)
	CloseIssue(context.Context, CloseIssueRequest) (CloseIssueResponse, error)
	ListPullRequests(context.Context, ListPullRequestsRequest) (ListPullRequestsResponse, error)
	CreatePullRequest(context.Context, CreatePullRequestRequest) (CreatePullRequestResponse, error)
	CommentPullRequest(context.Context, CommentPullRequestRequest) (CommentPullRequestResponse, error)
	MergePullRequest(context.Context, MergePullRequestRequest) (MergePullRequestResponse, error)
	ClosePullRequest(context.Context, ClosePullRequestRequest) (ClosePullRequestResponse, error)
	ResetPullRequest(context.Context, ResetPullRequestRequest) (ResetPullRequestResponse, error)
	GrantPullRequest(context.Context, GrantPullRequestRequest) (GrantPullRequestResponse, error)
	PullRequestDiff(context.Context, PullRequestDiffRequest) (PullRequestDiffResponse, error)
	ListRepositories(context.Context, ListRepositoriesRequest) (ListRepositoriesResponse, error)
	GetRepository(context.Context, GetRepositoryRequest) (GetRepositoryResponse, error)
	ListOrganisations(context.Context, ListOrganisationsRequest) (ListOrganisationsResponse, error)
	GetOrganisation(context.Context, GetOrganisationRequest) (GetOrganisationResponse, error)
	GetAgentSettings(context.Context, GetAgentSettingsPath) (AgentSettings, error)
	SetAgentRole(context.Context, SetAgentRolePath, SetAgentRoleRequest) error
	ListAgentRuns(context.Context, ListAgentRunsPath) (ListAgentRunsResponse, error)
	GetAgentRun(context.Context, GetAgentRunPath) (AgentRun, error)
	RunOutput(context.Context, RunOutputPath, RunOutputQuery) (RunOutputPage, error)
	AgentActivity(context.Context, AgentActivityQuery) (AgentActivityResponse, error)
	CancelAgentRun(context.Context, CancelAgentRunPath) (CancelAgentRunResponse, error)
	ListSandboxes(context.Context, ListSandboxesPath) (ListSandboxesResponse, error)

	Capabilities(context.Context) (CapabilitiesResponse, error)
	AddRepository(context.Context, AddRepositoryRequest) (AddRepositoryResponse, error)
	RunIssue(context.Context, RunIssueRequest) (RunIssueResponse, error)
	OpenPullRequests(context.Context, OpenPullRequestsRequest) (OpenPullRequestsResponse, error)
	TransitionPullRequest(context.Context, TransitionPullRequestRequest) (TransitionPullRequestResponse, error)
	Reconcile(context.Context, ReconcileRequest) (ReconcileResponse, error)
	Purge(context.Context, PurgeRequest) (PurgeResponse, error)
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
	Authenticated bool
}

const (
	routeProcess = APIPrefix + "/process"
)

// Contract is the complete versioned route inventory. The first 42 rows are
// the original TUI client operations; the remaining rows are daemon routes
// needed by the host.
var Contract = []Operation{
	{Name: "Process", Method: MethodGet, Route: routeProcess, Response: "ProcessResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ListProjects", Method: MethodGet, Route: APIPrefix + "/projects", Response: "ListProjectsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CreateProject", Method: MethodPost, Route: APIPrefix + "/projects", Request: "CreateProjectRequest", Response: "CreateProjectResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "OpenProject", Method: MethodPost, Route: APIPrefix + "/projects/{project}/open", Request: "OpenProjectRequest", Response: "OpenProjectResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ForgetProject", Method: MethodDelete, Route: APIPrefix + "/projects/{project}", Request: "ForgetProjectRequest", Response: "ForgetProjectResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ProjectSummaries", Method: MethodGet, Route: APIPrefix + "/projects/{project}/summaries", Request: "ProjectSummariesRequest", Response: "ProjectSummariesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GetSetup", Method: MethodGet, Route: APIPrefix + "/projects/{project}/setup", Request: "GetSetupRequest", Response: "GetSetupResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "SaveSetup", Method: MethodPut, Route: APIPrefix + "/projects/{project}/setup", Request: "SaveSetupRequest", Response: "SaveSetupResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ListEpics", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/epics", Path: "ProjectPath", Response: "ListEpicsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GetEpic", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}", Path: "EpicPath", Response: "Epic", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CreateEpic", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics", Path: "ProjectPath", Request: "CreateEpicRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "CloseEpic", Method: MethodDelete, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}", Path: "EpicPath", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "TransitionEpicState", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/state-transitions", Path: "EpicPath", Request: "TransitionEpicStateRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "SetBranchPrefix", Method: MethodPut, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/branch-prefix", Path: "EpicPath", Request: "SetBranchPrefixRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "CompleteEpic", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/complete", Path: "EpicPath", Response: "CompleteEpicResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ReviewApprovedBranches", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/review-approved-branches", Path: "EpicPath", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "RunEpicAgent", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/agent-runs", Path: "EpicPath", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ListIssues", Method: MethodGet, Route: APIPrefix + "/projects/{project}/epics/{epic}/issues", Request: "ListIssuesRequest", Response: "ListIssuesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GetIssue", Method: MethodGet, Route: APIPrefix + "/projects/{project}/epics/{epic}/issues/{issue}", Request: "GetIssueRequest", Response: "GetIssueResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CreateIssue", Method: MethodPost, Route: APIPrefix + "/projects/{project}/epics/{epic}/issues", Request: "CreateIssueRequest", Response: "CreateIssueResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "UpdateIssue", Method: MethodPut, Route: APIPrefix + "/projects/{project}/epics/{epic}/issues/{issue}", Request: "UpdateIssueRequest", Response: "UpdateIssueResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "TransitionIssue", Method: MethodPost, Route: APIPrefix + "/projects/{project}/epics/{epic}/issues/{issue}/transition", Request: "TransitionIssueRequest", Response: "TransitionIssueResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CloseIssue", Method: MethodPost, Route: APIPrefix + "/projects/{project}/epics/{epic}/issues/{issue}/close", Request: "CloseIssueRequest", Response: "CloseIssueResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ListPullRequests", Method: MethodGet, Route: APIPrefix + "/projects/{project}/epics/{epic}/issues/{issue}/pull-requests", Request: "ListPullRequestsRequest", Response: "ListPullRequestsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CreatePullRequest", Method: MethodPost, Route: APIPrefix + "/projects/{project}/epics/{epic}/issues/{issue}/pull-requests", Request: "CreatePullRequestRequest", Response: "CreatePullRequestResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CommentPullRequest", Method: MethodPost, Route: APIPrefix + "/projects/{project}/pull-requests/{pull_request}/comments", Request: "CommentPullRequestRequest", Response: "CommentPullRequestResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "MergePullRequest", Method: MethodPost, Route: APIPrefix + "/projects/{project}/pull-requests/{pull_request}/merge", Request: "MergePullRequestRequest", Response: "MergePullRequestResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ClosePullRequest", Method: MethodPost, Route: APIPrefix + "/projects/{project}/pull-requests/{pull_request}/close", Request: "ClosePullRequestRequest", Response: "ClosePullRequestResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ResetPullRequest", Method: MethodPost, Route: APIPrefix + "/projects/{project}/pull-requests/{pull_request}/reset", Request: "ResetPullRequestRequest", Response: "ResetPullRequestResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GrantPullRequest", Method: MethodPost, Route: APIPrefix + "/projects/{project}/pull-requests/{pull_request}/grant", Request: "GrantPullRequestRequest", Response: "GrantPullRequestResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "PullRequestDiff", Method: MethodGet, Route: APIPrefix + "/projects/{project}/pull-requests/{pull_request}/diff", Request: "PullRequestDiffRequest", Response: "PullRequestDiffResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ListRepositories", Method: MethodGet, Route: APIPrefix + "/repositories", Request: "ListRepositoriesRequest", Response: "ListRepositoriesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GetRepository", Method: MethodGet, Route: APIPrefix + "/repositories/{repository}", Request: "GetRepositoryRequest", Response: "GetRepositoryResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ListOrganisations", Method: MethodGet, Route: APIPrefix + "/organisations", Request: "ListOrganisationsRequest", Response: "ListOrganisationsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GetOrganisation", Method: MethodGet, Route: APIPrefix + "/organisations/{organisation}", Request: "GetOrganisationRequest", Response: "GetOrganisationResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GetAgentSettings", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/agent-settings", Path: "GetAgentSettingsPath", Response: "AgentSettings", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "SetAgentRole", Method: MethodPut, Route: APIPrefix + "/projects/{projectID}/agent-settings/roles/{role}", Path: "SetAgentRolePath", Request: "SetAgentRoleRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "ListAgentRuns", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/agent-runs", Path: "ListAgentRunsPath", Response: "ListAgentRunsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "RunOutput", Method: MethodGet, Route: APIPrefix + "/agent-runs/{runID}/output", Path: "RunOutputPath", Query: "RunOutputQuery", Response: "RunOutputPage", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "AgentActivity", Method: MethodGet, Route: APIPrefix + "/agent-runs/activity", Query: "AgentActivityQuery", Response: "AgentActivityResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CancelAgentRun", Method: MethodPost, Route: APIPrefix + "/agent-runs/{runID}/cancel", Path: "CancelAgentRunPath", Response: "CancelAgentRunResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ListSandboxes", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/sandboxes", Path: "ListSandboxesPath", Response: "ListSandboxesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "Capabilities", Method: MethodGet, Route: APIPrefix + "/capabilities", Response: "CapabilitiesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "AddRepository", Method: MethodPost, Route: APIPrefix + "/repositories", Request: "AddRepositoryRequest", Response: "AddRepositoryResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GetAgentRun", Method: MethodGet, Route: APIPrefix + "/agent-runs/{runID}", Path: "GetAgentRunPath", Response: "AgentRun", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "RunIssue", Method: MethodPost, Route: APIPrefix + "/runs/issue", Request: "RunIssueRequest", Response: "RunIssueResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "OpenPullRequests", Method: MethodGet, Route: APIPrefix + "/open-pull-requests", Request: "OpenPullRequestsRequest", Response: "OpenPullRequestsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "TransitionPullRequest", Method: MethodPost, Route: APIPrefix + "/pull-requests/{pull_request}/transition", Request: "TransitionPullRequestRequest", Response: "TransitionPullRequestResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "Reconcile", Method: MethodPost, Route: APIPrefix + "/reconcile", Request: "ReconcileRequest", Response: "ReconcileResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "Purge", Method: MethodPost, Route: APIPrefix + "/purge", Request: "PurgeRequest", Response: "PurgeResponse", SuccessStatus: http.StatusOK, Authenticated: true},
}

// ContractOperations returns a copy so callers cannot mutate the package's
// route registry.
func ContractOperations() []Operation {
	return append([]Operation(nil), Contract...)
}

// ClientOperationCount is the number of retained operations that originated
// at the old TUI boundary. It intentionally excludes daemon-only routes and
// ReadDaemonLog.
const ClientOperationCount = 42

// DaemonOperationCount includes every row in Contract.
const DaemonOperationCount = 50

// ValidateContract catches accidental omissions when a route is added to the
// table without a corresponding public DTO or method declaration.
func ValidateContract() error {
	if len(Contract) != DaemonOperationCount {
		return fmt.Errorf("netomatic: contract has %d operations, want %d", len(Contract), DaemonOperationCount)
	}
	seen := make(map[string]struct{}, len(Contract))
	for _, operation := range Contract {
		if err := validateOperation(operation); err != nil {
			return err
		}
		if _, ok := seen[operation.Name]; ok {
			return fmt.Errorf("netomatic: duplicate operation %q", operation.Name)
		}
		seen[operation.Name] = struct{}{}
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
	if operation.SuccessStatus < http.StatusOK || operation.SuccessStatus >= http.StatusMultipleChoices {
		return fmt.Errorf("netomatic: invalid success status %d for %q", operation.SuccessStatus, operation.Name)
	}
	return nil
}
