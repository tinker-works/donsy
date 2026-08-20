package httpapi

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"os"
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
	project, err := s.projectName(r)
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
	response := netomatic.ListEpicsResponse{Epics: make([]netomatic.Epic, 0, len(epics))}
	for _, epic := range epics {
		response.Epics = append(response.Epics, epicResponse(epic))
	}
	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) getEpicValue(r *http.Request) (epicpkg.Epic, error) {
	project, err := s.projectName(r)
	if err != nil {
		return epicpkg.Epic{}, err
	}
	if s.useCases.GetEpic == nil {
		return epicpkg.Epic{}, errUnavailable("reading epics")
	}
	return s.useCases.GetEpic.Handle(usecases.GetEpicQuery{Project: project, EpicID: r.PathValue("epic")})
}

func (s *Server) getEpic(w http.ResponseWriter, r *http.Request) {
	epic, err := s.getEpicValue(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, netomatic.GetEpicResponse{Epic: epicResponse(epic)})
}

func (s *Server) createEpic(w http.ResponseWriter, r *http.Request) {
	var request netomatic.CreateEpicRequest
	err := s.decode(r, &request)
	project, projectErr := s.projectName(r)
	if err == nil {
		err = projectErr
	}
	if err == nil && request.Project != "" && request.Project != project.Name {
		err = errInvalidRequest("project does not match request path")
	}
	if err == nil && strings.TrimSpace(request.Title) == "" {
		err = errInvalidRequest("epic title is required")
	}
	if err == nil && s.useCases.CreateEpic == nil {
		err = errUnavailable("creating epics")
	}
	if err == nil {
		var epic epicpkg.Epic
		epic, err = s.useCases.CreateEpic.Handle(usecases.CreateEpicCommand{Project: project, Title: request.Title, Assignee: s.useCases.CurrentUser, Body: request.Description})
		if err == nil {
			s.writeJSON(w, http.StatusOK, netomatic.CreateEpicResponse{Epic: epicResponse(epic)})
			return
		}
	}
	if err != nil {
		s.fail(w, err)
		return
	}
}

func (s *Server) prefixEpic(w http.ResponseWriter, r *http.Request) { s.mutateEpic(w, r, "prefix") }
func (s *Server) transitionEpic(w http.ResponseWriter, r *http.Request) {
	s.mutateEpic(w, r, "transition")
}

