// Package setup contains the persisted setup used to initialize a project.
package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/tinker-works/donsy/internal/domain/id"
	"github.com/tinker-works/donsy/internal/domain/slug"
)

// Setup describes the project instructions and branch defaults used by the
// worker. It contains no execution or adapter state.
type Setup struct {
	ID            id.ID     `json:"id"`
	Name          slug.Slug `json:"name"`
	Description   string    `json:"description,omitempty"`
	Instructions  string    `json:"instructions,omitempty"`
	BaseBranch    string    `json:"base_branch,omitempty"`
	DefaultBranch string    `json:"default_branch,omitempty"`
}

func New(name string, details ...string) (Setup, error) {
	parsedName, err := slug.Parse(strings.TrimSpace(name))
	if err != nil {
		return Setup{}, fmt.Errorf("setup name: %w", err)
	}
	value := Setup{ID: id.New(), Name: parsedName}
	if len(details) > 0 {
		value.Description = details[0]
	}
	if len(details) > 1 {
		value.Instructions = details[1]
	}
	if len(details) > 2 {
		value.BaseBranch = details[2]
	}
	if len(details) > 3 {
		value.DefaultBranch = details[3]
	}
	if err := value.Validate(); err != nil {
		return Setup{}, err
	}
	return value, nil
}

func (value Setup) Validate() error {
	if value.Name.IsZero() || !value.Name.Valid() {
		return errors.New("setup name is not a valid slug")
	}
	for _, branch := range []string{value.BaseBranch, value.DefaultBranch} {
		if strings.ContainsAny(branch, "\r\n") {
			return errors.New("setup branch contains a newline")
		}
	}
	return nil
}

func (value Setup) Valid() bool { return value.Validate() == nil }

func (value Setup) MarshalJSON() ([]byte, error) {
	if err := value.Validate(); err != nil {
		return nil, fmt.Errorf("marshal setup: %w", err)
	}
	type setupJSON Setup
	return json.Marshal(setupJSON(value))
}
