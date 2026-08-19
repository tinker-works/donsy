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
| ListIssues | GET | `/api/v1/projects/{project}/epics/{epic}/issues` | `ListIssuesRequest` | `ListIssuesResponse` |
| GetIssue | GET | `/api/v1/projects/{project}/epics/{epic}/issues/{issue}` | `GetIssueRequest` | `GetIssueResponse` |
| CreateIssue | POST | `/api/v1/projects/{project}/epics/{epic}/issues` | `CreateIssueRequest` | `CreateIssueResponse` |
| UpdateIssue | PUT | `/api/v1/projects/{project}/epics/{epic}/issues/{issue}` | `UpdateIssueRequest` | `UpdateIssueResponse` |
| TransitionIssue | POST | `/api/v1/projects/{project}/epics/{epic}/issues/{issue}/transition` | `TransitionIssueRequest` | `TransitionIssueResponse` |
| CloseIssue | POST | `/api/v1/projects/{project}/epics/{epic}/issues/{issue}/close` | `CloseIssueRequest` | `CloseIssueResponse` |
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
| RunIssue | POST | `/api/v1/runs/issue` | `RunIssueRequest` | `RunIssueResponse` |
| OpenPullRequests | GET | `/api/v1/open-pull-requests` | `OpenPullRequestsRequest` | `OpenPullRequestsResponse` |
| TransitionPullRequest | POST | `/api/v1/pull-requests/{pull_request}/transition` | `TransitionPullRequestRequest` | `TransitionPullRequestResponse` |
| Reconcile | POST | `/api/v1/reconcile` | `ReconcileRequest` | `ReconcileResponse` |
| Purge | POST | `/api/v1/purge` | `PurgeRequest` | `PurgeResponse` |

`RunOutput` accepts an optional non-negative `from` query value. `AgentActivity`
uses repeated `runID` query values and returns the `sizes` map directly. Both
operations send an empty query when no values are supplied.
