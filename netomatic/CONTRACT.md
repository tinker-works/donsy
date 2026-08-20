# Netomatic Contract

Netomatic mirrors the 48 `mux.HandleFunc` registrations in Go Merge's
`internal/httpapi/server.go` at revision
`a4cebdcc1e9182f113e033aeda0d52850c6fe9f4` (`mount fix`). The server
registration is authoritative; the OpenAPI document omits the activity route.

The daemon speaks protocol `v1` below `/api/v1`. Every API operation requires
the daemon bearer token. The unauthenticated `/healthz` readiness endpoint is
outside this contract. Path values are supplied separately from JSON bodies,
and GET operations never send a request body.

| Operation | Method | Route | Path | Query | Request | Response / status |
| --- | --- | --- | --- | --- | --- | --- |
| Process | GET | `/api/v1/process` | - | - | - | `ProcessResponse` (200) |
| Capabilities | GET | `/api/v1/capabilities` | - | - | - | `CapabilitiesResponse` (200) |
| ListProjects | GET | `/api/v1/projects` | - | - | - | `ListProjectsResponse` (200) |
| CreateProject | POST | `/api/v1/projects` | - | - | `CreateProjectRequest` | `CreateProjectResponse` (201) |
| ListProjectSummaries | GET | `/api/v1/projects/summaries` | - | - | - | `ListProjectSummariesResponse` (200) |
| OpenProject | POST | `/api/v1/projects/{projectID}/open` | `ProjectPath` | - | - | - (204) |
| ForgetProject | DELETE | `/api/v1/projects/{projectID}` | `ProjectPath` | - | - | - (204) |
| StoreSetup | GET | `/api/v1/projects/{projectID}/setup` | `ProjectPath` | - | - | `SetupState` (200) |
| InitialiseStore | POST | `/api/v1/projects/{projectID}/setup` | `ProjectPath` | - | `InitialiseStoreRequest` | - (204) |
| ListEpics | GET | `/api/v1/projects/{projectID}/epics` | `ProjectPath` | - | - | `ListEpicsResponse` (200) |
| GetEpic | GET | `/api/v1/projects/{projectID}/epics/{epicID}` | `EpicPath` | - | - | `Epic` (200) |
| CreateEpic | POST | `/api/v1/projects/{projectID}/epics` | `EpicPath` | - | `CreateEpicRequest` | - (204) |
| CloseEpic | DELETE | `/api/v1/projects/{projectID}/epics/{epicID}` | `EpicPath` | - | - | - (204) |
| TransitionEpicState | POST | `/api/v1/projects/{projectID}/epics/{epicID}/state-transitions` | `EpicPath` | - | `TransitionEpicStateRequest` | - (204) |
| SetBranchPrefix | PUT | `/api/v1/projects/{projectID}/epics/{epicID}/branch-prefix` | `EpicPath` | - | `SetBranchPrefixRequest` | - (204) |
| CompleteEpic | POST | `/api/v1/projects/{projectID}/epics/{epicID}/complete` | `EpicPath` | - | - | `CompleteEpicResponse` (200) |
| ReviewApprovedBranches | POST | `/api/v1/projects/{projectID}/epics/{epicID}/review-approved-branches` | `EpicPath` | - | - | - (204) |
| RunEpicAgent | POST | `/api/v1/projects/{projectID}/epics/{epicID}/agent-runs` | `EpicPath` | - | - | `feature_not_configured` (501) |
| CreateIssue | POST | `/api/v1/projects/{projectID}/epics/{epicID}/issues` | `CreateIssuePath` | - | `CreateIssueRequest` | - (204) |
| CloseIssue | DELETE | `/api/v1/projects/{projectID}/epics/{epicID}/issues/{issueID}` | `CloseIssuePath` | - | - | - (204) |
| RunIssueAgent | POST | `/api/v1/projects/{projectID}/epics/{epicID}/issues/{issueID}/agent-runs` | `RunIssueAgentPath` | - | - | `feature_not_configured` (501) |
| CreatePullRequest | POST | `/api/v1/projects/{projectID}/epics/{epicID}/pull-requests` | `CreatePullRequestPath` | - | `CreatePullRequestRequest` | - (204) |
| TransitionPullRequest | POST | `/api/v1/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/state-transitions` | `TransitionPullRequestPath` | - | `TransitionPullRequestRequest` | - (204) |
| GrantCodingRound | POST | `/api/v1/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/coding-rounds` | `GrantCodingRoundPath` | - | - | - (204) |
| MergePullRequest | POST | `/api/v1/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/merge` | `MergePullRequestPath` | - | - | `MergePullRequestResponse` (200) |
| ResetIssue | POST | `/api/v1/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/reset` | `ResetIssuePath` | - | - | - (204; may be 501) |
| GetPullRequestDiff | GET | `/api/v1/projects/{projectID}/epics/{epicID}/pull-requests/{pullRequestID}/diff` | `GetPullRequestDiffPath` | - | - | `PullRequestDiffResponse` (200) |
| OpenPullRequests | POST | `/api/v1/projects/{projectID}/epics/{epicID}/open-pull-requests` | `OpenPullRequestsPath` | - | - | `OpenPullRequestsResponse` (200; may be 501) |
| AddComment | POST | `/api/v1/projects/{projectID}/epics/{epicID}/comments` | `AddCommentPath` | - | `AddCommentRequest` | - (204) |
| ListOrganisations | GET | `/api/v1/organisations` | - | - | - | `ListOrganisationsResponse` (200) |
| AddOrganisation | POST | `/api/v1/organisations` | - | - | `AddOrganisationRequest` | - (204) |
| RemoveOrganisation | DELETE | `/api/v1/organisations/{name}` | `RemoveOrganisationPath` | - | - | - (204) |
| DiscoverOrganisations | POST | `/api/v1/organisations/discovery` | - | - | - | `DiscoverOrganisationsResponse` (200; may be 501) |
| ListRepositories | GET | `/api/v1/repositories` | - | - | - | `ListRepositoriesResponse` (200; may be 501) |
| AddRepository | POST | `/api/v1/repositories` | - | - | `AddRepositoryRequest` | `Repository` (201) |
| SyncRepositories | POST | `/api/v1/repositories/sync` | - | - | - | - (204; may be 501) |
| ListProjectRepositories | GET | `/api/v1/projects/{projectID}/repositories` | `ListProjectRepositoriesPath` | - | - | `ListProjectRepositoriesResponse` (200) |
| UpdateProjectRepositories | PUT | `/api/v1/projects/{projectID}/repositories` | `UpdateProjectRepositoriesPath` | - | `UpdateProjectRepositoriesRequest` | - (204) |
| GetAgentSettings | GET | `/api/v1/projects/{projectID}/agent-settings` | `GetAgentSettingsPath` | - | - | `AgentSettings` (200) |
| SetAgentRole | PUT | `/api/v1/projects/{projectID}/agent-settings/roles/{role}` | `SetAgentRolePath` | - | `SetAgentRoleRequest` | - (204) |
| ListAgentRuns | GET | `/api/v1/projects/{projectID}/agent-runs` | `ListAgentRunsPath` | - | - | `ListAgentRunsResponse` (200) |
| GetAgentRun | GET | `/api/v1/agent-runs/{runID}` | `GetAgentRunPath` | - | - | `AgentRun` (200) |
| RunOutput | GET | `/api/v1/agent-runs/{runID}/output` | `RunOutputPath` | `RunOutputQuery` (`from`) | - | `RunOutputPage` (200; may be 501) |
| AgentActivity | GET | `/api/v1/agent-runs/activity` | - | `AgentActivityQuery` (repeated `runID`) | - | `AgentActivityResponse` (200; may be 501) |
| CancelAgentRun | POST | `/api/v1/agent-runs/{runID}/cancel` | `CancelAgentRunPath` | - | - | `CancelAgentRunResponse` (200; may be 501) |
| ListSandboxes | GET | `/api/v1/projects/{projectID}/sandboxes` | `ListSandboxesPath` | - | - | `ListSandboxesResponse` (200) |
| ReconcileSandboxes | POST | `/api/v1/projects/{projectID}/maintenance/reconcile` | `ProjectPath` | - | - | `feature_not_configured` (501) |
| PurgeFinishedWork | POST | `/api/v1/projects/{projectID}/maintenance/purge` | `ProjectPath` | - | - | `feature_not_configured` (501) |

The four permanently unavailable routes remain in the contract because they are
registered server operations: manual epic agent execution, manual issue agent
execution, sandbox reconciliation, and finished-work purge. Optional features
may also return `501 feature_not_configured` when their use case is not wired.

The contract snapshot is deliberate. A future Go Merge route change requires
updating this inventory and its tests, or introducing a shared manifest in a
separate change.
