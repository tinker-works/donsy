package usecases

import (
	"fmt"
	"strings"

	"github.com/tinker-works/donsy/internal/application"
)

// resolveEpicRepositories decides the repository scope an epic is created
// with. RunEpicAgentUseCase refuses an epic that names no repository and
// nothing can add one afterwards, so an empty request falls back to every
// repository the project configures rather than producing a dead epic.
func resolveEpicRepositories(
	workspace application.Workspace, requested []string,
) ([]string, error) {
	configured, err := workspace.Repositories()
	if err != nil {
		return nil, err
	}
	if len(requested) == 0 {
		if len(configured) == 0 {
			return nil, fmt.Errorf("project configures no repositories to scope the epic to")
		}
		return append([]string(nil), configured...), nil
	}
	allowed := make(map[string]struct{}, len(configured))
	for _, repository := range configured {
		allowed[repository] = struct{}{}
	}
	resolved := make([]string, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, repository := range requested {
		repository = strings.TrimSpace(repository)
		if repository == "" {
			return nil, fmt.Errorf("epic repository is required")
		}
		if _, exists := allowed[repository]; !exists {
			return nil, fmt.Errorf("epic repository %q is not configured for this project", repository)
		}
		if _, exists := seen[repository]; exists {
			return nil, fmt.Errorf("duplicate epic repository %q", repository)
		}
		seen[repository] = struct{}{}
		resolved = append(resolved, repository)
	}
	return resolved, nil
}
