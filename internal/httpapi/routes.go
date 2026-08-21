package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/application/usecases"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
	"github.com/tinker-works/donsy/netomatic"
)

func (s *Server) process(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, netomatic.ProcessResponse{CurrentUser: s.useCases.CurrentUser, Protocol: netomatic.ProtocolVersion})
}

func (s *Server) capabilities(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, netomatic.CapabilitiesResponse{
		"discoverOrganisations": s.useCases.DiscoverOrganisations != nil,
		"syncRepositories":      s.useCases.SyncRepositories != nil,
		"listRepositories":      s.useCases.ListRepositories != nil,
		"readRunOutput":         s.useCases.ReadRunOutput != nil,
		"reconcileSandboxes":    false,
		"runEpicAgent":          false,
		"runIssueAgent":         false,
		"cancelAgentRun":        s.useCases.CancelAgentRun != nil,
		"resetIssue":            s.useCases.ResetIssue != nil,
	})
}

func (s *Server) listProjects(w http.ResponseWriter, _ *http.Request) {
	if s.useCases.ListProjects == nil {
		s.fail(w, errUnavailable("listing projects"))
		return
	}
	projects, err := s.useCases.ListProjects.Handle(usecases.ListProjectsQuery{})
	if err != nil {
		s.fail(w, err)
		return
	}
	response := make(netomatic.ListProjectsResponse, 0, len(projects))
	for _, project := range projects {
		response = append(response, projectResponse(project))
	}
	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	if s.useCases.CreateProject == nil {
		s.fail(w, errUnavailable("creating projects"))
		return
	}
	var request netomatic.CreateProjectRequest
	if err := s.decode(r, &request); err != nil {
		s.fail(w, err)
		return
	}
	if !validProjectName(request.Name) {
		s.fail(w, errInvalidRequest("project name must contain only letters, numbers, and dashes"))
		return
	}
	project, err := s.useCases.CreateProject.Handle(usecases.CreateProjectCommand{Name: request.Name})
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, projectResponse(project))
}

