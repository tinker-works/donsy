package usecases

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tinker-works/donsy/internal/domain"
	epicpkg "github.com/tinker-works/donsy/internal/domain/epic"
)

// The tests below cover the failure halves of the use cases: a store that
// cannot be read or written, a record the command names but the aggregate does
// not hold, and answers the domain refuses. Every one is a state the UI has to
// report rather than crash on.

var errStore = errors.New("the store could not be reached")

// oneEpic is the smallest aggregate the commands below act on.
func oneEpic() epicpkg.Epic {
	return epicpkg.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner",
		State: epicpkg.EpicStateConcept,
		Issues: []epicpkg.Issue{
			{ID: "root", Title: "Root", State: epicpkg.IssueStateOpen},
		},
	}
}

// broken is a workspace whose reads and writes both fail.
func broken() *fakeFactory {
	return &fakeFactory{workspace: &fakeWorkspace{
		detail: oneEpic(), updateErr: errStore, readEpicErr: errStore,
		listEpicsErr: errStore, createEpicErr: errStore,
	}}
}

// healthy is a workspace holding one epic and nothing else.
func healthy() *fakeFactory {
	return &fakeFactory{workspace: &fakeWorkspace{detail: oneEpic()}}
}

func project() domain.Project {
	return domain.Project{ID: 1, Name: "acme"}
}

func TestOpenProjectUseCase_ShouldStampTheProjectAsMostRecentlyOpened(t *testing.T) {
	// Arrange: the project list is ordered by that stamp, so this is what the
	// next start resumes on.
	registry := &fakeRegistry{projects: []domain.Project{project()}}
	useCase := &OpenProjectUseCase{registry: registry}

	// Act
	err := useCase.Handle(OpenProjectCommand{Project: project()})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if registry.touchedID != project().ID {
		t.Fatalf("expected the project touched, got %d", registry.touchedID)
	}
}

func TestOpenProjectUseCase_ShouldSurfaceARegistryFailure(t *testing.T) {
	// Arrange
	registry := &fakeRegistry{touchErr: errStore}
	useCase := &OpenProjectUseCase{registry: registry}

	// Act & Assert
	if err := useCase.Handle(OpenProjectCommand{Project: project()}); err == nil {
		t.Fatal("expected the registry failure surfaced")
	}
}

func TestAddCommentUseCase_ShouldRefuseAnUnknownTarget(t *testing.T) {
	// Arrange: only an issue and a pull request can carry a comment.
	useCase := &AddCommentUseCase{factory: healthy()}

	// Act
	err := useCase.Handle(AddCommentCommand{
		Project: project(), EpicID: "aggregate", TargetID: "root",
		Target: CommentTarget("nonsense"), Author: "me", Body: "hello",
	})

	// Assert
	if err == nil {
		t.Fatal("expected an unknown target to be refused")
	}
	if !strings.Contains(err.Error(), "nonsense") {
		t.Fatalf("expected the target named, got %v", err)
	}
}

func TestAddCommentUseCase_ShouldRefuseAnAuthorlessComment(t *testing.T) {
	// Arrange: the domain requires an author, and an unauthenticated session has
	// none to offer.
	useCase := &AddCommentUseCase{factory: healthy()}

	// Act
	err := useCase.Handle(AddCommentCommand{
		Project: project(), EpicID: "aggregate", TargetID: "root",
		Target: IssueCommentTarget, Author: "  ", Body: "hello",
	})

	// Assert
	if err == nil {
		t.Fatal("expected the blank author to be refused")
	}
}

func TestAddCommentUseCase_ShouldSurfaceAWriteFailure(t *testing.T) {
	// Arrange
	useCase := &AddCommentUseCase{factory: broken()}

	// Act & Assert
	err := useCase.Handle(AddCommentCommand{
		Project: project(), EpicID: "aggregate", TargetID: "root",
		Target: IssueCommentTarget, Author: "me", Body: "hello",
	})
	if err == nil {
		t.Fatal("expected the write failure surfaced")
	}
}

