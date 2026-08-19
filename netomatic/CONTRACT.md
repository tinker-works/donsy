# Netomatic Contract

The daemon speaks protocol `v1` below `/api/v1`. Protocol compatibility is
exact: a client must reject any protocol other than `v1`. Every operation
requires the daemon bearer token. The separate unauthenticated `/healthz`
readiness endpoint is outside this client contract.

The request column names the JSON body DTO. Path parameters are supplied
separately by the client method so they cannot leak into strict JSON bodies. An
em dash means that the operation has no request body.

| Operation | Method | Route | Request DTO | Response DTO |
| --- | --- | --- | --- | --- |
| Process | GET | `/api/v1/process` | — | `ProcessResponse` |
| ListProjects | GET | `/api/v1/projects` | — | `ListProjectsResponse` |
| CreateProject | POST | `/api/v1/projects` | `CreateProjectRequest` | `CreateProjectResponse` |
| OpenProject | POST | `/api/v1/projects/{project}/open` | `OpenProjectRequest` | `OpenProjectResponse` |
| ForgetProject | DELETE | `/api/v1/projects/{project}` | `ForgetProjectRequest` | `ForgetProjectResponse` |
| ProjectSummaries | GET | `/api/v1/projects/{project}/summaries` | `ProjectSummariesRequest` | `ProjectSummariesResponse` |
| GetSetup | GET | `/api/v1/projects/{project}/setup` | `GetSetupRequest` | `GetSetupResponse` |
| SaveSetup | PUT | `/api/v1/projects/{project}/setup` | `SaveSetupRequest` | `SaveSetupResponse` |
| ListEpics | GET | `/api/v1/projects/{project}/epics` | `ListEpicsRequest` | `ListEpicsResponse` |
| GetEpic | GET | `/api/v1/projects/{project}/epics/{epic}` | `GetEpicRequest` | `GetEpicResponse` |
| CreateEpic | POST | `/api/v1/projects/{project}/epics` | `CreateEpicRequest` | `CreateEpicResponse` |
| PrefixEpic | POST | `/api/v1/projects/{project}/epics/{epic}/prefix` | `PrefixEpicRequest` | `PrefixEpicResponse` |
| TransitionEpic | POST | `/api/v1/projects/{project}/epics/{epic}/transition` | `TransitionEpicRequest` | `TransitionEpicResponse` |
| CloseEpic | POST | `/api/v1/projects/{project}/epics/{epic}/close` | `CloseEpicRequest` | `CloseEpicResponse` |
| ListIssues | GET | `/api/v1/projects/{project}/epics/{epic}/issues` | `ListIssuesRequest` | `ListIssuesResponse` |
| GetIssue | GET | `/api/v1/projects/{project}/epics/{epic}/issues/{issue}` | `GetIssueRequest` | `GetIssueResponse` |
| CreateIssue | POST | `/api/v1/projects/{project}/epics/{epic}/issues` | `CreateIssueRequest` | `CreateIssueResponse` |
| UpdateIssue | PUT | `/api/v1/projects/{project}/epics/{epic}/issues/{issue}` | `UpdateIssueRequest` | `UpdateIssueResponse` |
| TransitionIssue | POST | `/api/v1/projects/{project}/epics/{epic}/issues/{issue}/transition` | `TransitionIssueRequest` | `TransitionIssueResponse` |
| CloseIssue | POST | `/api/v1/projects/{project}/epics/{epic}/issues/{issue}/close` | `CloseIssueRequest` | `CloseIssueResponse` |
| CreatePullRequest | POST | `/api/v1/projects/{projectID}/epics/{epicID}/pull-requests` | `CreatePullRequestRequest` | — (204) |
| TransitionPullRequest | POST | `/api/v1/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/state-transitions` | `TransitionPullRequestRequest` | — (204) |
| GrantCodingRound | POST | `/api/v1/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/coding-rounds` | — | — (204) |
| MergePullRequest | POST | `/api/v1/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/merge` | — | `MergePullRequestResponse` |
| ResetIssue | POST | `/api/v1/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/reset` | — | — (204) |
| GetPullRequestDiff | GET | `/api/v1/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/diff` | — | `PullRequestDiffResponse` |
| OpenPullRequests | POST | `/api/v1/projects/{projectID}/epics/{epicID}/open-pull-requests` | — | `OpenPullRequestsResponse` |
| AddComment | POST | `/api/v1/projects/{projectID}/epics/{epicID}/comments` | `AddCommentRequest` | — (204) |
| ListRepositories | GET | `/api/v1/repositories` | `ListRepositoriesRequest` | `ListRepositoriesResponse` |
| GetRepository | GET | `/api/v1/repositories/{repository}` | `GetRepositoryRequest` | `GetRepositoryResponse` |
| ListOrganisations | GET | `/api/v1/organisations` | `ListOrganisationsRequest` | `ListOrganisationsResponse` |
| GetOrganisation | GET | `/api/v1/organisations/{organisation}` | `GetOrganisationRequest` | `GetOrganisationResponse` |
| GetAgentSettings | GET | `/api/v1/projects/{project}/agent-settings` | `GetAgentSettingsRequest` | `GetAgentSettingsResponse` |
| ListAgentRuns | GET | `/api/v1/projects/{project}/agent-runs` | `ListAgentRunsRequest` | `ListAgentRunsResponse` |
| ListSandboxes | GET | `/api/v1/sandboxes` | `ListSandboxesRequest` | `ListSandboxesResponse` |
| CancelAgentRun | POST | `/api/v1/agent-runs/{run}/cancel` | `CancelAgentRunRequest` | `CancelAgentRunResponse` |
| AgentActivity | GET | `/api/v1/agent-runs/{run}/activity` | `AgentActivityRequest` | `AgentActivityResponse` |
| RunOutput | GET | `/api/v1/agent-runs/{run}/output` | `RunOutputRequest` | `RunOutputResponse` |
| Capabilities | GET | `/api/v1/capabilities` | — | `CapabilitiesResponse` |
| AddRepository | POST | `/api/v1/repositories` | `AddRepositoryRequest` | `AddRepositoryResponse` |
| GetAgentRun | GET | `/api/v1/agent-runs/{run}` | `GetAgentRunRequest` | `GetAgentRunResponse` |
| Complete | POST | `/api/v1/complete` | `CompleteRequest` | `CompleteResponse` |
| ReviewApprovedBranches | POST | `/api/v1/review-approved-branches` | `ReviewApprovedBranchesRequest` | `ReviewApprovedBranchesResponse` |
| RunEpic | POST | `/api/v1/runs/epic` | `RunEpicRequest` | `RunEpicResponse` |
| RunIssue | POST | `/api/v1/runs/issue` | `RunIssueRequest` | `RunIssueResponse` |
| Reconcile | POST | `/api/v1/reconcile` | `ReconcileRequest` | `ReconcileResponse` |
| Purge | POST | `/api/v1/purge` | `PurgeRequest` | `PurgeResponse` |
| ReadDaemonLog | GET | `/api/v1/daemon-log` | `ReadDaemonLogRequest` | `ReadDaemonLogResponse` |

## Daemon Log

`ReadDaemonLog` is authenticated and uses a byte offset into the daemon's
newline-delimited log. The request `limit` must be positive. The daemon clamps
it to `MaxDaemonLogLines` (1000) and also caps a page at `MaxDaemonLogBytes`
(64 KiB). Pages contain complete lines only. If one line is larger than the
byte cap, that complete line is skipped and the page offset advances past its
newline. It is never split or returned, and subsequent lines can fill the same
page.

`next_offset` is always the byte offset immediately after the last newline
consumed while building the page, including skipped oversized records; returned
`lines` do not include that newline. A client should pass it unchanged to the
next request. If the log was truncated or rotated and the requested offset is
past the current file size, the daemon starts at byte zero and sets
`offset_reset` to `true`. When no record is consumed, `next_offset` remains the
effective starting offset, so polling at EOF is stable.
