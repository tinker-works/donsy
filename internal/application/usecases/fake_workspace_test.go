package usecases

import (
	"fmt"
	"sync"

	"github.com/tinker-works/donsy/internal/domain/agent"
	"github.com/tinker-works/donsy/internal/domain/epic"
)

// fakeWorkspace mirrors what workspace.Repository does with its own lock: every
// call is serialized, because concurrent rounds in one project all reach the
// same handle and the epic they read-modify-write is shared state.
type fakeWorkspace struct {
	mu                 sync.Mutex
	detail             epic.Epic
	createdEpic        *epic.Epic
	updatedEpicID      string
	listEpicsErr       error
	readEpicErr        error
	createEpicErr      error
	updateErr          error
	agentSettings      agent.AgentSettings
	repositories       []string
	repositorySettings map[string]agent.RepositorySettings
	files              map[string]string
	readFileErr        error
}

func (w *fakeWorkspace) ListEpics() ([]epic.Epic, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return []epic.Epic{copyEpic(w.detail)}, w.listEpicsErr
}

func (w *fakeWorkspace) ReadEpic(string) (epic.Epic, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return copyEpic(w.detail), w.readEpicErr
}

// copyEpic gives every reader its own slices.
//
// Returning the epic by value is not enough: the struct's slice headers point
// at the same backing arrays, so a caller ranging over the issues it read would
// be reading memory a concurrent round is transitioning through UpdateEpic. The
// real workspace deserializes each read from the store and so hands back
// independent data; without this the fake would be the only place that shares.
func copyEpic(detail epic.Epic) epic.Epic {
	detail.Repositories = append([]string(nil), detail.Repositories...)
	detail.Issues = append([]epic.Issue(nil), detail.Issues...)
	for index := range detail.Issues {
		detail.Issues[index].Comments = append(
			[]epic.Comment(nil), detail.Issues[index].Comments...,
		)
	}
	detail.PullRequests = append([]epic.PullRequest(nil), detail.PullRequests...)
	return detail
}

func (w *fakeWorkspace) AgentSettings() (agent.AgentSettings, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.agentSettings, nil
}

func (w *fakeWorkspace) UpdateAgentSettings(change func(*agent.AgentSettings) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.updateErr != nil {
		return w.updateErr
	}
	return change(&w.agentSettings)
}

func (w *fakeWorkspace) UpdateRepositorySettings(
	string, func(*agent.RepositorySettings) error,
) error {
	return nil
}

func (w *fakeWorkspace) RepositorySettings(repository string) (agent.RepositorySettings, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.repositorySettings[repository], nil
}

func (w *fakeWorkspace) ReadFile(path string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.readFileErr != nil {
		return "", w.readFileErr
	}
	contents, ok := w.files[path]
	if !ok {
		return "", fmt.Errorf("fake workspace has no file %q", path)
	}
	return contents, nil
}

func (w *fakeWorkspace) WriteFile(string, string) error { return nil }

func (w *fakeWorkspace) Repositories() ([]string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.repositories, nil
}

func (w *fakeWorkspace) UpdateRepositories(repositories []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.repositories = append([]string(nil), repositories...)
	return nil
}

func (w *fakeWorkspace) CreateEpic(detail epic.Epic) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.createEpicErr != nil {
		return w.createEpicErr
	}
	w.createdEpic = &detail
	return nil
}

func (w *fakeWorkspace) UpdateEpic(epicID string, change func(*epic.Epic) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.updateErr != nil {
		return w.updateErr
	}
	w.updatedEpicID = epicID
	if err := change(&w.detail); err != nil {
		return err
	}
	return nil
}
