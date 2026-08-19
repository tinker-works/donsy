// Package owner contains repository and project owner identities.
package owner

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/tinker-works/donsy/internal/domain/id"
)

// Owner identifies a user or organisation that owns a resource.
type Owner struct {
	ID    id.ID  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

func New(login string, details ...string) (Owner, error) {
	value := Owner{ID: id.New(), Login: strings.TrimSpace(login)}
	if len(details) > 0 {
		value.Name = strings.TrimSpace(details[0])
	}
	if len(details) > 1 {
		value.Email = strings.TrimSpace(details[1])
	}
	if err := value.Validate(); err != nil {
		return Owner{}, err
	}
	return value, nil
}

func (value Owner) Validate() error {
	if value.ID.IsZero() && strings.TrimSpace(value.Login) == "" {
		return errors.New("owner requires an id or login")
	}
	if strings.TrimSpace(value.Login) == "" {
		return errors.New("owner login cannot be empty")
	}
	return nil
}

func (value Owner) Valid() bool { return value.Validate() == nil }

func (value Owner) String() string { return value.Login }

func (value Owner) MarshalJSON() ([]byte, error) {
	if err := value.Validate(); err != nil {
		return nil, fmt.Errorf("marshal owner: %w", err)
	}
	type ownerJSON Owner
	return json.Marshal(ownerJSON(value))
}
