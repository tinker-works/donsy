// Package project contains the project aggregate and its tracker metadata.
package project

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tinker-works/donsy/internal/domain/id"
	"github.com/tinker-works/donsy/internal/domain/organisation"
	"github.com/tinker-works/donsy/internal/domain/owner"
	"github.com/tinker-works/donsy/internal/domain/repository"
	"github.com/tinker-works/donsy/internal/domain/setup"
	"github.com/tinker-works/donsy/internal/domain/slug"
)

// Project is the root tracker aggregate. Its child records are deliberately
// values so callers cannot mutate a project without going through validation.
type Project struct {
	ID           id.ID                     `json:"id"`
	Name         slug.Slug                 `json:"name"`
	Description  string                    `json:"description,omitempty"`
	Owner        owner.Owner               `json:"owner,omitempty"`
	Organisation organisation.Organisation `json:"organisation,omitempty"`
	Setup        setup.Setup               `json:"setup,omitempty"`
	Repositories []repository.Repository   `json:"repositories,omitempty"`
}

// New constructs a project. Optional details are description and setup.
func New(name string, details ...string) (Project, error) {
	parsedName, err := slug.Parse(strings.TrimSpace(name))
	if err != nil {
		return Project{}, fmt.Errorf("project name: %w", err)
	}
	value := Project{ID: id.New(), Name: parsedName}
	if len(details) > 0 {
		value.Description = details[0]
	}
	if err := value.Validate(); err != nil {
		return Project{}, err
	}
	return value, nil
}

func (value Project) Validate() error {
	if value.Name.IsZero() || !value.Name.Valid() {
		return errors.New("project name is not a valid slug")
	}
	if value.Owner.Login != "" || !value.Owner.ID.IsZero() {
		if err := value.Owner.Validate(); err != nil {
			return err
		}
	}
	if value.Organisation.Login != "" || !value.Organisation.ID.IsZero() {
		if err := value.Organisation.Validate(); err != nil {
			return err
		}
	}
	if value.Setup.Name != "" {
		if err := value.Setup.Validate(); err != nil {
			return err
		}
	}
	for index, item := range value.Repositories {
		if err := item.Validate(); err != nil {
			return fmt.Errorf("repository %d: %w", index, err)
		}
	}
	return nil
}

func (value Project) Valid() bool { return value.Validate() == nil }

func (value *Project) AddRepository(item repository.Repository) error {
	if value == nil {
		return errors.New("project is nil")
	}
	if err := item.Validate(); err != nil {
		return err
	}
	for _, existing := range value.Repositories {
		if existing.ID != "" && existing.ID == item.ID || existing.Name == item.Name {
			return fmt.Errorf("repository %q already belongs to project", item.Name)
		}
	}
	value.Repositories = append(value.Repositories, item)
	return nil
}

func (value *Project) RemoveRepository(name slug.Slug) bool {
	if value == nil {
		return false
	}
	for index, item := range value.Repositories {
		if item.Name == name {
			value.Repositories = append(value.Repositories[:index], value.Repositories[index+1:]...)
			return true
		}
	}
	return false
}