func (s *Server) mutateEpic(w http.ResponseWriter, r *http.Request, action string) {
	project, err := s.projectName(r)
	if err == nil && s.useCases.GetEpic == nil {
		err = errUnavailable("reading epics")
	}
	if err == nil {
		switch action {
		case "prefix":
			var request netomatic.PrefixEpicRequest
			err = s.decode(r, &request)
			if err == nil && (request.Project != project.Name || request.Epic != r.PathValue("epic")) {
				err = errInvalidRequest("request does not match path")
			}
			if err == nil && s.useCases.SetBranchPrefix == nil {
				err = errUnavailable("setting epic prefixes")
			}
			if err == nil {
				err = s.useCases.SetBranchPrefix.Handle(usecases.SetBranchPrefixCommand{Project: project, EpicID: request.Epic, Prefix: request.Prefix})
			}
		case "transition":
			var request netomatic.TransitionEpicRequest
			err = s.decode(r, &request)
			if err == nil && (request.Project != project.Name || request.Epic != r.PathValue("epic")) {
				err = errInvalidRequest("request does not match path")
			}
			if err == nil && !validEpicState(epicpkg.EpicState(request.Status)) {
				err = errInvalidRequest("status is not a valid epic state")
			}
			if err == nil && s.useCases.TransitionEpicState == nil {
				err = errUnavailable("transitioning epics")
			}
			if err == nil {
				err = s.useCases.TransitionEpicState.Handle(usecases.TransitionEpicStateCommand{Project: project, EpicID: request.Epic, State: epicpkg.EpicState(request.Status)})
			}
		}
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	epic, err := s.useCases.GetEpic.Handle(usecases.GetEpicQuery{Project: project, EpicID: r.PathValue("epic")})
	if err != nil {
		s.fail(w, err)
		return
	}
	if action == "prefix" {
		s.writeJSON(w, http.StatusOK, netomatic.PrefixEpicResponse{Epic: epicResponse(epic)})
		return
	}
	s.writeJSON(w, http.StatusOK, netomatic.TransitionEpicResponse{Epic: epicResponse(epic)})
}

func (s *Server) closeEpic(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectName(r)
	if err == nil && s.useCases.CloseEpic == nil {
		err = errUnavailable("closing epics")
	}
	if err == nil && s.useCases.GetEpic == nil {
		err = errUnavailable("reading epics")
	}
	if err == nil {
		err = s.useCases.CloseEpic.Handle(r.Context(), usecases.CloseEpicCommand{Project: project, EpicID: r.PathValue("epic")})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	epic, err := s.useCases.GetEpic.Handle(usecases.GetEpicQuery{Project: project, EpicID: r.PathValue("epic")})
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, netomatic.CloseEpicResponse{Epic: epicResponse(epic)})
}

func (s *Server) listIssues(w http.ResponseWriter, r *http.Request) {
	epic, err := s.getEpicValue(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	response := netomatic.ListIssuesResponse{Issues: make([]netomatic.Issue, 0, len(epic.Issues))}
	for _, issue := range epic.Issues {
		response.Issues = append(response.Issues, issueResponse(issue))
	}
	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) getIssue(w http.ResponseWriter, r *http.Request) {
	epic, err := s.getEpicValue(r)
	if err == nil {
		var issue epicpkg.Issue
		issue, err = epic.FindIssue(r.PathValue("issue"))
		if err == nil {
			s.writeJSON(w, http.StatusOK, netomatic.GetIssueResponse{Issue: issueResponse(issue)})
			return
		}
	}
	s.fail(w, err)
}

func (s *Server) createIssue(w http.ResponseWriter, r *http.Request) {
	var request netomatic.CreateIssueRequest
	err := s.decode(r, &request)
	project, projectErr := s.projectName(r)
	if err == nil {
		err = projectErr
	}
	if err == nil && (request.Project != project.Name || request.Epic != r.PathValue("epic")) {
		err = errInvalidRequest("request does not match path")
	}
	if err == nil && strings.TrimSpace(request.Title) == "" {
		err = errInvalidRequest("issue title is required")
	}
	if err == nil && s.useCases.CreateIssue == nil {
		err = errUnavailable("creating issues")
	}
	if err == nil {
		var epic epicpkg.Epic
		epic, err = s.getEpicValue(r)
		if err == nil && len(epic.Repositories) == 0 {
			err = errInvalidRequest("epic has no repository for the new issue")
		}
		if err != nil {
			s.fail(w, err)
			return
		}
		issue, handleErr := s.useCases.CreateIssue.Handle(usecases.CreateIssueCommand{
			Project: project, EpicID: request.Epic, Title: request.Title, Body: request.Description, Repository: epic.Repositories[0],
		})
		err = handleErr
		if err == nil {
			s.writeJSON(w, http.StatusOK, netomatic.CreateIssueResponse{Issue: issueResponse(issue)})
			return
		}
	}
	s.fail(w, err)
}

func (s *Server) closeIssue(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectName(r)
	if err == nil && s.useCases.CloseIssue == nil {
		err = errUnavailable("closing issues")
	}
	if err == nil && s.useCases.GetEpic == nil {
		err = errUnavailable("reading epics")
	}
	if err == nil {
		err = s.useCases.CloseIssue.Handle(r.Context(), usecases.CloseIssueCommand{Project: project, EpicID: r.PathValue("epic"), IssueID: r.PathValue("issue")})
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	epic, err := s.useCases.GetEpic.Handle(usecases.GetEpicQuery{Project: project, EpicID: r.PathValue("epic")})
	if err != nil {
		s.fail(w, err)
		return
	}
	issue, err := epic.FindIssue(r.PathValue("issue"))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, netomatic.CloseIssueResponse{Issue: issueResponse(issue)})
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
	project, err := s.projectName(r)
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
	response := netomatic.GetAgentSettingsResponse{Settings: make([]netomatic.AgentSettings, 0, len(agent.Roles()))}
	for _, role := range agent.Roles() {
		profile := settings.Roles[role]
		response.Settings = append(response.Settings, netomatic.AgentSettings{Agent: profile.Agent, Variant: profile.Variant, Values: map[string]string{"role": string(role), "maxRounds": strconv.Itoa(profile.MaxRounds)}})
	}
	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) listAgentRuns(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectName(r)
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
	response := netomatic.ListAgentRunsResponse{Runs: make([]netomatic.AgentRun, 0, len(runs))}
	for _, run := range runs {
		response.Runs = append(response.Runs, agentRunResponse(run, project.Name))
	}
	s.writeJSON(w, http.StatusOK, response)
}
func (s *Server) getAgentRun(w http.ResponseWriter, r *http.Request) {
	if s.useCases.GetAgentRun == nil {
		s.fail(w, errUnavailable("reading agent runs"))
		return
	}
	run, err := s.useCases.GetAgentRun.Handle(usecases.GetAgentRunQuery{RunID: r.PathValue("run")})
	if err != nil {
		s.fail(w, err)
		return
	}
	project, err := s.projectForRun(run)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, netomatic.GetAgentRunResponse{Run: agentRunResponse(run, project.Name)})
}

func (s *Server) cancelAgentRun(w http.ResponseWriter, r *http.Request) {
	if s.useCases.CancelAgentRun == nil || s.useCases.GetAgentRun == nil {
		s.fail(w, errUnavailable("agent run cancellation"))
		return
	}
	_, err := s.useCases.CancelAgentRun.Handle(usecases.CancelAgentRunCommand{RunID: r.PathValue("run")})
	if err != nil {
		s.fail(w, err)
		return
	}
	run, err := s.useCases.GetAgentRun.Handle(usecases.GetAgentRunQuery{RunID: r.PathValue("run")})
	if err != nil {
		s.fail(w, err)
		return
	}
	project, err := s.projectForRun(run)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, netomatic.CancelAgentRunResponse{Run: agentRunResponse(run, project.Name)})
}

