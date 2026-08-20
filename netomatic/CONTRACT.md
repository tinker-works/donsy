# Netomatic Contract

The daemon speaks protocol `v1` below `/api/v1`. Protocol compatibility is
exact: a client must reject any protocol other than `v1`. Every operation
requires the daemon bearer token. The separate unauthenticated `/healthz`
readiness endpoint is outside this client contract.

The request column names the JSON body DTO, if any. Path parameters are
supplied separately by the client method and are not sent as JSON. An em dash
means that the operation has no request body.

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
| ListEpics | GET | `/api/v1/projects/{projectID}/epics` | — | `ListEpicsResponse` |
| GetEpic | GET | `/api/v1/projects/{projectID}/epics/{epicID}` | — | `Epic` |
| CreateEpic | POST | `/api/v1/projects/{projectID}/epics` | `CreateEpicRequest` | — |
| CloseEpic | DELETE | `/api/v1/projects/{projectID}/epics/{epicID}` | — | — |
| TransitionEpicState | POST | `/api/v1/projects/{projectID}/epics/{epicID}/state-transitions` | `TransitionEpicStateRequest` | — |
| SetBranchPrefix | PUT | `/api/v1/projects/{projectID}/epics/{epicID}/branch-prefix` | `SetBranchPrefixRequest` | — |
| CompleteEpic | POST | `/api/v1/projects/{projectID}/epics/{epicID}/complete` | — | `CompleteEpicResponse` |
| ReviewApprovedBranches | POST | `/api/v1/projects/{projectID}/epics/{epicID}/review-approved-branches` | — | — |
| RunEpicAgent | POST | `/api/v1/projects/{projectID}/epics/{epicID}/agent-runs` | — | — |
| CreateIssue | POST | `/api/v1/projects/{projectID}/epics/{epicID}/issues` | `CreateIssueRequest` | — |
| CloseIssue | DELETE | `/api/v1/projects/{projectID}/epics/{epicID}/issues/{issueID}` | — | — |
| ListPullRequests | GET | `/api/v1/projects/{project}/epics/{epic}/issues/{issue}/pull-requests` | `ListPullRequestsRequest` | `ListPullRequestsResponse` |
| CreatePullRequest | POST | `/api/v1/projects/{project}/epics/{epic}/issues/{issue}/pull-requests` | `CreatePullRequestRequest` | `CreatePullRequestResponse` |
| CommentPullRequest | POST | `/api/v1/projects/{project}/pull-requests/{pull_request}/comments` | `CommentPullRequestRequest` | `CommentPullRequestResponse` |
| MergePullRequest | POST | `/api/v1/projects/{project}/pull-requests/{pull_request}/merge` | `MergePullRequestRequest` | `MergePullRequestResponse` |
| ClosePullRequest | POST | `/api/v1/projects/{project}/pull-requests/{pull_request}/close` | `ClosePullRequestRequest` | `ClosePullRequestResponse` |
| ResetPullRequest | POST | `/api/v1/projects/{project}/pull-requests/{pull_request}/reset` | `ResetPullRequestRequest` | `ResetPullRequestResponse` |
| GrantPullRequest | POST | `/api/v1/projects/{project}/pull-requests/{pull_request}/grant` | `GrantPullRequestRequest` | `GrantPullRequestResponse` |
| PullRequestDiff | GET | `/api/v1/projects/{project}/pull-requests/{pull_request}/diff` | `PullRequestDiffRequest` | `PullRequestDiffResponse` |
| ListRepositories | GET | `/api/v1/repositories` | `ListRepositoriesRequest` | `ListRepositoriesResponse` |
| GetRepository | GET | `/api/v1/repositories/{repository}` | `GetRepositoryRequest` | `GetRepositoryResponse` |
| ListOrganisations | GET | `/api/v1/organisations` | `ListOrganisationsRequest` | `ListOrganisationsResponse` |
| GetOrganisation | GET | `/api/v1/organisations/{organisation}` | `GetOrganisationRequest` | `GetOrganisationResponse` |
| GetAgentSettings | GET | `/api/v1/projects/{projectID}/agent-settings` | — | `AgentSettings` |
| SetAgentRole | PUT | `/api/v1/projects/{projectID}/agent-settings/roles/{role}` | `SetAgentRoleRequest` | — |
| ListAgentRuns | GET | `/api/v1/projects/{projectID}/agent-runs` | — | `ListAgentRunsResponse` |
| GetAgentRun | GET | `/api/v1/agent-runs/{runID}` | — | `AgentRun` |
| RunOutput | GET | `/api/v1/agent-runs/{runID}/output` | — | `RunOutputPage` |
| AgentActivity | GET | `/api/v1/agent-runs/activity` | — | `AgentActivityResponse` |
| CancelAgentRun | POST | `/api/v1/agent-runs/{runID}/cancel` | — | `CancelAgentRunResponse` |
| ListSandboxes | GET | `/api/v1/projects/{projectID}/sandboxes` | — | `ListSandboxesResponse` |
| Capabilities | GET | `/api/v1/capabilities` | — | `CapabilitiesResponse` |
| AddRepository | POST | `/api/v1/repositories` | `AddRepositoryRequest` | `AddRepositoryResponse` |
| RunIssueAgent | POST | `/api/v1/projects/{projectID}/epics/{epicID}/issues/{issueID}/agent-runs` | — | — |
| RunIssue | POST | `/api/v1/runs/issue` | `RunIssueRequest` | `RunIssueResponse` |
| OpenPullRequests | GET | `/api/v1/open-pull-requests` | `OpenPullRequestsRequest` | `OpenPullRequestsResponse` |
| TransitionPullRequest | POST | `/api/v1/pull-requests/{pull_request}/transition` | `TransitionPullRequestRequest` | `TransitionPullRequestResponse` |
| ReconcileSandboxes | POST | `/api/v1/projects/{projectID}/maintenance/reconcile` | — | — |
| PurgeFinishedWork | POST | `/api/v1/projects/{projectID}/maintenance/purge` | — | — |

`CreateIssueRequest` contains `parentId`, `title`, `body`, and `repository`.
`parentId` may be omitted to create the aggregate root, and `body` may be
empty. Issue creation and closure return `204 No Content`. Manual issue-agent
execution is registered but currently returns `501 Not Implemented` with the
`feature_not_configured` error code.

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
`RunOutput` accepts an optional non-negative `from` query value. `AgentActivity`
uses repeated `runID` query values and returns the `sizes` map directly. Both
operations send an empty query when no values are supplied.

`ReconcileSandboxes` and `PurgeFinishedWork` are registered but currently
unavailable until the worker coordinator is shared. Go Merge returns HTTP 501
with `code: "feature_not_configured"` and the operation-specific `detail`; the
client exposes this as `ErrUnavailable`.
