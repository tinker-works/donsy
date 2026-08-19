// Package repository contains the tracker repository aggregate.
package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/tinker-works/donsy/internal/domain/id"
	"github.com/tinker-works/donsy/internal/domain/owner"
	"github.com/tinker-works/donsy/internal/domain/slug"
)

// Repository is a repository configured for a project. It is metadata, not a
// checkout; runtime workspaces belong to an adapter.
type Repository struct {
	ID            id.ID       `json:"id"`
	Name          slug.Slug   `json:"name"`
	FullName      string      `json:"full_name,omitempty"`
	URL           string      `json:"url"`
	DefaultBranch string      `json:"default_branch,omitempty"`
	Owner         owner.Owner `json:"owner,omitempty"`
	Private       bool        `json:"private,omitempty"`
}

// New constructs a repository from its name and remote URL. Optional
// arguments are full name and default branch.
func New(name, remote string, details ...string) (Repository, error) {
	parsedName, err := slug.Parse(strings.TrimSpace(name))
	if err != nil {
		return Repository{}, fmt.Errorf("repository name: %w", err)
	}
	value := Repository{ID: id.New(), Name: parsedName, URL: strings.TrimSpace(remote)}
	if len(details) > 0 {
		value.FullName = strings.TrimSpace(details[0])
	}
	if len(details) > 1 {
		value.DefaultBranch = strings.TrimSpace(details[1])
	}
	if err := value.Validate(); err != nil {
		return Repository{}, err
	}
	return value, nil
}

func (value Repository) Validate() error {
	if value.Name.IsZero() {
		return errors.New("repository name cannot be empty")
	}
	if !value.Name.Valid() {
		return errors.New("repository name is not a valid slug")
	}
	if value.URL == "" {
		return errors.New("repository URL cannot be empty")
	}
	remote, err := url.Parse(value.URL)
	if err != nil || remote.Scheme == "" || remote.Host == "" {
		return errors.New("repository URL is invalid")
	}
	if value.DefaultBranch == "-" || strings.ContainsAny(value.DefaultBranch, "\r\n") {
		return errors.New("repository default branch is invalid")
	}
	if value.Owner != (owner.Owner{}) {
		if err := value.Owner.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (value Repository) Valid() bool { return value.Validate() == nil }

func (value Repository) String() string {
	if value.FullName != "" {
		return value.FullName
	}
	return value.Name.String()
}

func (value Repository) MarshalJSON() ([]byte, error) {
	if err := value.Validate(); err != nil {
		return nil, fmt.Errorf("marshal repository: %w", err)
	}
	type repositoryJSON Repository
	return json.Marshal(repositoryJSON(value))
}