func (s *Server) projectForRun(run agent.AgentRun) (domain.Project, error) {
	return s.findProject(func(project domain.Project) bool { return project.ID == run.ProjectID })
}

func (s *Server) listSandboxes(w http.ResponseWriter, _ *http.Request) {
	if s.useCases.ListProjects == nil || s.useCases.ListSandboxes == nil {
		s.fail(w, errUnavailable("listing sandboxes"))
		return
	}
	projects, err := s.useCases.ListProjects.Handle(usecases.ListProjectsQuery{})
	if err != nil {
		s.fail(w, err)
		return
	}
	response := netomatic.ListSandboxesResponse{Sandboxes: make([]netomatic.Sandbox, 0)}
	for _, project := range projects {
		sandboxes, err := s.useCases.ListSandboxes.Handle(usecases.ListSandboxesQuery{ProjectID: project.ID})
		if err != nil {
			s.fail(w, err)
			return
		}
		for _, sandbox := range sandboxes {
			response.Sandboxes = append(response.Sandboxes, sandboxResponse(sandbox))
		}
	}
	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) agentActivity(w http.ResponseWriter, r *http.Request) {
	if s.useCases.ReadRunOutput == nil {
		s.fail(w, errUnavailable("agent activity"))
		return
	}
	runID := r.PathValue("run")
	if strings.TrimSpace(runID) == "" {
		s.fail(w, errInvalidRequest("agent run ID is required"))
		return
	}
	size, ok := s.useCases.ReadRunOutput.Sizes([]string{runID})[runID]
	response := netomatic.AgentActivityResponse{}
	if ok && size > 0 {
		response.Activity = []netomatic.AgentActivity{{
			RunID: runID, Status: "available", Size: size,
		}}
	}
	s.writeJSON(w, http.StatusOK, response)
}