func TestCreateEpicUseCase_ShouldSlugTheBranchPrefixItStores(t *testing.T) {
	// Arrange: the prefix goes into a Git ref verbatim, so what is stored is
	// already what a branch can be called.
	factory := healthy()
	factory.workspace.repositories = []string{"acme/api"}
	useCase := &CreateEpicUseCase{factory: factory}

	// Act
	_, err := useCase.Handle(CreateEpicCommand{
		Project: project(), Title: "Epic", Assignee: "owner", BranchPrefix: "JIRA 123/../x",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	stored := factory.workspace.createdEpic
	if stored == nil {
		t.Fatal("expected the epic written")
	}
	if stored.BranchPrefix != domain.Slug("JIRA 123/../x") {
		t.Fatalf("expected the prefix slugged, got %q", stored.BranchPrefix)
	}
}

func TestCreateEpicUseCase_ShouldRefuseAnEpicItCannotScope(t *testing.T) {
	// Arrange: RunEpicAgentUseCase refuses an epic that names no repository and
	// nothing can add one afterwards, so an unscopable epic is a dead epic.
	useCase := &CreateEpicUseCase{factory: healthy()}

	// Act
	_, err := useCase.Handle(CreateEpicCommand{
		Project: project(), Title: "Epic", Assignee: "owner",
	})

	// Assert
	if err == nil {
		t.Fatal("expected an epic with no repository scope to be refused")
	}
}

func TestCreateEpicUseCase_ShouldRefuseARepositoryTheProjectDoesNotConfigure(t *testing.T) {
	// Arrange
	factory := healthy()
	factory.workspace.repositories = []string{"acme/api"}
	useCase := &CreateEpicUseCase{factory: factory}

	// Act & Assert
	for _, requested := range [][]string{
		{"someone-else/tool"},
		{"acme/api", "acme/api"},
		{"  "},
	} {
		_, err := useCase.Handle(CreateEpicCommand{
			Project: project(), Title: "Epic", Assignee: "owner", Repositories: requested,
		})
		if err == nil {
			t.Fatalf("expected %v to be refused", requested)
		}
	}
}

func TestCreateEpicUseCase_ShouldSurfaceAFailedWrite(t *testing.T) {
	// Arrange
	factory := broken()
	factory.workspace.repositories = []string{"acme/api"}
	useCase := &CreateEpicUseCase{factory: factory}

	// Act & Assert
	_, err := useCase.Handle(CreateEpicCommand{
		Project: project(), Title: "Epic", Assignee: "owner",
	})
	if err == nil {
		t.Fatal("expected the write failure surfaced")
	}
}

func TestCreateIssueUseCase_ShouldFallBackToTheRootAsParent(t *testing.T) {
	// Arrange: an issue created from a screen with no cursor on a row still has to
	// land somewhere.
	factory := healthy()
	useCase := &CreateIssueUseCase{factory: factory}

	// Act
	_, err := useCase.Handle(CreateIssueCommand{
		Project: project(), EpicID: "aggregate",
		Title: "Child", Repository: "acme/api",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := factory.workspace.ReadEpic("aggregate")
	for _, issue := range stored.Issues {
		if issue.Title == "Child" && issue.ParentID == "root" {
			return
		}
	}
	t.Fatalf("expected the issue under the root, got %+v", stored.Issues)
}

func TestCreateIssueUseCase_ShouldReportAnEpicWithNoTree(t *testing.T) {
	// Arrange: an epic nobody drafted has no root to parent an issue to.
	factory := &fakeFactory{workspace: &fakeWorkspace{detail: epicpkg.Epic{
		ID: "aggregate", Title: "Aggregate", Assignee: "owner",
		State: epicpkg.EpicStateConcept,
	}}}
	useCase := &CreateIssueUseCase{factory: factory}

	// Act & Assert
	_, err := useCase.Handle(CreateIssueCommand{
		Project: project(), EpicID: "aggregate", Title: "Child", Repository: "acme/api",
	})
	if err == nil {
		t.Fatal("expected the missing root to be reported")
	}
}

func TestCreateIssueUseCase_ShouldRefuseATitlelessIssue(t *testing.T) {
	// Arrange
	useCase := &CreateIssueUseCase{factory: healthy()}

	// Act & Assert
	_, err := useCase.Handle(CreateIssueCommand{
		Project: project(), EpicID: "aggregate", Title: "  ", Repository: "acme/api",
	})
	if err == nil {
		t.Fatal("expected the blank title to be refused")
	}
}

func TestCreatePullRequestUseCase_ShouldRefuseARecordWithNoTitle(t *testing.T) {
	// Arrange
	useCase := &CreatePullRequestUseCase{factory: healthy()}

	// Act & Assert
	err := useCase.Handle(CreatePullRequestCommand{
		Project: project(), EpicID: "aggregate", IssueID: "root",
		Title: "  ", Repository: "acme/api", Head: "head", Base: "main",
	})
	if err == nil {
		t.Fatal("expected the blank title to be refused")
	}
}

func TestCreatePullRequestUseCase_ShouldReportAnUnknownIssue(t *testing.T) {
	// Arrange
	useCase := &CreatePullRequestUseCase{factory: healthy()}

	// Act & Assert
	err := useCase.Handle(CreatePullRequestCommand{
		Project: project(), EpicID: "aggregate", IssueID: "ghost",
		Title: "PR", Repository: "acme/api", Head: "head", Base: "main",
	})
	if err == nil {
		t.Fatal("expected the unknown issue to be reported")
	}
}

func TestCloseEpicUseCase_ShouldSurfaceAFailedWrite(t *testing.T) {
	// Arrange
	useCase := &CloseEpicUseCase{factory: broken()}

	// Act & Assert
	err := useCase.Handle(context.Background(),
		CloseEpicCommand{Project: project(), EpicID: "aggregate"})
	if err == nil {
		t.Fatal("expected the write failure surfaced")
	}
}

func TestCloseIssueUseCase_ShouldReportAnUnknownIssue(t *testing.T) {
	// Arrange
	useCase := &CloseIssueUseCase{factory: healthy()}

	// Act & Assert
	err := useCase.Handle(context.Background(), CloseIssueCommand{
		Project: project(), EpicID: "aggregate", IssueID: "ghost",
	})
	if err == nil {
		t.Fatal("expected the unknown issue to be reported")
	}
}

func TestCloseIssueUseCase_ShouldSurfaceAFailedWrite(t *testing.T) {
	// Arrange
	useCase := &CloseIssueUseCase{factory: broken()}

	// Act & Assert
	err := useCase.Handle(context.Background(), CloseIssueCommand{
		Project: project(), EpicID: "aggregate", IssueID: "root",
	})
	if err == nil {
		t.Fatal("expected the write failure surfaced")
	}
}

func TestTransitionEpicStateUseCase_ShouldSurfaceAFailedWrite(t *testing.T) {
	// Arrange
	useCase := &TransitionEpicStateUseCase{factory: broken()}

	// Act & Assert
	err := useCase.Handle(TransitionEpicStateCommand{
		Project: project(), EpicID: "aggregate", State: epicpkg.EpicStateRefine,
	})
	if err == nil {
		t.Fatal("expected the write failure surfaced")
	}
}

func TestSetBranchPrefixUseCase_ShouldSlugWhateverTheUserTyped(t *testing.T) {
	// Arrange: callers pass free text; the aggregate slugs it.
	factory := healthy()
	useCase := &SetBranchPrefixUseCase{factory: factory}

	// Act
	err := useCase.Handle(SetBranchPrefixCommand{
		Project: project(), EpicID: "aggregate", Prefix: "JIRA 123",
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := factory.workspace.ReadEpic("aggregate")
	if stored.BranchPrefix != domain.Slug("JIRA 123") {
		t.Fatalf("expected the prefix slugged, got %q", stored.BranchPrefix)
	}
}

func TestSetBranchPrefixUseCase_ShouldRefuseItOnceBranchesAreCut(t *testing.T) {
	// Arrange: the branches are already on the remote under their old names, and
	// renaming only the epic's copy would leave the two disagreeing.
	detail := oneEpic()
	detail.PullRequests = []epicpkg.PullRequest{{
		ID: "pr", IssueID: "root", Title: "PR", Status: epicpkg.PullRequestOpen,
	}}
	factory := &fakeFactory{workspace: &fakeWorkspace{detail: detail}}
	useCase := &SetBranchPrefixUseCase{factory: factory}

	// Act & Assert
	err := useCase.Handle(SetBranchPrefixCommand{
		Project: project(), EpicID: "aggregate", Prefix: "jira-9",
	})
	if err == nil {
		t.Fatal("expected the prefix to be fixed once branches exist")
	}
}

func TestListEpicsUseCase_ShouldSurfaceAReadFailure(t *testing.T) {
	// Arrange
	useCase := &ListEpicsUseCase{factory: broken()}

	// Act & Assert
	if _, err := useCase.Handle(ListEpicsQuery{Project: project()}); err == nil {
		t.Fatal("expected the read failure surfaced")
	}
}

func TestGetEpicUseCase_ShouldSurfaceAReadFailure(t *testing.T) {
	// Arrange
	useCase := &GetEpicUseCase{factory: broken()}

	// Act & Assert
	if _, err := useCase.Handle(GetEpicQuery{
		Project: project(), EpicID: "aggregate",
	}); err == nil {
		t.Fatal("expected the read failure surfaced")
	}
}

func TestListProjectsUseCase_ShouldSurfaceARegistryFailure(t *testing.T) {
	// Arrange
	useCase := &ListProjectsUseCase{registry: &fakeRegistry{listErr: errStore}}

	// Act & Assert
	if _, err := useCase.Handle(ListProjectsQuery{}); err == nil {
		t.Fatal("expected the registry failure surfaced")
	}
}

func TestGetAgentSettingsUseCase_ShouldReadTheProjectsSettings(t *testing.T) {
	// Arrange
	factory := healthy()
	factory.workspace.agentSettings = testAgentSettings()
	useCase := &GetAgentSettingsUseCase{factory: factory}

	// Act
	settings, err := useCase.Handle(GetAgentSettingsQuery{Project: project()})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if settings.ConfiguredRoles() == 0 {
		t.Fatal("expected the assigned roles read back")
	}
}

func TestSetAgentRoleUseCase_ShouldSurfaceAFailedWrite(t *testing.T) {
	// Arrange
	useCase := &SetAgentRoleUseCase{factory: broken()}

	// Act & Assert
	err := useCase.Handle(SetAgentRoleCommand{
		Project: project(), Role: "coding", Agent: "opencode", Variant: "high",
	})
	if err == nil {
		t.Fatal("expected the write failure surfaced")
	}
}

func TestUpdateProjectRepositoriesUseCase_ShouldRewriteTheLinkedSet(t *testing.T) {
	// Arrange
	factory := healthy()
	useCase := &UpdateProjectRepositoriesUseCase{factory: factory}

	// Act
	err := useCase.Handle(UpdateProjectRepositoriesCommand{
		Project: project(), Repositories: []string{"acme/api", "acme/web"},
	})

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	linked, _ := factory.workspace.Repositories()
	if len(linked) != 2 {
		t.Fatalf("expected both repositories linked, got %v", linked)
	}
}

func TestReadRunOutputUseCase_Sizes_ShouldSampleEveryReadableRun(t *testing.T) {
	// Arrange: a run whose size cannot be read is simply absent — the sparkline
	// it feeds is decoration, and decoration must not surface errors.
	output := &fakeRunOutput{
		sizes:   map[string]int64{"run-1": 4096},
		sizeErr: map[string]error{"run-2": errStore},
	}
	useCase := &ReadRunOutputUseCase{output: output, builder: fakeCommandBuilder{}}

	// Act
	sizes := useCase.Sizes([]string{"run-1", "run-2"})

	// Assert
	if got := sizes["run-1"]; got != 4096 {
		t.Fatalf("expected the readable run sampled, got %d", got)
	}
	if _, present := sizes["run-2"]; present {
		t.Fatalf("expected the unreadable run absent, got %v", sizes)
	}
}

func TestReadRunOutputUseCase_Sizes_ShouldAnswerEmptyWithoutAReader(t *testing.T) {
	// Arrange: the activity poll runs whether or not an agent runtime is wired.
	useCase := &ReadRunOutputUseCase{}

	// Act
	sizes := useCase.Sizes([]string{"run-1"})

	// Assert
	if len(sizes) != 0 {
		t.Fatalf("expected no samples, got %v", sizes)
	}
}

func TestListProjectSummariesUseCase_ShouldMarkAProjectItCannotRead(t *testing.T) {
	// Arrange: the row renders as unreadable rather than as empty.
	registry := &fakeRegistry{projects: []domain.Project{project()}}
	factory := broken()
	useCase := &ListProjectSummariesUseCase{
		registry: registry, agentRegistry: &fakeAgentRegistry{}, factory: factory,
	}

	// Act
	summaries, err := useCase.Handle()

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected one summary, got %v", summaries)
	}
	if summaries[0].Err == nil {
		t.Fatal("expected the unreadable project marked")
	}
}

func TestListProjectSummariesUseCase_ShouldSurfaceARegistryFailure(t *testing.T) {
	// Arrange
	useCase := &ListProjectSummariesUseCase{
		registry:      &fakeRegistry{listErr: errStore},
		agentRegistry: &fakeAgentRegistry{}, factory: healthy(),
	}

	// Act & Assert
	if _, err := useCase.Handle(); err == nil {
		t.Fatal("expected the registry failure surfaced")
	}
}
