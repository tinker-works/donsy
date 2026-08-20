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

func (c *HTTPClient) ListEpics(ctx context.Context, request ListEpicsRequest) (ListEpicsResponse, error) {
	var response ListEpicsResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/epics"
	err := c.do(ctx, MethodGet, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) GetEpic(ctx context.Context, request GetEpicRequest) (GetEpicResponse, error) {
	var response GetEpicResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/epics/" + escapePathSegment(request.Epic)
	err := c.do(ctx, MethodGet, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) CreateEpic(ctx context.Context, request CreateEpicRequest) (CreateEpicResponse, error) {
	var response CreateEpicResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/epics"
	err := c.do(ctx, MethodPost, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) PrefixEpic(ctx context.Context, request PrefixEpicRequest) (PrefixEpicResponse, error) {
	var response PrefixEpicResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/epics/" + escapePathSegment(request.Epic) + "/prefix"
	err := c.do(ctx, MethodPost, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) TransitionEpic(ctx context.Context, request TransitionEpicRequest) (TransitionEpicResponse, error) {
	var response TransitionEpicResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/epics/" + escapePathSegment(request.Epic) + "/transition"
	err := c.do(ctx, MethodPost, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) CloseEpic(ctx context.Context, request CloseEpicRequest) (CloseEpicResponse, error) {
	var response CloseEpicResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/epics/" + escapePathSegment(request.Epic) + "/close"
	err := c.do(ctx, MethodPost, route, true, nil, request, &response, http.StatusOK)
	return response, err
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

func (c *HTTPClient) CreatePullRequest(ctx context.Context, path CreatePullRequestPath, request CreatePullRequestRequest) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/pull-requests"
	return c.do(ctx, MethodPost, route, true, nil, request, nil, http.StatusNoContent)
}

func (c *HTTPClient) TransitionPullRequest(ctx context.Context, path TransitionPullRequestPath, request TransitionPullRequestRequest) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/pull-requests/" + escapePathSegment(path.PullRequestID) + "/state-transitions"
	return c.do(ctx, MethodPost, route, true, nil, request, nil, http.StatusNoContent)
}

func (c *HTTPClient) GrantCodingRound(ctx context.Context, path GrantCodingRoundPath) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/pull-requests/" + escapePathSegment(path.PullRequestID) + "/coding-rounds"
	return c.do(ctx, MethodPost, route, true, nil, nil, nil, http.StatusNoContent)
}

func (c *HTTPClient) MergePullRequest(ctx context.Context, path MergePullRequestPath) (MergePullRequestResponse, error) {
	var response MergePullRequestResponse
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/pull-requests/" + escapePathSegment(path.PullRequestID) + "/merge"
	err := c.do(ctx, MethodPost, route, true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ResetIssue(ctx context.Context, path ResetIssuePath) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/pull-requests/" + escapePathSegment(path.PullRequestID) + "/reset"
	return c.do(ctx, MethodPost, route, true, nil, nil, nil, http.StatusNoContent)
}

func (c *HTTPClient) GetPullRequestDiff(ctx context.Context, path GetPullRequestDiffPath) (PullRequestDiffResponse, error) {
	var response PullRequestDiffResponse
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/pull-requests/" + escapePathSegment(path.PullRequestID) + "/diff"
	err := c.do(ctx, MethodGet, route, true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) OpenPullRequests(ctx context.Context, path OpenPullRequestsPath) (OpenPullRequestsResponse, error) {
	var response OpenPullRequestsResponse
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/open-pull-requests"
	err := c.do(ctx, MethodPost, route, true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) AddComment(ctx context.Context, path AddCommentPath, request AddCommentRequest) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/comments"
	return c.do(ctx, MethodPost, route, true, nil, request, nil, http.StatusNoContent)
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

func (c *HTTPClient) GetAgentSettings(ctx context.Context, request GetAgentSettingsRequest) (GetAgentSettingsResponse, error) {
	var response GetAgentSettingsResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/agent-settings"
	err := c.do(ctx, MethodGet, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ListAgentRuns(ctx context.Context, request ListAgentRunsRequest) (ListAgentRunsResponse, error) {
	var response ListAgentRunsResponse
	route := APIPrefix + "/projects/" + escapePathSegment(request.Project) + "/agent-runs"
	err := c.do(ctx, MethodGet, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ListSandboxes(ctx context.Context, request ListSandboxesRequest) (ListSandboxesResponse, error) {
	var response ListSandboxesResponse
	err := c.do(ctx, MethodGet, APIPrefix+"/sandboxes", true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) CancelAgentRun(ctx context.Context, request CancelAgentRunRequest) (CancelAgentRunResponse, error) {
	var response CancelAgentRunResponse
	route := APIPrefix + "/agent-runs/" + escapePathSegment(request.Run) + "/cancel"
	err := c.do(ctx, MethodPost, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) AgentActivity(ctx context.Context, request AgentActivityRequest) (AgentActivityResponse, error) {
	var response AgentActivityResponse
	route := APIPrefix + "/agent-runs/" + escapePathSegment(request.Run) + "/activity"
	err := c.do(ctx, MethodGet, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) RunOutput(ctx context.Context, request RunOutputRequest) (RunOutputResponse, error) {
	var response RunOutputResponse
	route := APIPrefix + "/agent-runs/" + escapePathSegment(request.Run) + "/output"
	err := c.do(ctx, MethodGet, route, true, nil, request, &response, http.StatusOK)
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

func (c *HTTPClient) GetAgentRun(ctx context.Context, request GetAgentRunRequest) (GetAgentRunResponse, error) {
	var response GetAgentRunResponse
	route := APIPrefix + "/agent-runs/" + escapePathSegment(request.Run)
	err := c.do(ctx, MethodGet, route, true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) Complete(ctx context.Context, request CompleteRequest) (CompleteResponse, error) {
	var response CompleteResponse
	err := c.do(ctx, MethodPost, APIPrefix+"/complete", true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ReviewApprovedBranches(ctx context.Context, request ReviewApprovedBranchesRequest) (ReviewApprovedBranchesResponse, error) {
	var response ReviewApprovedBranchesResponse
	err := c.do(ctx, MethodPost, APIPrefix+"/review-approved-branches", true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) RunEpic(ctx context.Context, request RunEpicRequest) (RunEpicResponse, error) {
	var response RunEpicResponse
	err := c.do(ctx, MethodPost, APIPrefix+"/runs/epic", true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) RunIssue(ctx context.Context, request RunIssueRequest) (RunIssueResponse, error) {
	var response RunIssueResponse
	err := c.do(ctx, MethodPost, APIPrefix+"/runs/issue", true, nil, request, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) RunIssueAgent(ctx context.Context, path RunIssueAgentPath) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/issues/" + escapePathSegment(path.IssueID) + "/agent-runs"
	return c.do(ctx, MethodPost, route, true, nil, nil, nil, http.StatusNoContent)
}

func (c *HTTPClient) ReconcileSandboxes(ctx context.Context, path ProjectPath) error {
	route := APIPrefix + "/projects/" + escapePathSegment(strconv.FormatUint(uint64(path.ProjectID), 10)) + "/maintenance/reconcile"
	return c.do(ctx, MethodPost, route, true, nil, nil, nil, 0)
}

func (c *HTTPClient) PurgeFinishedWork(ctx context.Context, path ProjectPath) error {
	route := APIPrefix + "/projects/" + escapePathSegment(strconv.FormatUint(uint64(path.ProjectID), 10)) + "/maintenance/purge"
	return c.do(ctx, MethodPost, route, true, nil, nil, nil, 0)
}

func (c *HTTPClient) ReadDaemonLog(ctx context.Context, offset int64, limit int) (ReadDaemonLogResponse, error) {
	var response ReadDaemonLogResponse
	request, err := BoundDaemonLogRequest(ReadDaemonLogRequest{Offset: offset, Limit: limit})
	if err != nil {
		return response, err
	}
	query := url.Values{
		"offset": {strconv.FormatInt(request.Offset, 10)},
		"limit":  {strconv.Itoa(request.Limit)},
	}
	err = c.do(ctx, MethodGet, APIPrefix+"/daemon-log", true, query, nil, &response, http.StatusOK)
	return response, err
}

var _ Client = (*HTTPClient)(nil)