func (s *Server) runOutput(w http.ResponseWriter, r *http.Request) {
	if s.useCases.ReadRunOutput == nil {
		s.fail(w, errUnavailable("agent run output"))
		return
	}
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if r.URL.Query().Get("offset") != "" && (err != nil || offset < 0) {
		s.fail(w, errInvalidRequest("offset must be a non-negative integer"))
		return
	}
	page, err := s.useCases.ReadRunOutput.Handle(usecases.ReadRunOutputQuery{RunID: r.PathValue("run"), From: offset})
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, netomatic.RunOutputResponse{Output: netomatic.RunOutput{RunID: r.PathValue("run"), Output: page.Output, Next: page.Next, Done: page.Next == offset}})
}

func (s *Server) completeEpic(w http.ResponseWriter, r *http.Request) {
	var request netomatic.CompleteRequest
	err := s.decode(r, &request)
	if err == nil && strings.TrimSpace(request.Project) == "" {
		err = errInvalidRequest("project is required")
	}
	if err == nil && strings.TrimSpace(request.Run) == "" {
		err = errInvalidRequest("run is required")
	}
	project, projectErr := s.projectNameValue(request.Project)
	if err == nil {
		err = projectErr
	}
	if err == nil && (s.useCases.GetAgentRun == nil || s.useCases.CompleteEpic == nil) {
		err = errUnavailable("completing epics")
	}
	var completed bool
	if err == nil {
		var run agent.AgentRun
		run, err = s.useCases.GetAgentRun.Handle(usecases.GetAgentRunQuery{RunID: request.Run})
		if err == nil && run.ProjectID != project.ID {
			err = errInvalidRequest("run does not belong to project")
		}
		if err == nil && run.Subject.Kind != agent.AgentSubjectEpic {
			err = errInvalidRequest("run does not belong to an epic")
		}
		if err == nil {
			completed, err = s.useCases.CompleteEpic.Handle(usecases.CompleteEpicCommand{Project: project, EpicID: run.Subject.ID})
		}
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, netomatic.CompleteResponse{Complete: completed})
}

func (s *Server) reviewApprovedBranches(w http.ResponseWriter, r *http.Request) {
	var request netomatic.ReviewApprovedBranchesRequest
	err := s.decode(r, &request)
	if err == nil && strings.TrimSpace(request.Project) == "" {
		err = errInvalidRequest("project is required")
	}
	project, projectErr := s.projectNameValue(request.Project)
	if err == nil {
		err = projectErr
	}
	if err == nil && (s.useCases.ListEpics == nil || s.useCases.ReviewApprovedBranches == nil) {
		err = errUnavailable("reviewing approved branches")
	}
	branches := make([]string, 0)
	if err == nil {
		var epics []epicpkg.Epic
		epics, err = s.useCases.ListEpics.Handle(usecases.ListEpicsQuery{Project: project})
		for _, epic := range epics {
			if err != nil {
				break
			}
			for _, pullRequest := range epic.PullRequests {
				if pullRequest.Status == epicpkg.PullRequestOpen && pullRequest.Approved {
					branches = append(branches, pullRequest.Head)
				}
			}
			err = s.useCases.ReviewApprovedBranches.Handle(r.Context(), usecases.ReviewApprovedBranchesCommand{Project: project, EpicID: epic.ID})
		}
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, netomatic.ReviewApprovedBranchesResponse{Branches: branches})
}

func (s *Server) runEpic(w http.ResponseWriter, r *http.Request) {
	s.unavailable("manual epic agent execution")(w, r)
}
func (s *Server) runIssue(w http.ResponseWriter, r *http.Request) {
	s.unavailable("manual issue agent execution")(w, r)
}

