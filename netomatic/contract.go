// Package netomatic contains the public protocol shared by donsy and its
// clients.  The package deliberately contains transport types only; it does
// not expose any of donsy's domain or storage packages.
package netomatic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
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

	// MaxDaemonLogBytes and MaxDaemonLogLines are enforced by the daemon for
	// every log page. A line is never split; a single oversized line is skipped
	// and the page offset advances past it.
	MaxDaemonLogBytes = 64 * 1024
	MaxDaemonLogLines = 1000
	// MaxLogBytes and MaxLogLines are shorter names for clients that do not
	// need to distinguish daemon logs from other future log streams.
	MaxLogBytes = MaxDaemonLogBytes
	MaxLogLines = MaxDaemonLogLines
)

var (
	ErrInvalidProtocol  = errors.New("netomatic: incompatible protocol")
	ErrInvalidLogOffset = errors.New("netomatic: log offset must not be negative")
	ErrInvalidLogLimit  = errors.New("netomatic: log limit must be positive")
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
	Agent   string            `json:"agent"`
	Variant string            `json:"variant,omitempty"`
	Values  map[string]string `json:"values,omitempty"`
}

type AgentRun struct {
	ID           string `json:"id"`
	Project      string `json:"project,omitempty"`
	Agent        string `json:"agent,omitempty"`
	Variant      string `json:"variant,omitempty"`
	Status       string `json:"status"`
	SessionID    string `json:"session_id,omitempty"`
	Error        string `json:"error,omitempty"`
	StartedAt    string `json:"started_at,omitempty"`
	FinishedAt   string `json:"finished_at,omitempty"`
	InputTokens  int64  `json:"input_tokens,omitempty"`
	OutputTokens int64  `json:"output_tokens,omitempty"`
}

type Sandbox struct {
	ID         string `json:"id"`
	Name       string `json:"name,omitempty"`
	Status     string `json:"status"`
	AgentRunID string `json:"agent_run_id,omitempty"`
}

type AgentActivity struct {
	RunID     string `json:"run_id"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type RunOutput struct {
	RunID  string `json:"run_id"`
	Output string `json:"output"`
	Done   bool   `json:"done"`
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
type ListEpicsRequest struct {
	Project string `json:"project"`
}
type ListEpicsResponse struct {
	Epics []Epic `json:"epics"`
}
type GetEpicRequest struct {
	Project string `json:"project"`
	Epic    string `json:"epic"`
}
type GetEpicResponse struct {
	Epic Epic `json:"epic"`
}
type CreateEpicRequest struct {
	Project     string `json:"project"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}
