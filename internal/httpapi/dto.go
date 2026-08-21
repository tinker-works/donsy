package httpapi

import (
	"time"

	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
	"github.com/tinker-works/donsy/netomatic"
)

func timestamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func projectResponse(project domain.Project) netomatic.Project {
	return netomatic.Project{ID: project.ID, Name: project.Name, LastOpenedAt: timestamp(project.LastOpenedAt)}
}

func repositoryResponse(repository domain.Repository) netomatic.Repository {
	return netomatic.Repository{
		Name: repository.Name, FullName: repository.FullName, HTTPURL: repository.HTTPURL,
		SSHURL: repository.SSHURL, Organisation: repository.Organisation,
	}
}

func commentResponse(comment epicpkg.Comment) netomatic.Comment {
	return netomatic.Comment{ID: comment.ID, Author: comment.Author, CreatedAt: timestamp(comment.CreatedAt), Body: comment.Body}
}

func issueResponse(issue epicpkg.Issue) netomatic.Issue {
	comments := make([]netomatic.Comment, 0, len(issue.Comments))
	for _, comment := range issue.Comments {
		comments = append(comments, commentResponse(comment))
	}
	return netomatic.Issue{
		ID: issue.ID, Title: issue.Title, ParentID: issue.ParentID, Repository: issue.Repository,
		State: string(issue.State), CreatedAt: timestamp(issue.CreatedAt), Body: issue.Body,
		Comments: comments, BlockedBy: append([]string(nil), issue.BlockedBy...),
	}
}

func pullRequestResponse(pullRequest epicpkg.PullRequest) netomatic.PullRequest {
	comments := make([]netomatic.Comment, 0, len(pullRequest.Comments))
	for _, comment := range pullRequest.Comments {
		comments = append(comments, commentResponse(comment))
	}
	flags := make([]string, 0, len(pullRequest.Flags))
	for _, flag := range pullRequest.Flags {
		flags = append(flags, string(flag))
	}
	return netomatic.PullRequest{
		ID: pullRequest.ID, IssueID: pullRequest.IssueID, Title: pullRequest.Title, Status: string(pullRequest.Status),
		Repository: pullRequest.Repository, Number: pullRequest.Number, URL: pullRequest.URL, Head: pullRequest.Head,
		Base: pullRequest.Base, Flags: flags, ReviewedHead: pullRequest.ReviewedHead, ReviewedBase: pullRequest.ReviewedBase,
		Rounds: pullRequest.Rounds, Reviews: pullRequest.Reviews, RoundsGranted: pullRequest.RoundsGranted,
		CodingRounds: pullRequest.CodingRounds, Approved: pullRequest.Approved, CreatedAt: timestamp(pullRequest.CreatedAt), Comments: comments,
	}
}

func epicResponse(epic epicpkg.Epic) netomatic.Epic {
	issues := make([]netomatic.Issue, 0, len(epic.Issues))
	for _, issue := range epic.Issues {
		issues = append(issues, issueResponse(issue))
	}
	pullRequests := make([]netomatic.PullRequest, 0, len(epic.PullRequests))
	for _, pullRequest := range epic.PullRequests {
		pullRequests = append(pullRequests, pullRequestResponse(pullRequest))
	}
	return netomatic.Epic{
		ID: epic.ID, Title: epic.Title, Assignee: epic.Assignee, Repositories: append([]string(nil), epic.Repositories...),
		Body: epic.Body, State: string(epic.State), BranchPrefix: epic.BranchPrefix, Issues: issues,
		PullRequests: pullRequests, DraftingPasses: epic.DraftingPasses,
	}
}

func agentRunResponse(run agent.AgentRun) netomatic.AgentRun {
	return netomatic.AgentRun{
		ID: run.ID, ProjectID: run.ProjectID, SandboxID: run.SandboxID, Role: string(run.Role),
		Subject: netomatic.AgentSubject{Kind: string(run.Subject.Kind), ID: run.Subject.ID}, Engine: string(run.Engine),
		Agent: run.Agent, Variant: run.Variant, SessionMode: string(run.SessionMode), Status: string(run.Status),
		Round: run.Round, Error: run.Error,
		Usage:     netomatic.RunUsage{TokensIn: run.Usage.TokensIn, TokensOut: run.Usage.TokensOut, CostUSD: run.Usage.CostUSD},
		CreatedAt: timestamp(run.CreatedAt), StartedAt: timestampPointer(run.StartedAt), FinishedAt: timestampPointer(run.FinishedAt),
	}
}

func sandboxResponse(sandbox agent.Sandbox) netomatic.Sandbox {
	return netomatic.Sandbox{
		ID: sandbox.ID, ProjectID: sandbox.ProjectID, Name: sandbox.Name, Role: string(sandbox.Role),
		Subject: netomatic.AgentSubject{Kind: string(sandbox.Subject.Kind), ID: sandbox.Subject.ID},
		Status:  string(sandbox.Status), CreatedAt: timestamp(sandbox.CreatedAt), UpdatedAt: timestamp(sandbox.UpdatedAt),
	}
}

func timestampPointer(value *time.Time) *string {
	if value == nil {
		return nil
	}
	result := timestamp(*value)
	return &result
}