func validEpicState(state epicpkg.EpicState) bool {
	switch state {
	case epicpkg.EpicStateConcept,
		epicpkg.EpicStateRefine,
		epicpkg.EpicStateReview,
		epicpkg.EpicStateChangesRequested,
		epicpkg.EpicStateProposed,
		epicpkg.EpicStateReady,
		epicpkg.EpicStateDone,
		epicpkg.EpicStateClosed,
		epicpkg.EpicStateFailed:
		return true
	default:
		return false
	}
}

func (s *Server) readDaemonLog(w http.ResponseWriter, r *http.Request) {
	if s.daemonLogPath == "" {
		s.fail(w, errUnavailable("daemon log"))
		return
	}
	offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	if err != nil {
		s.fail(w, errInvalidRequest("offset must be an integer"))
		return
	}
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil {
		s.fail(w, errInvalidRequest("limit must be an integer"))
		return
	}
	request, err := netomatic.BoundDaemonLogRequest(netomatic.ReadDaemonLogRequest{Offset: offset, Limit: limit})
	if err != nil {
		s.fail(w, errInvalidRequest(err.Error()))
		return
	}
	file, err := os.Open(s.daemonLogPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.fail(w, errUnavailable("daemon log"))
			return
		}
		s.fail(w, err)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			s.logger.Error("close daemon log", "error", err)
		}
	}()
	info, err := file.Stat()
	if err != nil {
		s.fail(w, err)
		return
	}
	response, err := pageDaemonLog(file, info.Size(), request)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, response)
}

func pageDaemonLog(file *os.File, size int64, request netomatic.ReadDaemonLogRequest) (netomatic.ReadDaemonLogResponse, error) {
	start := request.Offset
	reset := start > size
	if reset {
		start = 0
	}
	if start > 0 {
		var previous [1]byte
		if _, err := file.ReadAt(previous[:], start-1); err != nil {
			return netomatic.ReadDaemonLogResponse{}, err
		}
		if previous[0] != '\n' {
			cursor, complete, err := skipLogRecord(file, start, size)
			if err != nil {
				return netomatic.ReadDaemonLogResponse{}, err
			}
			if !complete {
				return netomatic.ReadDaemonLogResponse{NextOffset: cursor, OffsetReset: reset}, nil
			}
			start = cursor
		}
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return netomatic.ReadDaemonLogResponse{}, err
	}
	reader := bufio.NewReader(io.LimitReader(file, size-start))
	cursor := start
	usedBytes := 0
	lines := make([]string, 0, request.Limit)
	for len(lines) < request.Limit && cursor < size {
		line, length, complete, oversized, err := readLogRecord(reader)
		if err != nil {
			return netomatic.ReadDaemonLogResponse{}, err
		}
		if !complete {
			break
		}
		if oversized {
			cursor += int64(length)
			continue
		}
		if usedBytes+length > netomatic.MaxDaemonLogBytes {
			break
		}
		lines = append(lines, string(line[:len(line)-1]))
		usedBytes += length
		cursor += int64(length)
	}
	return netomatic.ReadDaemonLogResponse{Lines: lines, NextOffset: cursor, OffsetReset: reset}, nil
}

func skipLogRecord(file *os.File, start, size int64) (int64, bool, error) {
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return 0, false, err
	}
	_, length, complete, _, err := readLogRecord(bufio.NewReader(io.LimitReader(file, size-start)))
	return start + int64(length), complete, err
}

func readLogRecord(reader *bufio.Reader) ([]byte, int, bool, bool, error) {
	var line []byte
	length := 0
	oversized := false
	for {
		fragment, err := reader.ReadSlice('\n')
		length += len(fragment)
		if !oversized {
			if length > netomatic.MaxDaemonLogBytes {
				oversized = true
				line = nil
			} else {
				line = append(line, fragment...)
			}
		}
		switch {
		case err == nil:
			return line, length, true, oversized, nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return nil, length, false, oversized, nil
		default:
			return nil, 0, false, false, err
		}
	}
}
