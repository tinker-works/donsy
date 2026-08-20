package usecases

import (
	"context"
	"fmt"
	"sync"

	"github.com/tinker-works/donsy/internal/application/agent_runtime"
	"github.com/tinker-works/donsy/internal/domain"
	"github.com/tinker-works/donsy/internal/domain/agent"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

type fakeAgentCredentials struct {
	discarded  []string
	discardErr error
}

func (c *fakeAgentCredentials) Discard(sandboxName string) error {
	c.discarded = append(c.discarded, sandboxName)
	return c.discardErr
}

func (*fakeAgentCredentials) OpenCodeMount(string, string) (agent_runtime.SandboxMount, error) {
	return agent_runtime.SandboxMount{
		HostLocation:  "/tmp/credentials",
		GuestLocation: "/run/go-merge/credentials",
	}, nil
}

// fakeRepositoryWorkspace is shared by every round in a tick, so it is guarded
// like the real one, which serializes per epic directory.
type fakeRepositoryWorkspace struct {
	mu       sync.Mutex
	purged   []string
	purgeErr error
	ensured  []string
}

func (w *fakeRepositoryWorkspace) Purge(epicID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.purged = append(w.purged, epicID)
	return w.purgeErr
}

func (w *fakeRepositoryWorkspace) Ensure(
	_ context.Context, epicID, repository string,
) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ensured = append(w.ensured, repository)
	return "/tmp/repositories/" + epicID + "/" + repository, nil
}

type fakeIssueTreeStore struct{}

func (fakeIssueTreeStore) Write(string, epicpkg.Epic) (string, error) { return "/tmp/epic", nil }

func (fakeIssueTreeStore) Read(_ string, detail epicpkg.Epic) (epicpkg.Epic, error) {
	epics := append([]epicpkg.Issue(nil), detail.Issues...)
	epics[0].Body = "Refined issue tree."
	epics = append(epics, epicpkg.Issue{
		ID: "child-1", Title: "Implement workflow", ParentID: epics[0].ID,
		Repository: "acme/widgets", State: epicpkg.IssueStateOpen,
		CreatedAt: epics[0].CreatedAt, Body: "Implement and test the workflow.",
	})
	detail.Issues = epics
	return detail, nil
}

type fakeRegistry struct {
	projects  []domain.Project
	listErr   error
	createErr error
	touchErr  error
	touchedID uint
	deletedID uint
	deleteErr error
}

// fakeAgentRegistry stands in for the sqlite registry, which concurrent rounds
// all reach at once. The real one serializes on a single connection; this one
// takes a mutex, so a test driving several rounds is not racing the fake
// instead of exercising the code. Tests read the fields directly once their
// rounds have finished, which is why the fields stay exported to the package.
type fakeAgentRegistry struct {
	mu               sync.Mutex
	sandboxes        []agent.Sandbox
	savedSandboxes   []agent.Sandbox
	runs             []agent.AgentRun
	listRunsErr      error
	deleteSubjectErr error

	// listSandboxesErr, when set, is returned by ListSandboxes. If
	// listSandboxesErrFor is non-zero, the
	// error only applies to that project ID, letting tests fail one project while
	// leaving others healthy.
	listSandboxesErr    error
	listSandboxesErrFor uint
	// sandboxesFor, when non-zero, scopes sandboxes to that project ID: other projects list
	// nothing, the way the sqlite registry separates projects.
	sandboxesFor uint

	forgottenProjects []uint
}

func (r *fakeAgentRegistry) DeleteSubjectRuntime(projectID uint, subject agent.AgentSubject) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.deleteSubjectErr != nil {
		return r.deleteSubjectErr
	}
	keptSandboxes := make([]agent.Sandbox, 0, len(r.sandboxes))
	for _, sandbox := range r.sandboxes {
		if sandbox.ProjectID != projectID || sandbox.Subject != subject {
			keptSandboxes = append(keptSandboxes, sandbox)
		}
	}
	r.sandboxes = keptSandboxes
	keptRuns := make([]agent.AgentRun, 0, len(r.runs))
	for _, run := range r.runs {
		if run.ProjectID != projectID || run.Subject != subject {
			keptRuns = append(keptRuns, run)
		}
	}
	r.runs = keptRuns
	return nil
}

func (r *fakeAgentRegistry) DeleteProjectRuntime(projectID uint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forgottenProjects = append(r.forgottenProjects, projectID)
	kept := make([]agent.Sandbox, 0, len(r.sandboxes))
	for _, sandbox := range r.sandboxes {
		if sandbox.ProjectID != projectID {
			kept = append(kept, sandbox)
		}
	}
	r.sandboxes = kept
	runs := make([]agent.AgentRun, 0, len(r.runs))
	for _, run := range r.runs {
		if run.ProjectID != projectID {
			runs = append(runs, run)
		}
	}
	r.runs = runs
	return nil
}

func (r *fakeAgentRegistry) SaveSandbox(sandbox agent.Sandbox) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.savedSandboxes = append(r.savedSandboxes, sandbox)
	return nil
}

func (r *fakeAgentRegistry) ListSandboxes(projectID uint) ([]agent.Sandbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listSandboxesErr != nil &&
		(r.listSandboxesErrFor == 0 || r.listSandboxesErrFor == projectID) {
		return nil, r.listSandboxesErr
	}
	if r.sandboxesFor != 0 && r.sandboxesFor != projectID {
		return nil, nil
	}
	return r.sandboxes, nil
}

// SaveAgentRun validates like the sqlite registry does. Without that, a use case
// can mint a run the real store would reject and every test still passes, while
// the round dies at the first save in production with no record of why.
func (r *fakeAgentRegistry) SaveAgentRun(run agent.AgentRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := run.Validate(); err != nil {
		return err
	}
	for index, current := range r.runs {
		if current.ID == run.ID {
			r.runs[index] = run
			return nil
		}
	}
	r.runs = append(r.runs, run)
	return nil
}

func (r *fakeAgentRegistry) ListAgentRuns(
	uint,
	agent.AgentSubject,
) ([]agent.AgentRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]agent.AgentRun(nil), r.runs...), nil
}

func (r *fakeAgentRegistry) ListProjectAgentRuns(projectID uint) ([]agent.AgentRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	runs := make([]agent.AgentRun, 0, len(r.runs))
	for _, run := range r.runs {
		if run.ProjectID == projectID {
			runs = append(runs, run)
		}
	}
	return runs, r.listRunsErr
}

func (r *fakeAgentRegistry) GetAgentRun(runID string) (agent.AgentRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, run := range r.runs {
		if run.ID == runID {
			return run, nil
		}
	}
	return agent.AgentRun{}, fmt.Errorf("agent run %q not found", runID)
}

func (r *fakeRegistry) List() ([]domain.Project, error) { return r.projects, r.listErr }

func (r *fakeRegistry) Create(project *domain.Project) error {
	if r.createErr != nil {
		return r.createErr
	}
	project.ID = 1
	r.projects = append(r.projects, *project)
	return nil
}

func (r *fakeRegistry) Touch(id uint) error {
	r.touchedID = id
	return r.touchErr
}

func (r *fakeRegistry) Delete(id uint) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	r.deletedID = id
	remaining := make([]domain.Project, 0, len(r.projects))
	for _, project := range r.projects {
		if project.ID != id {
			remaining = append(remaining, project)
		}
	}
	r.projects = remaining
	return nil
}

func (r *fakeRegistry) Close() error { return nil }
