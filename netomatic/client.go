package netomatic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const MaxResponseBytes = 8 * 1024 * 1024

var ErrResponseTooLarge = errors.New("netomatic: response exceeds maximum size")
var ErrUnexpectedStatus = errors.New("netomatic: unexpected success status")

// HTTPClient implements Client over the daemon's versioned HTTP API.
type HTTPClient struct {
	baseURL    *url.URL
	token      string
	httpClient *http.Client
}

// NewHTTPClient creates an authenticated client for an HTTP or HTTPS daemon
// endpoint.
func NewHTTPClient(baseURL, token string) (*HTTPClient, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("netomatic: invalid base URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("netomatic: invalid base URL %q", baseURL)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("netomatic: base URL must not contain query or fragment")
	}

	return &HTTPClient{
		baseURL:    parsed,
		token:      token,
		httpClient: http.DefaultClient,
	}, nil
}

// NewClient is the concise constructor for the public HTTP client.
func NewClient(baseURL, token string) (*HTTPClient, error) {
	return NewHTTPClient(baseURL, token)
}

func (c *HTTPClient) endpoint(route string, query url.Values) *url.URL {
	target := *c.baseURL
	escapedPath := strings.TrimRight(c.baseURL.EscapedPath(), "/") + route
	target.Path, _ = url.PathUnescape(escapedPath)
	target.RawPath = escapedPath
	target.RawQuery = query.Encode()
	target.Fragment = ""
	return &target
}

func escapePathSegment(value string) string {
	return url.PathEscape(value)
}

func projectRoute(projectID uint, suffix string) string {
	return APIPrefix + "/projects/" + strconv.FormatUint(uint64(projectID), 10) + suffix
}

func epicRoute(path EpicPath, suffix string) string {
	return projectRoute(path.ProjectID, "/epics/"+escapePathSegment(path.EpicID)+suffix)
}

func (c *HTTPClient) do(ctx context.Context, method HTTPMethod, route string, authenticated bool, query url.Values, request any, response any, expectedStatus int) error {
	if expectedStatus < http.StatusOK || expectedStatus >= http.StatusMultipleChoices {
		return fmt.Errorf("netomatic: invalid expected success status %d", expectedStatus)
	}

	var body io.Reader
	if request != nil && method != MethodGet {
		encoded, err := json.Marshal(request)
		if err != nil {
			return fmt.Errorf("netomatic: marshal %s %s request: %w", method, route, err)
		}
		body = bytes.NewReader(encoded)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, string(method), c.endpoint(route, query).String(), body)
	if err != nil {
		return fmt.Errorf("netomatic: create %s request: %w", method, err)
	}
	if body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	if authenticated {
		httpRequest.Header.Set("Authorization", "Bearer "+c.token)
	}

	httpResponse, err := c.httpClient.Do(httpRequest)
	if err != nil {
		return fmt.Errorf("netomatic: %s %s: %w", method, route, err)
	}
	defer func() { _ = httpResponse.Body.Close() }()

	limited := io.LimitReader(httpResponse.Body, MaxResponseBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return fmt.Errorf("netomatic: read %s %s response: %w", method, route, err)
	}
	if int64(len(payload)) > MaxResponseBytes {
		return ErrResponseTooLarge
	}

	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		return decodeAPIError(httpResponse.StatusCode, payload)
	}
	if httpResponse.StatusCode != expectedStatus {
		return fmt.Errorf("%w: got %d, want %d", ErrUnexpectedStatus, httpResponse.StatusCode, expectedStatus)
	}
	if response == nil {
		return nil
	}
	if err := json.Unmarshal(payload, response); err != nil {
		return fmt.Errorf("netomatic: decode %s %s response: %w", method, route, err)
	}
	return nil
}

func decodeAPIError(statusCode int, payload []byte) error {
	apiError := &APIError{StatusCode: statusCode}
	if len(payload) != 0 {
		if err := json.Unmarshal(payload, apiError); err != nil {
			apiError.Detail = string(payload)
		}
	}
	apiError.StatusCode = statusCode
	if apiError.Detail == "" {
		apiError.Detail = http.StatusText(statusCode)
	}
	return apiError
}

