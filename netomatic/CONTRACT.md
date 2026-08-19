# Netomatic Contract

The daemon speaks protocol `v1` below `/api/v1`. Protocol compatibility is
exact: a client must reject any protocol other than `v1`. Every operation
requires the daemon bearer token. The separate unauthenticated `/healthz`
readiness endpoint is outside this client contract.

The request column names the JSON body DTO, if any. Path and query arguments are
supplied separately by the client method so they cannot leak into strict JSON
bodies. An em dash
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
| Reconcile | POST | `/api/v1/reconcile` | `ReconcileRequest` | `ReconcileResponse` |
| Purge | POST | `/api/v1/purge` | `PurgeRequest` | `PurgeResponse` |

`RunOutput` accepts an optional non-negative `from` query value. `AgentActivity`
uses repeated `runID` query values and returns the `sizes` map directly. Both
operations send an empty query when no values are supplied.
