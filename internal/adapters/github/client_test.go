package github

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tinker-works/donsy/internal/application"
)

type fakeCommandRunner struct {
	output []byte
	err    error
	args   []string
}

func (r *fakeCommandRunner) CombinedOutput(
	_ context.Context, _ string, args ...string,
) ([]byte, error) {
	r.args = args
	return r.output, r.err
}

func TestClient_CheckAuth_UsesGhAuthStatus(t *testing.T) {
	runner := &fakeCommandRunner{}
	client := newClient(runner, "github.com")

	if err := client.CheckAuth(context.Background()); err != nil {
		t.Fatal(err)
	}

	expected := []string{"auth", "status", "--hostname", "github.com"}
	if len(runner.args) != len(expected) {
		t.Fatalf("unexpected args: %#v", runner.args)
	}
	for i := range expected {
		if runner.args[i] != expected[i] {
			t.Fatalf("unexpected args: %#v", runner.args)
		}
	}
}

func TestClient_CurrentUser_ReadsAuthenticatedLogin(t *testing.T) {
	runner := &fakeCommandRunner{output: []byte(`{"login":"octocat"}`)}
	client := newClient(runner, "github.com")

	login, err := client.CurrentUser(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if login != "octocat" {
		t.Fatalf("unexpected login: %q", login)
	}

	expected := []string{"api", "--hostname", "github.com", "user"}
	for i := range expected {
		if runner.args[i] != expected[i] {
			t.Fatalf("unexpected args: %#v", runner.args)
		}
	}
}

func TestClient_ListOrganisations_UsesPaginatedGhAPI(t *testing.T) {
	runner := &fakeCommandRunner{output: []byte(`[[{"login":"acme"},{"login":"tools"}]]`)}
	client := newClient(runner, "github.com")

	organisations, err := client.ListOrganisations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(organisations) != 2 || organisations[0].Name != "acme" || organisations[1].Name != "tools" {
		t.Fatalf("unexpected organisations: %#v", organisations)
	}

	expected := []string{"api", "--hostname", "github.com", "--paginate", "--slurp", "user/orgs"}
	for i := range expected {
		if runner.args[i] != expected[i] {
			t.Fatalf("unexpected args: %#v", runner.args)
		}
	}
}

func TestClient_ListUserRepositories_ReadsOwnedAndCollaboratingRepositories(t *testing.T) {
	// Arrange: one repository the account owns, one it only collaborates on.
	runner := &fakeCommandRunner{output: []byte(`[[
		{"name":"dotfiles","full_name":"octocat/dotfiles",
		 "html_url":"https://github.com/octocat/dotfiles",
		 "ssh_url":"git@github.com:octocat/dotfiles.git",
		 "owner":{"login":"octocat"}},
		{"name":"widgets","full_name":"acme/widgets",
		 "html_url":"https://github.com/acme/widgets",
		 "ssh_url":"git@github.com:acme/widgets.git",
		 "owner":{"login":"acme"}}
	]]`)}
	client := newClient(runner, "github.com")

	// Act
	repositories, err := client.ListUserRepositories(context.Background())

	// Assert: the owner is read per repository, so a collaboration is not
	// mistaken for something the account owns.
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 2 {
		t.Fatalf("unexpected repositories: %#v", repositories)
	}
	expected := []application.GitHubRepository{
		{
			Name: "dotfiles", FullName: "octocat/dotfiles",
			HTTPURL: "https://github.com/octocat/dotfiles",
			SSHURL:  "git@github.com:octocat/dotfiles.git", Organisation: "octocat",
		},
		{
			Name: "widgets", FullName: "acme/widgets",
			HTTPURL: "https://github.com/acme/widgets",
			SSHURL:  "git@github.com:acme/widgets.git", Organisation: "acme",
		},
	}
	for i := range expected {
		if repositories[i] != expected[i] {
			t.Fatalf("unexpected repository %d: %#v", i, repositories[i])
		}
	}

	// Assert: the user endpoint is used, because orgs/<login>/repos does not
	// answer for a user account.
	want := []string{
		"api", "--hostname", "github.com", "--paginate", "--slurp",
		"user/repos?affiliation=owner,collaborator",
	}
	if len(runner.args) != len(want) {
		t.Fatalf("unexpected args: %#v", runner.args)
	}
	for i := range want {
		if runner.args[i] != want[i] {
			t.Fatalf("unexpected args: %#v", runner.args)
		}
	}
}

func TestClient_CheckAuth_ShouldSurfaceGhFailure(t *testing.T) {
	// Arrange
	runner := &fakeCommandRunner{err: errors.New("exit status 1")}
	client := newClient(runner, "github.com")

	// Act
	err := client.CheckAuth(context.Background())

	// Assert: the error names the login command a person can act on.
	if err == nil {
		t.Fatal("expected an unauthenticated CLI to fail")
	}
	if !strings.Contains(err.Error(), "gh auth login --hostname github.com") {
		t.Fatalf("error does not say how to authenticate: %v", err)
	}
}

func TestClient_CurrentUser_ShouldFailOnUndecodableResponse(t *testing.T) {
	// Arrange
	runner := &fakeCommandRunner{output: []byte("not json")}
	client := newClient(runner, "github.com")

	// Act
	_, err := client.CurrentUser(context.Background())

	// Assert
	if err == nil {
		t.Fatal("expected an undecodable response to fail")
	}
}

func TestClient_CurrentUser_ShouldFailWhenLoginIsMissing(t *testing.T) {
	// Arrange: valid JSON whose login is blank, so decoding alone cannot
	// catch it.
	runner := &fakeCommandRunner{output: []byte(`{"login":"  "}`)}
	client := newClient(runner, "github.com")

	// Act
	_, err := client.CurrentUser(context.Background())

	// Assert
	if err == nil {
		t.Fatal("expected a missing login to fail")
	}
}

func TestClient_ListRepositories_ShouldRejectEmptyOrganisation(t *testing.T) {
	// Arrange
	runner := &fakeCommandRunner{}
	client := newClient(runner, "github.com")

	// Act
	_, err := client.ListRepositories(context.Background(), "  ")

	// Assert: rejected before shelling out, so gh never runs.
	if err == nil {
		t.Fatal("expected an empty organisation to be rejected")
	}
	if runner.args != nil {
		t.Fatalf("gh was invoked anyway: %#v", runner.args)
	}
}

func TestClient_Run_ShouldWrapOriginalErrorAndGhOutput(t *testing.T) {
	// Arrange
	original := errors.New("exit status 4")
	runner := &fakeCommandRunner{output: []byte("gh: Not Found (HTTP 404)\n"), err: original}
	client := newClient(runner, "github.com")

	// Act
	_, err := client.CurrentUser(context.Background())

	// Assert: the original error stays in the chain so a caller can still
	// reach the exit code, and gh's own message rides along for a human.
	if !errors.Is(err, original) {
		t.Fatalf("original error is not in the chain: %v", err)
	}
	if !strings.Contains(err.Error(), "gh: Not Found (HTTP 404)") ||
		!strings.Contains(err.Error(), original.Error()) {
		t.Fatalf("error is missing gh output or the original error: %v", err)
	}
}

func TestClient_Run_ShouldKeepOriginalErrorWhenGhSaysNothing(t *testing.T) {
	// Arrange
	original := errors.New("exit status 1")
	runner := &fakeCommandRunner{err: original}
	client := newClient(runner, "github.com")

	// Act
	_, err := client.CurrentUser(context.Background())

	// Assert
	if !errors.Is(err, original) {
		t.Fatalf("original error is not in the chain: %v", err)
	}
}

func TestClient_ListRepositories_ReturnsRepositoryDetails(t *testing.T) {
	runner := &fakeCommandRunner{output: []byte(
		`[[{"name":"project","full_name":"acme/project","html_url":"https://github.com/acme/project",
		"ssh_url":"git@github.com:acme/project.git"}]]`,
	)}
	client := newClient(runner, "github.com")

	repositories, err := client.ListRepositories(context.Background(), "acme")
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 1 {
		t.Fatalf("expected one repository, got %d", len(repositories))
	}
	want := application.GitHubRepository{
		Name:         "project",
		FullName:     "acme/project",
		HTTPURL:      "https://github.com/acme/project",
		SSHURL:       "git@github.com:acme/project.git",
		Organisation: "acme",
	}
	if repositories[0] != want {
		t.Fatalf("unexpected repository: %#v", repositories[0])
	}
}

func TestNewClient_ShouldRunGhAgainstGitHubDotCom(t *testing.T) {
	// Arrange: the production constructor is what pins the hostname every command
	// is scoped to, so a client built any other way would query the wrong host.

	// Act
	client := NewClient()

	// Assert
	if client.hostname != "github.com" {
		t.Fatalf("expected github.com, got %q", client.hostname)
	}
	if _, ok := client.runner.(execRunner); !ok {
		t.Fatalf("expected the host runner, got %T", client.runner)
	}
}

func TestExecRunner_CombinedOutput_ShouldReturnBothStreamsTogether(t *testing.T) {
	// Arrange: gh writes its reason to stderr and its answer to stdout, and the
	// error path needs both — which is why the two are combined rather than split.
	runner := execRunner{}

	// Act
	output, err := runner.CombinedOutput(context.Background(),
		"sh", "-c", "echo answer; echo reason >&2")

	// Assert
	if err != nil {
		t.Fatal(err)
	}
	combined := string(output)
	if !strings.Contains(combined, "answer") || !strings.Contains(combined, "reason") {
		t.Fatalf("expected both streams, got %q", combined)
	}
}

func TestExecRunner_CombinedOutput_ShouldHandBackWhatFailedAlongWithTheError(t *testing.T) {
	// Arrange: gh's own message is usually the part a human can act on, so it has
	// to survive the failure.
	runner := execRunner{}

	// Act
	output, err := runner.CombinedOutput(context.Background(),
		"sh", "-c", "echo not logged in >&2; exit 1")

	// Assert
	if err == nil {
		t.Fatal("expected the failure reported")
	}
	if !strings.Contains(string(output), "not logged in") {
		t.Fatalf("expected the reason kept, got %q", output)
	}
}