type CreateEpicResponse struct {
	Epic Epic `json:"epic"`
}
type PrefixEpicRequest struct {
	Project string `json:"project"`
	Epic    string `json:"epic"`
	Prefix  string `json:"prefix"`
}
type PrefixEpicResponse struct {
	Epic Epic `json:"epic"`
}
type TransitionEpicRequest struct {
	Project string `json:"project"`
	Epic    string `json:"epic"`
	Status  string `json:"status"`
}
type TransitionEpicResponse struct {
	Epic Epic `json:"epic"`
}
type CloseEpicRequest struct {
	Project string `json:"project"`
	Epic    string `json:"epic"`
}
type CloseEpicResponse struct {
	Epic Epic `json:"epic"`
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
type ProjectPath struct{ ProjectID uint }
type RunIssueAgentPath struct {
	ProjectID       uint
	EpicID, IssueID string
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
type GetAgentSettingsRequest struct {
	Project string `json:"project"`
}
type GetAgentSettingsResponse struct {
	Settings []AgentSettings `json:"settings"`
}
type ListAgentRunsRequest struct {
	Project string `json:"project,omitempty"`
}
type ListAgentRunsResponse struct {
	Runs []AgentRun `json:"runs"`
}
type ListSandboxesRequest struct{}
type ListSandboxesResponse struct {
	Sandboxes []Sandbox `json:"sandboxes"`
}
type CancelAgentRunRequest struct {
	Run string `json:"run"`
}
type CancelAgentRunResponse struct {
	Run AgentRun `json:"run"`
}
type AgentActivityRequest struct {
	Run string `json:"run"`
}
type AgentActivityResponse struct {
	Activity []AgentActivity `json:"activity"`
}
type RunOutputRequest struct {
	Run    string `json:"run"`
	Offset int64  `json:"offset,omitempty"`
}
type RunOutputResponse struct {
	Output RunOutput `json:"output"`
}

// ReadDaemonLogRequest addresses the daemon log by byte offset. Offset zero
// starts at the beginning; clients should use NextOffset from the prior page.
type ReadDaemonLogRequest struct {
	Offset int64 `json:"offset"`
	Limit  int   `json:"limit"`
}

// ReadDaemonLogResponse contains only complete newline-delimited log lines.
// Oversized records are omitted, but still advance NextOffset. If the daemon
// has rotated or truncated the file and Offset is no longer valid, it starts
// at zero and reports OffsetReset=true.
type ReadDaemonLogResponse struct {
	Lines       []string `json:"lines"`
	NextOffset  int64    `json:"next_offset"`
	OffsetReset bool     `json:"offset_reset"`
}

// DaemonLogPage and LogPage are descriptive aliases for the paginated log
// response used by different client layers.
type DaemonLogPage = ReadDaemonLogResponse
type LogPage = ReadDaemonLogResponse

// BoundDaemonLogRequest applies the server's positive limits before reading a
// log. Limits above the maximum are clamped so an old client cannot request an
// unbounded response. An offset beyond the current file size is reset by the
// server when it reads the file; this function only validates the request.
func BoundDaemonLogRequest(request ReadDaemonLogRequest) (ReadDaemonLogRequest, error) {
	if request.Offset < 0 {
		return ReadDaemonLogRequest{}, ErrInvalidLogOffset
	}
	if request.Limit <= 0 {
		return ReadDaemonLogRequest{}, ErrInvalidLogLimit
	}
	if request.Limit > MaxDaemonLogLines {
		request.Limit = MaxDaemonLogLines
	}
	return request, nil
}

// PageDaemonLog applies the daemon log pagination rules to content read from
// the daemon's log file. It is useful to HTTP adapters and keeps the tricky
// offset behavior independent of filesystem code. Lines do not include their
// terminating newline. An unterminated final line is held for the next page.
func PageDaemonLog(content []byte, request ReadDaemonLogRequest) (ReadDaemonLogResponse, error) {
	request, err := BoundDaemonLogRequest(request)
	if err != nil {
		return ReadDaemonLogResponse{}, err
	}

	start := request.Offset
	reset := start > int64(len(content))
	if reset {
		start = 0
	}

	// Requests normally use a previous NextOffset, but a caller may provide an
	// offset in the middle of a line. Skip that partial line rather than
	// returning a fragment.
	if start > 0 && content[start-1] != '\n' {
		if newline := bytes.IndexByte(content[start:], '\n'); newline >= 0 {
			start += int64(newline + 1)
		} else {
			return ReadDaemonLogResponse{NextOffset: int64(len(content)), OffsetReset: reset}, nil
		}
	}

	cursor := start
	usedBytes := 0
	lines := make([]string, 0, request.Limit)
	for len(lines) < request.Limit && cursor < int64(len(content)) {
		relativeNewline := bytes.IndexByte(content[cursor:], '\n')
		if relativeNewline < 0 {
			break
		}
		lineEnd := cursor + int64(relativeNewline)
		next := lineEnd + 1
		lineBytes := int(next - cursor)
		if lineBytes > MaxDaemonLogBytes {
			// Oversized lines cannot be returned whole without breaking the byte
			// bound. Skip the complete record so a client can continue polling.
			cursor = next
			continue
		}
		if usedBytes+lineBytes > MaxDaemonLogBytes {
			break
		}
		lines = append(lines, string(content[cursor:lineEnd]))
		usedBytes += lineBytes
		cursor = next
	}

	return ReadDaemonLogResponse{Lines: lines, NextOffset: cursor, OffsetReset: reset}, nil
}

type AddRepositoryRequest struct {
	Project string `json:"project"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Branch  string `json:"branch,omitempty"`
}
type AddRepositoryResponse struct {
	Repository Repository `json:"repository"`
}
type GetAgentRunRequest struct {
	Run string `json:"run"`
}
type GetAgentRunResponse struct {
	Run AgentRun `json:"run"`
}
type CompleteRequest struct {
	Project string `json:"project"`
	Run     string `json:"run"`
}
type CompleteResponse struct {
	Complete bool `json:"complete"`
}
type ReviewApprovedBranchesRequest struct {
	Project string `json:"project"`
}
type ReviewApprovedBranchesResponse struct {
	Branches []string `json:"branches"`
}
type RunEpicRequest struct {
	Project string `json:"project"`
	Epic    string `json:"epic"`
}
type RunEpicResponse struct {
	Run AgentRun `json:"run"`
}
type RunIssueRequest struct {
	Project string `json:"project"`
	Epic    string `json:"epic"`
	Issue   string `json:"issue"`
}
type RunIssueResponse struct {
	Run AgentRun `json:"run"`
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
	ListEpics(context.Context, ListEpicsRequest) (ListEpicsResponse, error)
	GetEpic(context.Context, GetEpicRequest) (GetEpicResponse, error)
	CreateEpic(context.Context, CreateEpicRequest) (CreateEpicResponse, error)
	PrefixEpic(context.Context, PrefixEpicRequest) (PrefixEpicResponse, error)
	TransitionEpic(context.Context, TransitionEpicRequest) (TransitionEpicResponse, error)
	CloseEpic(context.Context, CloseEpicRequest) (CloseEpicResponse, error)
	ListIssues(context.Context, ListIssuesRequest) (ListIssuesResponse, error)
	GetIssue(context.Context, GetIssueRequest) (GetIssueResponse, error)
	CreateIssue(context.Context, CreateIssueRequest) (CreateIssueResponse, error)
	UpdateIssue(context.Context, UpdateIssueRequest) (UpdateIssueResponse, error)
	TransitionIssue(context.Context, TransitionIssueRequest) (TransitionIssueResponse, error)
	CloseIssue(context.Context, CloseIssueRequest) (CloseIssueResponse, error)
	CreatePullRequest(context.Context, CreatePullRequestPath, CreatePullRequestRequest) error
	TransitionPullRequest(context.Context, TransitionPullRequestPath, TransitionPullRequestRequest) error
	GrantCodingRound(context.Context, GrantCodingRoundPath) error
	MergePullRequest(context.Context, MergePullRequestPath) (MergePullRequestResponse, error)
	ResetIssue(context.Context, ResetIssuePath) error
	GetPullRequestDiff(context.Context, GetPullRequestDiffPath) (PullRequestDiffResponse, error)
	OpenPullRequests(context.Context, OpenPullRequestsPath) (OpenPullRequestsResponse, error)
	AddComment(context.Context, AddCommentPath, AddCommentRequest) error
	ListRepositories(context.Context, ListRepositoriesRequest) (ListRepositoriesResponse, error)
	GetRepository(context.Context, GetRepositoryRequest) (GetRepositoryResponse, error)
	ListOrganisations(context.Context, ListOrganisationsRequest) (ListOrganisationsResponse, error)
	GetOrganisation(context.Context, GetOrganisationRequest) (GetOrganisationResponse, error)
	GetAgentSettings(context.Context, GetAgentSettingsRequest) (GetAgentSettingsResponse, error)
	ListAgentRuns(context.Context, ListAgentRunsRequest) (ListAgentRunsResponse, error)
	ListSandboxes(context.Context, ListSandboxesRequest) (ListSandboxesResponse, error)
	CancelAgentRun(context.Context, CancelAgentRunRequest) (CancelAgentRunResponse, error)
	AgentActivity(context.Context, AgentActivityRequest) (AgentActivityResponse, error)
	RunOutput(context.Context, RunOutputRequest) (RunOutputResponse, error)
	ReadDaemonLog(context.Context, int64, int) (ReadDaemonLogResponse, error)

	Capabilities(context.Context) (CapabilitiesResponse, error)
	AddRepository(context.Context, AddRepositoryRequest) (AddRepositoryResponse, error)
	GetAgentRun(context.Context, GetAgentRunRequest) (GetAgentRunResponse, error)
	Complete(context.Context, CompleteRequest) (CompleteResponse, error)
	ReviewApprovedBranches(context.Context, ReviewApprovedBranchesRequest) (ReviewApprovedBranchesResponse, error)
	RunEpic(context.Context, RunEpicRequest) (RunEpicResponse, error)
	RunIssue(context.Context, RunIssueRequest) (RunIssueResponse, error)
	RunIssueAgent(context.Context, RunIssueAgentPath) error
	ReconcileSandboxes(context.Context, ProjectPath) error
	PurgeFinishedWork(context.Context, ProjectPath) error
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

// Contract is the complete versioned route inventory. The first 38 rows are
// the original TUI client operations; the remaining rows are daemon routes
// needed by the host and the public log operation.
var Contract = []Operation{
	{Name: "Process", Method: MethodGet, Route: routeProcess, Response: "ProcessResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ListProjects", Method: MethodGet, Route: APIPrefix + "/projects", Response: "ListProjectsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CreateProject", Method: MethodPost, Route: APIPrefix + "/projects", Request: "CreateProjectRequest", Response: "CreateProjectResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "OpenProject", Method: MethodPost, Route: APIPrefix + "/projects/{project}/open", Request: "OpenProjectRequest", Response: "OpenProjectResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ForgetProject", Method: MethodDelete, Route: APIPrefix + "/projects/{project}", Request: "ForgetProjectRequest", Response: "ForgetProjectResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ProjectSummaries", Method: MethodGet, Route: APIPrefix + "/projects/{project}/summaries", Request: "ProjectSummariesRequest", Response: "ProjectSummariesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GetSetup", Method: MethodGet, Route: APIPrefix + "/projects/{project}/setup", Request: "GetSetupRequest", Response: "GetSetupResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "SaveSetup", Method: MethodPut, Route: APIPrefix + "/projects/{project}/setup", Request: "SaveSetupRequest", Response: "SaveSetupResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ListEpics", Method: MethodGet, Route: APIPrefix + "/projects/{project}/epics", Request: "ListEpicsRequest", Response: "ListEpicsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GetEpic", Method: MethodGet, Route: APIPrefix + "/projects/{project}/epics/{epic}", Request: "GetEpicRequest", Response: "GetEpicResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CreateEpic", Method: MethodPost, Route: APIPrefix + "/projects/{project}/epics", Request: "CreateEpicRequest", Response: "CreateEpicResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "PrefixEpic", Method: MethodPost, Route: APIPrefix + "/projects/{project}/epics/{epic}/prefix", Request: "PrefixEpicRequest", Response: "PrefixEpicResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "TransitionEpic", Method: MethodPost, Route: APIPrefix + "/projects/{project}/epics/{epic}/transition", Request: "TransitionEpicRequest", Response: "TransitionEpicResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CloseEpic", Method: MethodPost, Route: APIPrefix + "/projects/{project}/epics/{epic}/close", Request: "CloseEpicRequest", Response: "CloseEpicResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ListIssues", Method: MethodGet, Route: APIPrefix + "/projects/{project}/epics/{epic}/issues", Request: "ListIssuesRequest", Response: "ListIssuesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GetIssue", Method: MethodGet, Route: APIPrefix + "/projects/{project}/epics/{epic}/issues/{issue}", Request: "GetIssueRequest", Response: "GetIssueResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CreateIssue", Method: MethodPost, Route: APIPrefix + "/projects/{project}/epics/{epic}/issues", Request: "CreateIssueRequest", Response: "CreateIssueResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "UpdateIssue", Method: MethodPut, Route: APIPrefix + "/projects/{project}/epics/{epic}/issues/{issue}", Request: "UpdateIssueRequest", Response: "UpdateIssueResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "TransitionIssue", Method: MethodPost, Route: APIPrefix + "/projects/{project}/epics/{epic}/issues/{issue}/transition", Request: "TransitionIssueRequest", Response: "TransitionIssueResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CloseIssue", Method: MethodPost, Route: APIPrefix + "/projects/{project}/epics/{epic}/issues/{issue}/close", Request: "CloseIssueRequest", Response: "CloseIssueResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CreatePullRequest", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests", Path: "CreatePullRequestPath", Request: "CreatePullRequestRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "TransitionPullRequest", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/state-transitions", Path: "TransitionPullRequestPath", Request: "TransitionPullRequestRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "GrantCodingRound", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/coding-rounds", Path: "GrantCodingRoundPath", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "MergePullRequest", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/merge", Path: "MergePullRequestPath", Response: "MergePullRequestResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ResetIssue", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/reset", Path: "ResetIssuePath", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "GetPullRequestDiff", Method: MethodGet, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/diff", Path: "GetPullRequestDiffPath", Response: "PullRequestDiffResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "OpenPullRequests", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/open-pull-requests", Path: "OpenPullRequestsPath", Response: "OpenPullRequestsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "AddComment", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/comments", Path: "AddCommentPath", Request: "AddCommentRequest", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "ListRepositories", Method: MethodGet, Route: APIPrefix + "/repositories", Request: "ListRepositoriesRequest", Response: "ListRepositoriesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GetRepository", Method: MethodGet, Route: APIPrefix + "/repositories/{repository}", Request: "GetRepositoryRequest", Response: "GetRepositoryResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ListOrganisations", Method: MethodGet, Route: APIPrefix + "/organisations", Request: "ListOrganisationsRequest", Response: "ListOrganisationsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GetOrganisation", Method: MethodGet, Route: APIPrefix + "/organisations/{organisation}", Request: "GetOrganisationRequest", Response: "GetOrganisationResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GetAgentSettings", Method: MethodGet, Route: APIPrefix + "/projects/{project}/agent-settings", Request: "GetAgentSettingsRequest", Response: "GetAgentSettingsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ListAgentRuns", Method: MethodGet, Route: APIPrefix + "/projects/{project}/agent-runs", Request: "ListAgentRunsRequest", Response: "ListAgentRunsResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ListSandboxes", Method: MethodGet, Route: APIPrefix + "/sandboxes", Request: "ListSandboxesRequest", Response: "ListSandboxesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "CancelAgentRun", Method: MethodPost, Route: APIPrefix + "/agent-runs/{run}/cancel", Request: "CancelAgentRunRequest", Response: "CancelAgentRunResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "AgentActivity", Method: MethodGet, Route: APIPrefix + "/agent-runs/{run}/activity", Request: "AgentActivityRequest", Response: "AgentActivityResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "RunOutput", Method: MethodGet, Route: APIPrefix + "/agent-runs/{run}/output", Request: "RunOutputRequest", Response: "RunOutputResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "Capabilities", Method: MethodGet, Route: APIPrefix + "/capabilities", Response: "CapabilitiesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "AddRepository", Method: MethodPost, Route: APIPrefix + "/repositories", Request: "AddRepositoryRequest", Response: "AddRepositoryResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "GetAgentRun", Method: MethodGet, Route: APIPrefix + "/agent-runs/{run}", Request: "GetAgentRunRequest", Response: "GetAgentRunResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "Complete", Method: MethodPost, Route: APIPrefix + "/complete", Request: "CompleteRequest", Response: "CompleteResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "ReviewApprovedBranches", Method: MethodPost, Route: APIPrefix + "/review-approved-branches", Request: "ReviewApprovedBranchesRequest", Response: "ReviewApprovedBranchesResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "RunEpic", Method: MethodPost, Route: APIPrefix + "/runs/epic", Request: "RunEpicRequest", Response: "RunEpicResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "RunIssue", Method: MethodPost, Route: APIPrefix + "/runs/issue", Request: "RunIssueRequest", Response: "RunIssueResponse", SuccessStatus: http.StatusOK, Authenticated: true},
	{Name: "RunIssueAgent", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/epics/{epicID}/issues/{issueID}/agent-runs", Path: "RunIssueAgentPath", SuccessStatus: http.StatusNoContent, Authenticated: true},
	{Name: "ReconcileSandboxes", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/maintenance/reconcile", Path: "ProjectPath", Unavailable: true, Authenticated: true},
	{Name: "PurgeFinishedWork", Method: MethodPost, Route: APIPrefix + "/projects/{projectID}/maintenance/purge", Path: "ProjectPath", Unavailable: true, Authenticated: true},
	{Name: "ReadDaemonLog", Method: MethodGet, Route: APIPrefix + "/daemon-log", Response: "ReadDaemonLogResponse", SuccessStatus: http.StatusOK, Authenticated: true},
}

// ContractOperations returns a copy so callers cannot mutate the package's
// route registry.
func ContractOperations() []Operation {
	return append([]Operation(nil), Contract...)
}

// ClientOperationCount is the number of retained operations that originated
// at the old TUI boundary. It intentionally excludes daemon-only routes and
// ReadDaemonLog.
const ClientOperationCount = 38

// DaemonOperationCount includes every row in Contract.
const DaemonOperationCount = 49

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