func (s *Server) openProject(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.OpenProject == nil {
		err = errUnavailable("opening projects")
	}
	if err == nil {
		err = s.useCases.OpenProject.Handle(usecases.OpenProjectCommand{Project: project})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) forgetProject(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.ForgetProject == nil {
		err = errUnavailable("forgetting projects")
	}
	if err == nil {
		err = s.useCases.ForgetProject.Handle(r.Context(), usecases.ForgetProjectCommand{ProjectID: project.ID})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listProjectSummaries(w http.ResponseWriter, _ *http.Request) {
	if s.useCases.ListProjectSummaries == nil {
		s.fail(w, errUnavailable("listing project summaries"))
		return
	}
	summaries, err := s.useCases.ListProjectSummaries.Handle()
	if err != nil {
		s.fail(w, err)
		return
	}
	response := make(netomatic.ListProjectSummariesResponse, 0, len(summaries))
	for _, summary := range summaries {
		item := netomatic.ProjectSummary{Project: projectResponse(summary.Project), Epics: summary.Epics, Running: summary.Running}
		if summary.Err != nil {
			item.Error = &netomatic.APIError{Code: "workspace_unavailable", Detail: "the project workspace could not be read"}
		}
		response = append(response, item)
	}
	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) storeSetup(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.StoreSetup == nil {
		err = errUnavailable("reading store setup")
	}
	var state netomatic.SetupState
	if err == nil {
		value, handleErr := s.useCases.StoreSetup.Handle(project)
		err = handleErr
		state = netomatic.SetupState{Organisations: value.Organisations, Repositories: value.Repositories, RolesSet: value.RolesSet, RolesTotal: value.RolesTotal}
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, state)
}

func (s *Server) initialiseStore(w http.ResponseWriter, r *http.Request) {
	var request netomatic.InitialiseStoreRequest
	err := s.decode(r, &request)
	project, projectErr := s.projectID(r)
	if err == nil {
		err = projectErr
	}
	if err == nil && s.useCases.InitialiseStore == nil {
		err = errUnavailable("initialising stores")
	}
	if err == nil && strings.TrimSpace(request.Model) == "" {
		err = errInvalidRequest("a model is required to initialise the store")
	}
	if err == nil {
		err = s.useCases.InitialiseStore.Handle(usecases.InitialiseStoreCommand{Project: project, Model: request.Model, Variant: request.Variant, Repositories: request.Repositories})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listEpics(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.ListEpics == nil {
		err = errUnavailable("listing epics")
	}
	var epics []epicpkg.Epic
	if err == nil {
		epics, err = s.useCases.ListEpics.Handle(usecases.ListEpicsQuery{Project: project})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	response := make(netomatic.ListEpicsResponse, 0, len(epics))
	for _, epic := range epics {
		response = append(response, epicResponse(epic))
	}
	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) getEpicValue(r *http.Request) (epicpkg.Epic, error) {
	project, err := s.projectID(r)
	if err != nil {
		return epicpkg.Epic{}, err
	}
	if s.useCases.GetEpic == nil {
		return epicpkg.Epic{}, errUnavailable("reading epics")
	}
	return s.useCases.GetEpic.Handle(usecases.GetEpicQuery{Project: project, EpicID: r.PathValue("epicID")})
}

func (s *Server) getEpic(w http.ResponseWriter, r *http.Request) {
	epic, err := s.getEpicValue(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, epicResponse(epic))
}

func (s *Server) createEpic(w http.ResponseWriter, r *http.Request) {
	var request netomatic.CreateEpicRequest
	err := s.decode(r, &request)
	project, projectErr := s.projectID(r)
	if err == nil {
		err = projectErr
	}
	if err == nil && strings.TrimSpace(request.Title) == "" {
		err = errInvalidRequest("epic title is required")
	}
	if err == nil && s.useCases.CreateEpic == nil {
		err = errUnavailable("creating epics")
	}
	if err == nil {
		_, err = s.useCases.CreateEpic.Handle(usecases.CreateEpicCommand{Project: project, Title: request.Title, Assignee: request.Assignee, Body: request.Body, Repositories: request.Repositories, BranchPrefix: request.BranchPrefix})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) closeEpic(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.CloseEpic == nil {
		err = errUnavailable("closing epics")
	}
	if err == nil {
		err = s.useCases.CloseEpic.Handle(r.Context(), usecases.CloseEpicCommand{Project: project, EpicID: r.PathValue("epicID")})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) transitionEpicState(w http.ResponseWriter, r *http.Request) {
	var request netomatic.TransitionEpicStateRequest
	err := s.decode(r, &request)
	project, projectErr := s.projectID(r)
	if err == nil {
		err = projectErr
	}
	if err == nil && s.useCases.TransitionEpicState == nil {
		err = errUnavailable("transitioning epics")
	}
	if err == nil {
		err = s.useCases.TransitionEpicState.Handle(usecases.TransitionEpicStateCommand{
			Project: project, EpicID: r.PathValue("epicID"), State: epicpkg.EpicState(request.State), Force: request.Force,
		})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setBranchPrefix(w http.ResponseWriter, r *http.Request) {
	var request netomatic.SetBranchPrefixRequest
	err := s.decode(r, &request)
	project, projectErr := s.projectID(r)
	if err == nil {
		err = projectErr
	}
	if err == nil && s.useCases.SetBranchPrefix == nil {
		err = errUnavailable("setting epic prefixes")
	}
	if err == nil {
		err = s.useCases.SetBranchPrefix.Handle(usecases.SetBranchPrefixCommand{Project: project, EpicID: r.PathValue("epicID"), Prefix: request.Prefix})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createIssue(w http.ResponseWriter, r *http.Request) {
	var request netomatic.CreateIssueRequest
	err := s.decode(r, &request)
	project, projectErr := s.projectID(r)
	if err == nil {
		err = projectErr
	}
	if err == nil && strings.TrimSpace(request.Title) == "" {
		err = errInvalidRequest("issue title is required")
	}
	if err == nil && s.useCases.CreateIssue == nil {
		err = errUnavailable("creating issues")
	}
	if err == nil {
		_, err = s.useCases.CreateIssue.Handle(usecases.CreateIssueCommand{
			Project: project, EpicID: r.PathValue("epicID"), ParentID: request.ParentID, Title: request.Title, Body: request.Body, Repository: request.Repository,
		})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) closeIssue(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.CloseIssue == nil {
		err = errUnavailable("closing issues")
	}
	if err == nil {
		err = s.useCases.CloseIssue.Handle(r.Context(), usecases.CloseIssueCommand{Project: project, EpicID: r.PathValue("epicID"), IssueID: r.PathValue("issueID")})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createPullRequest(w http.ResponseWriter, r *http.Request) {
	var request netomatic.CreatePullRequestRequest
	err := s.decode(r, &request)
	project, projectErr := s.projectID(r)
	if err == nil {
		err = projectErr
	}
	if err == nil && s.useCases.CreatePullRequest == nil {
		err = errUnavailable("creating pull requests")
	}
	if err == nil && (strings.TrimSpace(request.IssueID) == "" || strings.TrimSpace(request.Title) == "") {
		err = errInvalidRequest("pull request issue and title are required")
	}
	if err == nil {
		err = s.useCases.CreatePullRequest.Handle(usecases.CreatePullRequestCommand{Project: project, EpicID: r.PathValue("epicID"), IssueID: request.IssueID, Title: request.Title, Repository: request.Repository, Head: request.Head, Base: request.Base})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) transitionPullRequest(w http.ResponseWriter, r *http.Request) {
	var request netomatic.TransitionPullRequestRequest
	err := s.decode(r, &request)
	project, projectErr := s.projectID(r)
	if err == nil {
		err = projectErr
	}
	if err == nil && s.useCases.TransitionPullRequest == nil {
		err = errUnavailable("transitioning pull requests")
	}
	if err == nil && epicpkg.PullRequestStatus(request.Status) != epicpkg.PullRequestClosed {
		err = errInvalidRequest("only closed pull request status can be recorded")
	}
	if err == nil {
		err = s.useCases.TransitionPullRequest.Handle(r.Context(), usecases.TransitionPullRequestCommand{Project: project, EpicID: r.PathValue("epicID"), PullRequestID: r.PathValue("pullRequestID"), Status: epicpkg.PullRequestStatus(request.Status)})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) grantCodingRound(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.GrantCodingRound == nil {
		err = errUnavailable("granting coding rounds")
	}
	if err == nil {
		err = s.useCases.GrantCodingRound.Handle(r.Context(), usecases.GrantCodingRoundCommand{Project: project, EpicID: r.PathValue("epicID"), PullRequestID: r.PathValue("pullRequestID")})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) mergePullRequest(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.MergePullRequest == nil {
		err = errUnavailable("merging pull requests")
	}
	if err == nil {
		err = s.useCases.MergePullRequest.Handle(r.Context(), usecases.MergePullRequestCommand{Project: project, EpicID: r.PathValue("epicID"), PullRequestID: r.PathValue("pullRequestID")})
	}
	if errors.Is(err, agent_runtime.ErrMergeConflict) {
		s.writeJSON(w, http.StatusOK, netomatic.MergePullRequestResponse{Outcome: netomatic.MergeOutcomeReturnedToCoding})
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, netomatic.MergePullRequestResponse{Outcome: netomatic.MergeOutcomeMerged})
}

func (s *Server) resetIssue(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.ResetIssue == nil {
		err = errUnavailable("issue reset")
	}
	if err == nil {
		err = s.useCases.ResetIssue.Handle(r.Context(), usecases.ResetIssueCommand{Project: project, EpicID: r.PathValue("epicID"), PullRequestID: r.PathValue("pullRequestID")})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getPullRequestDiff(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.GetPullRequestDiff == nil {
		err = errUnavailable("reading pull request diffs")
	}
	var diff string
	if err == nil {
		diff, err = s.useCases.GetPullRequestDiff.Handle(r.Context(), usecases.GetPullRequestDiffQuery{Project: project, EpicID: r.PathValue("epicID"), PullRequestID: r.PathValue("pullRequestID")})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, netomatic.PullRequestDiffResponse{Diff: diff})
}

func (s *Server) openPullRequests(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.OpenPullRequests == nil {
		err = errUnavailable("opening pull requests")
	}
	var opened int
	if err == nil {
		opened, err = s.useCases.OpenPullRequests.Handle(r.Context(), usecases.OpenPullRequestsCommand{Project: project, EpicID: r.PathValue("epicID")})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, netomatic.OpenPullRequestsResponse{Opened: opened})
}

func (s *Server) addComment(w http.ResponseWriter, r *http.Request) {
	var request netomatic.AddCommentRequest
	err := s.decode(r, &request)
	project, projectErr := s.projectID(r)
	if err == nil {
		err = projectErr
	}
	target := usecases.CommentTarget(request.Target)
	if err == nil && target != usecases.IssueCommentTarget && target != usecases.PullRequestCommentTarget {
		err = errInvalidRequest("target must be issue or pull_request")
	}
	if err == nil && s.useCases.AddComment == nil {
		err = errUnavailable("adding comments")
	}
	if err == nil {
		err = s.useCases.AddComment.Handle(usecases.AddCommentCommand{Project: project, EpicID: r.PathValue("epicID"), TargetID: request.TargetID, Target: target, Author: s.useCases.CurrentUser, Body: request.Body})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listOrganisations(w http.ResponseWriter, _ *http.Request) {
	if s.useCases.ListOrganisations == nil {
		s.fail(w, errUnavailable("listing organisations"))
		return
	}
	organisations, err := s.useCases.ListOrganisations.Handle()
	if err != nil {
		s.fail(w, err)
		return
	}
	response := make(netomatic.ListOrganisationsResponse, 0, len(organisations))
	for _, organisation := range organisations {
		response = append(response, netomatic.Organisation{Name: organisation.Name})
	}
	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) addOrganisation(w http.ResponseWriter, r *http.Request) {
	var request netomatic.AddOrganisationRequest
	err := s.decode(r, &request)
	if err == nil && strings.TrimSpace(request.Name) == "" {
		err = errInvalidRequest("organisation name is required")
	}
	if err == nil && s.useCases.AddOrganisation == nil {
		err = errUnavailable("adding organisations")
	}
	if err == nil {
		err = s.useCases.AddOrganisation.Handle(request.Name)
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) removeOrganisation(w http.ResponseWriter, r *http.Request) {
	var err error
	if strings.TrimSpace(r.PathValue("name")) == "" {
		err = errInvalidRequest("organisation name is required")
	} else if s.useCases.RemoveOrganisation == nil {
		err = errUnavailable("removing organisations")
	} else {
		err = s.useCases.RemoveOrganisation.Handle(r.PathValue("name"))
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) discoverOrganisations(w http.ResponseWriter, r *http.Request) {
	if s.useCases.DiscoverOrganisations == nil {
		s.fail(w, errUnavailable("organisation discovery"))
		return
	}
	organisations, err := s.useCases.DiscoverOrganisations.Handle(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	response := make(netomatic.DiscoverOrganisationsResponse, 0, len(organisations))
	for _, organisation := range organisations {
		response = append(response, netomatic.Organisation{Name: organisation.Name})
	}
	s.writeJSON(w, http.StatusOK, response)
}
func (s *Server) listRepositories(w http.ResponseWriter, _ *http.Request) {
	if s.useCases.ListRepositories == nil {
		s.fail(w, errUnavailable("repository listing"))
		return
	}
	repositories, err := s.useCases.ListRepositories.Handle()
	if err != nil {
		s.fail(w, err)
		return
	}
	response := make(netomatic.ListRepositoriesResponse, 0, len(repositories))
	for _, repository := range repositories {
		response = append(response, repositoryResponse(repository))
	}
	s.writeJSON(w, http.StatusOK, response)
}
func (s *Server) syncRepositories(w http.ResponseWriter, r *http.Request) {
	if s.useCases.SyncRepositories == nil {
		s.fail(w, errUnavailable("repository sync"))
		return
	}
	if err := s.useCases.SyncRepositories.Handle(r.Context()); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addRepository(w http.ResponseWriter, r *http.Request) {
	var request netomatic.AddRepositoryRequest
	err := s.decode(r, &request)
	if err == nil {
		if _, _, ok := domain.SplitRepositoryRef(request.FullName); !ok {
			err = errInvalidRequest("repository must use owner/name form")
		}
	}
	if err == nil && s.useCases.AddRepository == nil {
		err = errUnavailable("adding repositories")
	}
	var repository netomatic.Repository
	if err == nil {
		value, handleErr := s.useCases.AddRepository.Handle(usecases.AddRepositoryCommand{FullName: request.FullName})
		err = handleErr
		repository = repositoryResponse(value)
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, repository)
}

func (s *Server) listProjectRepositories(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.ListProjectRepositories == nil {
		err = errUnavailable("listing project repositories")
	}
	var repositories []string
	if err == nil {
		repositories, err = s.useCases.ListProjectRepositories.Handle(project)
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, netomatic.ListProjectRepositoriesResponse(repositories))
}
func (s *Server) updateProjectRepositories(w http.ResponseWriter, r *http.Request) {
	var request netomatic.UpdateProjectRepositoriesRequest
	err := s.decode(r, &request)
	project, projectErr := s.projectID(r)
	if err == nil {
		err = projectErr
	}
	if err == nil && s.useCases.UpdateRepositories == nil {
		err = errUnavailable("updating project repositories")
	}
	if err == nil {
		err = s.useCases.UpdateRepositories.Handle(usecases.UpdateProjectRepositoriesCommand{Project: project, Repositories: request.Repositories})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getAgentSettings(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.GetAgentSettings == nil {
		err = errUnavailable("reading agent settings")
	}
	var settings agent.AgentSettings
	if err == nil {
		settings, err = s.useCases.GetAgentSettings.Handle(usecases.GetAgentSettingsQuery{Project: project})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	response := netomatic.AgentSettings{SetupScript: settings.SetupScript, Roles: make(map[string]netomatic.AgentProfile, len(settings.Roles))}
	for role, profile := range settings.Roles {
		response.Roles[string(role)] = netomatic.AgentProfile{Agent: profile.Agent, Variant: profile.Variant, MaxRounds: profile.MaxRounds}
	}
	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) setAgentRole(w http.ResponseWriter, r *http.Request) {
	var request netomatic.SetAgentRoleRequest
	err := s.decode(r, &request)
	role := agent.AgentRole(r.PathValue("role"))
	if err == nil && !agent.IsAgentRole(role) {
		err = errInvalidRequest("role is not valid")
	}
	project, projectErr := s.projectID(r)
	if err == nil {
		err = projectErr
	}
	if err == nil && s.useCases.SetAgentRole == nil {
		err = errUnavailable("setting agent roles")
	}
	if err == nil {
		err = s.useCases.SetAgentRole.Handle(usecases.SetAgentRoleCommand{Project: project, Role: role, Agent: request.Agent, Variant: request.Variant})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listAgentRuns(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.ListAgentRuns == nil {
		err = errUnavailable("listing agent runs")
	}
	var runs []agent.AgentRun
	if err == nil {
		runs, err = s.useCases.ListAgentRuns.Handle(usecases.ListAgentRunsQuery{ProjectID: project.ID})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	response := make(netomatic.ListAgentRunsResponse, 0, len(runs))
	for _, run := range runs {
		response = append(response, agentRunResponse(run))
	}
	s.writeJSON(w, http.StatusOK, response)
}
func (s *Server) getAgentRun(w http.ResponseWriter, r *http.Request) {
	if s.useCases.GetAgentRun == nil {
		s.fail(w, errUnavailable("reading agent runs"))
		return
	}
	run, err := s.useCases.GetAgentRun.Handle(usecases.GetAgentRunQuery{RunID: r.PathValue("runID")})
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, agentRunResponse(run))
}

func (s *Server) cancelAgentRun(w http.ResponseWriter, r *http.Request) {
	if s.useCases.CancelAgentRun == nil {
		s.fail(w, errUnavailable("agent run cancellation"))
		return
	}
	cancelled, err := s.useCases.CancelAgentRun.Handle(usecases.CancelAgentRunCommand{RunID: r.PathValue("runID")})
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, netomatic.CancelAgentRunResponse{Cancelled: cancelled})
}

func (s *Server) listSandboxes(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.ListSandboxes == nil {
		s.fail(w, errUnavailable("listing sandboxes"))
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	sandboxes, err := s.useCases.ListSandboxes.Handle(usecases.ListSandboxesQuery{ProjectID: project.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	response := make(netomatic.ListSandboxesResponse, 0, len(sandboxes))
	for _, sandbox := range sandboxes {
		response = append(response, sandboxResponse(sandbox))
	}
	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) agentActivity(w http.ResponseWriter, r *http.Request) {
	if s.useCases.ReadRunOutput == nil {
		s.fail(w, errUnavailable("agent activity"))
		return
	}
	s.writeJSON(w, http.StatusOK, netomatic.AgentActivityResponse{Sizes: s.useCases.ReadRunOutput.Sizes(r.URL.Query()["runID"])})
}

func (s *Server) runOutput(w http.ResponseWriter, r *http.Request) {
	if s.useCases.ReadRunOutput == nil {
		s.fail(w, errUnavailable("agent run output"))
		return
	}
	from, err := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	if r.URL.Query().Get("from") != "" && (err != nil || from < 0) {
		s.fail(w, errInvalidRequest("from must be a non-negative integer"))
		return
	}
	page, err := s.useCases.ReadRunOutput.Handle(usecases.ReadRunOutputQuery{RunID: r.PathValue("runID"), From: from})
	if err != nil {
		s.fail(w, err)
		return
	}
	entries := make([]netomatic.TranscriptEntry, len(page.Entries))
	for index, entry := range page.Entries {
		entries[index] = netomatic.TranscriptEntry{Kind: uint8(entry.Kind), Tool: entry.Tool, CallID: entry.CallID, Text: entry.Text}
	}
	s.writeJSON(w, http.StatusOK, netomatic.RunOutputPage{Entries: entries, Next: page.Next})
}

func (s *Server) completeEpic(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.CompleteEpic == nil {
		err = errUnavailable("completing epics")
	}
	var completed bool
	if err == nil {
		completed, err = s.useCases.CompleteEpic.Handle(usecases.CompleteEpicCommand{Project: project, EpicID: r.PathValue("epicID")})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, netomatic.CompleteEpicResponse{Completed: completed})
}

func (s *Server) reviewApprovedBranches(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectID(r)
	if err == nil && s.useCases.ReviewApprovedBranches == nil {
		err = errUnavailable("reviewing approved branches")
	}
	if err == nil {
		err = s.useCases.ReviewApprovedBranches.Handle(r.Context(), usecases.ReviewApprovedBranchesCommand{Project: project, EpicID: r.PathValue("epicID")})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) runEpic(w http.ResponseWriter, r *http.Request) {
	s.unavailable("manual epic agent execution")(w, r)
}
func (s *Server) runIssue(w http.ResponseWriter, r *http.Request) {
	s.unavailable("manual issue agent execution")(w, r)
}