func (c *HTTPClient) Process(ctx context.Context) (ProcessResponse, error) {
	var response ProcessResponse
	err := c.do(ctx, MethodGet, routeProcess, true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ListProjects(ctx context.Context) (ListProjectsResponse, error) {
	var response ListProjectsResponse
	err := c.do(ctx, MethodGet, APIPrefix+"/projects", true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) CreateProject(ctx context.Context, request CreateProjectRequest) (CreateProjectResponse, error) {
	var response CreateProjectResponse
	err := c.do(ctx, MethodPost, APIPrefix+"/projects", true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) OpenProject(ctx context.Context, request OpenProjectRequest) (OpenProjectResponse, error) {
	var response OpenProjectResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/open"
	err := c.do(ctx, MethodPost, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ForgetProject(ctx context.Context, request ForgetProjectRequest) (ForgetProjectResponse, error) {
	var response ForgetProjectResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project)
	err := c.do(ctx, MethodDelete, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ProjectSummaries(ctx context.Context, request ProjectSummariesRequest) (ProjectSummariesResponse, error) {
	var response ProjectSummariesResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/summaries"
	err := c.do(ctx, MethodGet, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) GetSetup(ctx context.Context, request GetSetupRequest) (GetSetupResponse, error) {
	var response GetSetupResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/setup"
	err := c.do(ctx, MethodGet, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) SaveSetup(ctx context.Context, request SaveSetupRequest) (SaveSetupResponse, error) {
	var response SaveSetupResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/setup"
	err := c.do(ctx, MethodPut, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ListEpics(ctx context.Context, path ProjectPath) (ListEpicsResponse, error) {
	var response ListEpicsResponse
	route := projectRoute(path.ProjectID, "/epics")
	err := c.do(ctx, MethodGet, route, true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) GetEpic(ctx context.Context, path EpicPath) (Epic, error) {
	var response Epic
	route := epicRoute(path, "")
	err := c.do(ctx, MethodGet, route, true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) CreateEpic(ctx context.Context, path ProjectPath, request CreateEpicRequest) error {
	route := projectRoute(path.ProjectID, "/epics")
	return c.do(ctx, MethodPost, route, true, nil, request, nil, http.StatusNoContent)
}

func (c *HTTPClient) CloseEpic(ctx context.Context, path EpicPath) error {
	return c.do(ctx, MethodDelete, epicRoute(path, ""), true, nil, nil, nil, http.StatusNoContent)
}

func (c *HTTPClient) TransitionEpicState(ctx context.Context, path EpicPath, request TransitionEpicStateRequest) error {
	return c.do(ctx, MethodPost, epicRoute(path, "/state-transitions"), true, nil, request, nil, http.StatusNoContent)
}

func (c *HTTPClient) SetBranchPrefix(ctx context.Context, path EpicPath, request SetBranchPrefixRequest) error {
	return c.do(ctx, MethodPut, epicRoute(path, "/branch-prefix"), true, nil, request, nil, http.StatusNoContent)
}

func (c *HTTPClient) CompleteEpic(ctx context.Context, path EpicPath) (CompleteEpicResponse, error) {
	var response CompleteEpicResponse
	err := c.do(ctx, MethodPost, epicRoute(path, "/complete"), true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ReviewApprovedBranches(ctx context.Context, path EpicPath) error {
	return c.do(ctx, MethodPost, epicRoute(path, "/review-approved-branches"), true, nil, nil, nil, http.StatusNoContent)
}

func (c *HTTPClient) RunEpicAgent(ctx context.Context, path EpicPath) error {
	return c.do(ctx, MethodPost, epicRoute(path, "/agent-runs"), true, nil, nil, nil, http.StatusOK)
}

func (c *HTTPClient) ListIssues(ctx context.Context, request ListIssuesRequest) (ListIssuesResponse, error) {
	var response ListIssuesResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/epics/" + escapePathSegment(request.Epic) + "/issues"
	err := c.do(ctx, MethodGet, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) GetIssue(ctx context.Context, request GetIssueRequest) (GetIssueResponse, error) {
	var response GetIssueResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/epics/" + escapePathSegment(request.Epic) + "/issues/" + escapePathSegment(request.Issue)
	err := c.do(ctx, MethodGet, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) CreateIssue(ctx context.Context, request CreateIssueRequest) (CreateIssueResponse, error) {
	var response CreateIssueResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/epics/" + escapePathSegment(request.Epic) + "/issues"
	err := c.do(ctx, MethodPost, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) UpdateIssue(ctx context.Context, request UpdateIssueRequest) (UpdateIssueResponse, error) {
	var response UpdateIssueResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/epics/" + escapePathSegment(request.Epic) + "/issues/" + escapePathSegment(request.Issue)
	err := c.do(ctx, MethodPut, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) TransitionIssue(ctx context.Context, request TransitionIssueRequest) (TransitionIssueResponse, error) {
	var response TransitionIssueResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/epics/" + escapePathSegment(request.Epic) + "/issues/" + escapePathSegment(request.Issue) + "/transition"
	err := c.do(ctx, MethodPost, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) CloseIssue(ctx context.Context, request CloseIssueRequest) (CloseIssueResponse, error) {
	var response CloseIssueResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/epics/" + escapePathSegment(request.Epic) + "/issues/" + escapePathSegment(request.Issue) + "/close"
	err := c.do(ctx, MethodPost, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ListPullRequests(ctx context.Context, request ListPullRequestsRequest) (ListPullRequestsResponse, error) {
	var response ListPullRequestsResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/epics/" + escapePathSegment(request.Epic) + "/issues/" + escapePathSegment(request.Issue) + "/pull-requests"
	err := c.do(ctx, MethodGet, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) CreatePullRequest(ctx context.Context, request CreatePullRequestRequest) (CreatePullRequestResponse, error) {
	var response CreatePullRequestResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/epics/" + escapePathSegment(request.Epic) + "/issues/" + escapePathSegment(request.Issue) + "/pull-requests"
	err := c.do(ctx, MethodPost, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) CommentPullRequest(ctx context.Context, request CommentPullRequestRequest) (CommentPullRequestResponse, error) {
	var response CommentPullRequestResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/pull-requests/" + escapePathSegment(request.PullRequest) + "/comments"
	err := c.do(ctx, MethodPost, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) MergePullRequest(ctx context.Context, request MergePullRequestRequest) (MergePullRequestResponse, error) {
	var response MergePullRequestResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/pull-requests/" + escapePathSegment(request.PullRequest) + "/merge"
	err := c.do(ctx, MethodPost, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ClosePullRequest(ctx context.Context, request ClosePullRequestRequest) (ClosePullRequestResponse, error) {
	var response ClosePullRequestResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/pull-requests/" + escapePathSegment(request.PullRequest) + "/close"
	err := c.do(ctx, MethodPost, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ResetPullRequest(ctx context.Context, request ResetPullRequestRequest) (ResetPullRequestResponse, error) {
	var response ResetPullRequestResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/pull-requests/" + escapePathSegment(request.PullRequest) + "/reset"
	err := c.do(ctx, MethodPost, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) GrantPullRequest(ctx context.Context, request GrantPullRequestRequest) (GrantPullRequestResponse, error) {
	var response GrantPullRequestResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/pull-requests/" + escapePathSegment(request.PullRequest) + "/grant"
	err := c.do(ctx, MethodPost, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) PullRequestDiff(ctx context.Context, request PullRequestDiffRequest) (PullRequestDiffResponse, error) {
	var response PullRequestDiffResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/pull-requests/" + escapePathSegment(request.PullRequest) + "/diff"
	err := c.do(ctx, MethodGet, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ListRepositories(ctx context.Context, request ListRepositoriesRequest) (ListRepositoriesResponse, error) {
	var response ListRepositoriesResponse
	err := c.do(ctx, MethodGet, APIPrefix+"/repositories", true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) GetRepository(ctx context.Context, request GetRepositoryRequest) (GetRepositoryResponse, error) {
	var response GetRepositoryResponse
	route := APIPrefix + "/repositories/" + escapePathSegment(request.Repository)
	err := c.do(ctx, MethodGet, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ListOrganisations(ctx context.Context, request ListOrganisationsRequest) (ListOrganisationsResponse, error) {
	var response ListOrganisationsResponse
	err := c.do(ctx, MethodGet, APIPrefix+"/organisations", true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) GetOrganisation(ctx context.Context, request GetOrganisationRequest) (GetOrganisationResponse, error) {
	var response GetOrganisationResponse
	route := APIPrefix + "/organisations/" + escapePathSegment(request.Organisation)
	err := c.do(ctx, MethodGet, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) GetAgentSettings(ctx context.Context, path GetAgentSettingsPath) (AgentSettings, error) {
	var response AgentSettings
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/agent-settings"
	err := c.do(ctx, MethodGet, route, true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) SetAgentRole(ctx context.Context, path SetAgentRolePath, request SetAgentRoleRequest) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/agent-settings/roles/" + escapePathSegment(path.Role)
	return c.do(ctx, MethodPut, route, true, nil, request, nil, http.StatusNoContent)
}

func (c *HTTPClient) ListAgentRuns(ctx context.Context, path ListAgentRunsPath) (ListAgentRunsResponse, error) {
	var response ListAgentRunsResponse
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/agent-runs"
	err := c.do(ctx, MethodGet, route, true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) GetAgentRun(ctx context.Context, path GetAgentRunPath) (AgentRun, error) {
	var response AgentRun
	route := APIPrefix + "/agent-runs/" + escapePathSegment(path.RunID)
	err := c.do(ctx, MethodGet, route, true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) RunOutput(ctx context.Context, path RunOutputPath, query RunOutputQuery) (RunOutputPage, error) {
	var response RunOutputPage
	route := APIPrefix + "/agent-runs/" + escapePathSegment(path.RunID) + "/output"
	err := c.do(ctx, MethodGet, route, true, query, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) AgentActivity(ctx context.Context, query AgentActivityQuery) (AgentActivityResponse, error) {
	var response AgentActivityResponse
	err := c.do(ctx, MethodGet, APIPrefix+"/agent-runs/activity", true, query, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) CancelAgentRun(ctx context.Context, path CancelAgentRunPath) (CancelAgentRunResponse, error) {
	var response CancelAgentRunResponse
	route := APIPrefix + "/agent-runs/" + escapePathSegment(path.RunID) + "/cancel"
	err := c.do(ctx, MethodPost, route, true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ListSandboxes(ctx context.Context, path ListSandboxesPath) (ListSandboxesResponse, error) {
	var response ListSandboxesResponse
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/sandboxes"
	err := c.do(ctx, MethodGet, route, true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) Capabilities(ctx context.Context) (CapabilitiesResponse, error) {
	var response CapabilitiesResponse
	err := c.do(ctx, MethodGet, APIPrefix+"/capabilities", true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) AddRepository(ctx context.Context, request AddRepositoryRequest) (AddRepositoryResponse, error) {
	var response AddRepositoryResponse
	err := c.do(ctx, MethodPost, APIPrefix+"/repositories", true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) RunIssue(ctx context.Context, request RunIssueRequest) (RunIssueResponse, error) {
	var response RunIssueResponse
	err := c.do(ctx, MethodPost, APIPrefix+"/runs/issue", true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) OpenPullRequests(ctx context.Context, request OpenPullRequestsRequest) (OpenPullRequestsResponse, error) {
	var response OpenPullRequestsResponse
	err := c.do(ctx, MethodGet, APIPrefix+"/open-pull-requests", true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) TransitionPullRequest(ctx context.Context, request TransitionPullRequestRequest) (TransitionPullRequestResponse, error) {
	var response TransitionPullRequestResponse
	route := APIPrefix + "/pull-requests/" + escapePathSegment(request.PullRequest) + "/transition"
	err := c.do(ctx, MethodPost, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) Reconcile(ctx context.Context, request ReconcileRequest) (ReconcileResponse, error) {
	var response ReconcileResponse
	err := c.do(ctx, MethodPost, APIPrefix+"/reconcile", true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) Purge(ctx context.Context, request PurgeRequest) (PurgeResponse, error) {
	var response PurgeResponse
	err := c.do(ctx, MethodPost, APIPrefix+"/purge", true, nil, request, &response, http.StatusOK)
	return response, err
}

var _ Client = (*HTTPClient)(nil)
