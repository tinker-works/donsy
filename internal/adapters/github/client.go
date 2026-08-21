package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strings"

	"github.com/tinker-works/donsy/internal/application"
	"github.com/tinker-works/donsy/internal/domain"
)

var _ application.GitHubClient = (*Client)(nil)

type commandRunner interface {
	CombinedOutput(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type Client struct {
	runner   commandRunner
	hostname string
}

func NewClient() *Client {
	return &Client{runner: execRunner{}, hostname: "github.com"}
}

func newClient(runner commandRunner, hostname string) *Client {
	return &Client{runner: runner, hostname: hostname}
}

func (c *Client) CheckAuth(ctx context.Context) error {
	_, err := c.run(ctx, "auth", "status", "--hostname", c.hostname)
	if err != nil {
		return fmt.Errorf(
			"GitHub CLI is not authenticated for %s; run `gh auth login --hostname %s`: %w",
			c.hostname, c.hostname, err,
		)
	}
	return nil
}

func (c *Client) CurrentUser(ctx context.Context) (string, error) {
	output, err := c.run(ctx, "api", "--hostname", c.hostname, "user")
	if err != nil {
		return "", err
	}
	var user struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(output, &user); err != nil {
		return "", fmt.Errorf("decode GitHub user response: %w", err)
	}
	if strings.TrimSpace(user.Login) == "" {
		return "", fmt.Errorf("GitHub user response did not contain a login")
	}
	return user.Login, nil
}

func (c *Client) ListOrganisations(ctx context.Context) ([]domain.Organisation, error) {
	var pages [][]struct {
		Login string `json:"login"`
	}
	if err := c.runJSON(ctx, "user/orgs", &pages); err != nil {
		return nil, err
	}
	organisations := make([]domain.Organisation, 0)
	for _, page := range pages {
		for _, organisation := range page {
			organisations = append(organisations, domain.Organisation{Name: organisation.Login})
		}
	}
	return organisations, nil
}

func (c *Client) ListRepositories(
	ctx context.Context, organisation string,
) ([]application.GitHubRepository, error) {
	organisation = strings.TrimSpace(organisation)
	if organisation == "" {
		return nil, fmt.Errorf("organisation name is required")
	}

	var pages [][]struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
		SSHURL   string `json:"ssh_url"`
	}
	endpoint := "orgs/" + url.PathEscape(organisation) + "/repos"
	if err := c.runJSON(ctx, endpoint, &pages); err != nil {
		return nil, err
	}
	repositories := make([]application.GitHubRepository, 0)
	for _, page := range pages {
		for _, repository := range page {
			repositories = append(repositories, application.GitHubRepository{
				Name:         repository.Name,
				FullName:     repository.FullName,
				HTTPURL:      repository.HTMLURL,
				SSHURL:       repository.SSHURL,
				Organisation: organisation,
			})
		}
	}
	return repositories, nil
}

// ListUserRepositories lists the repositories the authenticated account reaches
// directly: the ones it owns, and the ones it was invited to. Both arrive from
// user/repos because orgs/<login>/repos does not answer for a user account.
func (c *Client) ListUserRepositories(ctx context.Context) ([]application.GitHubRepository, error) {
	var pages [][]struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		HTMLURL  string `json:"html_url"`
		SSHURL   string `json:"ssh_url"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	// gh adds its own page parameter, so the affiliation filter survives --paginate.
	endpoint := "user/repos?affiliation=owner,collaborator"
	if err := c.runJSON(ctx, endpoint, &pages); err != nil {
		return nil, err
	}
	repositories := make([]application.GitHubRepository, 0)
	for _, page := range pages {
		for _, repository := range page {
			repositories = append(repositories, application.GitHubRepository{
				Name:     repository.Name,
				FullName: repository.FullName,
				HTTPURL:  repository.HTMLURL,
				SSHURL:   repository.SSHURL,
				// A repository the account collaborates on is owned by someone
				// else, so the owner is read per repository rather than assumed.
				Organisation: repository.Owner.Login,
			})
		}
	}
	return repositories, nil
}

func (c *Client) runJSON(ctx context.Context, endpoint string, result any) error {
	output, err := c.run(ctx, "api", "--hostname", c.hostname, "--paginate", "--slurp", endpoint)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(output, result); err != nil {
		return fmt.Errorf("decode GitHub API response for %s: %w", endpoint, err)
	}
	return nil
}

func (c *Client) run(ctx context.Context, args ...string) ([]byte, error) {
	output, err := c.runner.CombinedOutput(ctx, "gh", args...)
	if err != nil {
		// The original error stays in the chain so a caller can still reach the exit code;
		// gh's own message is appended because it is usually the part a human can act on.
		message := strings.TrimSpace(string(output))
		if message == "" {
			return nil, fmt.Errorf("run gh %s: %w", strings.Join(args, " "), err)
		}
		return nil, fmt.Errorf("run gh %s: %w: %s", strings.Join(args, " "), err, message)
	}
	return output, nil
}
