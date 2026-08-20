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

func (c *HTTPClient) Capabilities(ctx context.Context) (CapabilitiesResponse, error) {
	var response CapabilitiesResponse
	err := c.do(ctx, MethodGet, APIPrefix+"/capabilities", true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ListProjects(ctx context.Context) (ListProjectsResponse, error) {
	var response ListProjectsResponse
	err := c.do(ctx, MethodGet, APIPrefix+"/projects", true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) CreateProject(ctx context.Context, request CreateProjectRequest) (CreateProjectResponse, error) {
	var response CreateProjectResponse
	err := c.do(ctx, MethodPost, APIPrefix+"/projects", true, nil, request, &response, http.StatusCreated)
	return response, err
}

func (c *HTTPClient) ListProjectSummaries(ctx context.Context) (ListProjectSummariesResponse, error) {
	var response ListProjectSummariesResponse
	err := c.do(ctx, MethodGet, APIPrefix+"/projects/summaries", true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) OpenProject(ctx context.Context, path ProjectPath) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/open"
	return c.do(ctx, MethodPost, route, true, nil, nil, nil, http.StatusNoContent)
}

func (c *HTTPClient) ForgetProject(ctx context.Context, path ProjectPath) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10)
	return c.do(ctx, MethodDelete, route, true, nil, nil, nil, http.StatusNoContent)
}

func (c *HTTPClient) StoreSetup(ctx context.Context, path ProjectPath) (SetupState, error) {
	var response SetupState
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/setup"
	err := c.do(ctx, MethodGet, route, true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) InitialiseStore(ctx context.Context, path ProjectPath, request InitialiseStoreRequest) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/setup"
	return c.do(ctx, MethodPost, route, true, nil, request, nil, http.StatusNoContent)
}

func (c *HTTPClient) ListEpics(ctx context.Context, path ProjectPath) (ListEpicsResponse, error) {
	var response ListEpicsResponse
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics"
	err := c.do(ctx, MethodGet, route, true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) GetEpic(ctx context.Context, path EpicPath) (Epic, error) {
	var response Epic
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID)
	err := c.do(ctx, MethodGet, route, true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) CreateEpic(ctx context.Context, path EpicPath, request CreateEpicRequest) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics"
	return c.do(ctx, MethodPost, route, true, nil, request, nil, http.StatusNoContent)
}

func (c *HTTPClient) CloseEpic(ctx context.Context, path EpicPath) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID)
	return c.do(ctx, MethodDelete, route, true, nil, nil, nil, http.StatusNoContent)
}

func (c *HTTPClient) TransitionEpicState(ctx context.Context, path EpicPath, request TransitionEpicStateRequest) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/state-transitions"
	return c.do(ctx, MethodPost, route, true, nil, request, nil, http.StatusNoContent)
}

func (c *HTTPClient) SetBranchPrefix(ctx context.Context, path EpicPath, request SetBranchPrefixRequest) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/branch-prefix"
	return c.do(ctx, MethodPut, route, true, nil, request, nil, http.StatusNoContent)
}

func (c *HTTPClient) CompleteEpic(ctx context.Context, path EpicPath) (CompleteEpicResponse, error) {
	var response CompleteEpicResponse
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/complete"
	err := c.do(ctx, MethodPost, route, true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ReviewApprovedBranches(ctx context.Context, path EpicPath) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/review-approved-branches"
	return c.do(ctx, MethodPost, route, true, nil, nil, nil, http.StatusNoContent)
}

func (c *HTTPClient) RunEpicAgent(ctx context.Context, path EpicPath) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/agent-runs"
	return c.do(ctx, MethodPost, route, true, nil, nil, nil, http.StatusNoContent)
}

func (c *HTTPClient) CreateIssue(ctx context.Context, path EpicPath, request CreateIssueRequest) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/issues"
	return c.do(ctx, MethodPost, route, true, nil, request, nil, http.StatusNoContent)
}

func (c *HTTPClient) CloseIssue(ctx context.Context, path IssuePath) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/issues/" + escapePathSegment(path.IssueID)
	return c.do(ctx, MethodDelete, route, true, nil, nil, nil, http.StatusNoContent)
}

func (c *HTTPClient) RunIssueAgent(ctx context.Context, path IssuePath) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/epics/" + escapePathSegment(path.EpicID) + "/issues/" + escapePathSegment(path.IssueID) + "/agent-runs"
	return c.do(ctx, MethodPost, route, true, nil, nil, nil, http.StatusNoContent)
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

func (c *HTTPClient) ListOrganisations(ctx context.Context) (ListOrganisationsResponse, error) {
	var response ListOrganisationsResponse
	err := c.do(ctx, MethodGet, APIPrefix+"/organisations", true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) AddOrganisation(ctx context.Context, request AddOrganisationRequest) error {
	return c.do(ctx, MethodPost, APIPrefix+"/organisations", true, nil, request, nil, http.StatusNoContent)
}

func (c *HTTPClient) RemoveOrganisation(ctx context.Context, path RemoveOrganisationPath) error {
	route := APIPrefix + "/organisations/" + escapePathSegment(path.Name)
	return c.do(ctx, MethodDelete, route, true, nil, nil, nil, http.StatusNoContent)
}

func (c *HTTPClient) DiscoverOrganisations(ctx context.Context) (DiscoverOrganisationsResponse, error) {
	var response DiscoverOrganisationsResponse
	err := c.do(ctx, MethodPost, APIPrefix+"/organisations/discovery", true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) ListRepositories(ctx context.Context) (ListRepositoriesResponse, error) {
	var response ListRepositoriesResponse
	err := c.do(ctx, MethodGet, APIPrefix+"/repositories", true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) AddRepository(ctx context.Context, request AddRepositoryRequest) (AddRepositoryResponse, error) {
	var response AddRepositoryResponse
	err := c.do(ctx, MethodPost, APIPrefix+"/repositories", true, nil, request, &response, http.StatusCreated)
	return response, err
}

func (c *HTTPClient) SyncRepositories(ctx context.Context) error {
	return c.do(ctx, MethodPost, APIPrefix+"/repositories/sync", true, nil, nil, nil, http.StatusNoContent)
}

func (c *HTTPClient) ListProjectRepositories(ctx context.Context, path ListProjectRepositoriesPath) (ListProjectRepositoriesResponse, error) {
	var response ListProjectRepositoriesResponse
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/repositories"
	err := c.do(ctx, MethodGet, route, true, nil, nil, &response, http.StatusOK)
	return response, err
}

func (c *HTTPClient) UpdateProjectRepositories(ctx context.Context, path UpdateProjectRepositoriesPath, request UpdateProjectRepositoriesRequest) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/repositories"
	return c.do(ctx, MethodPut, route, true, nil, request, nil, http.StatusNoContent)
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

func (c *HTTPClient) ReconcileSandboxes(ctx context.Context, path ProjectPath) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/maintenance/reconcile"
	return c.do(ctx, MethodPost, route, true, nil, nil, nil, http.StatusNoContent)
}

func (c *HTTPClient) PurgeFinishedWork(ctx context.Context, path ProjectPath) error {
	route := APIPrefix + "/projects/" + strconv.FormatUint(uint64(path.ProjectID), 10) + "/maintenance/purge"
	return c.do(ctx, MethodPost, route, true, nil, nil, nil, http.StatusNoContent)
}

var _ Client = (*HTTPClient)(nil)
