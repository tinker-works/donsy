// Package organisation contains the organisation aggregate used by projects
// and repository ownership.
package organisation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/tinker-works/donsy/internal/domain/id"
)

// Organisation identifies an organisation available to the host.
type Organisation struct {
	ID     id.ID  `json:"id"`
	Login  string `json:"login"`
	Name   string `json:"name,omitempty"`
	URL    string `json:"url,omitempty"`
	Avatar string `json:"avatar,omitempty"`
}

// New constructs an organisation from its stable login. Optional arguments
// are name, URL, and avatar URL in that order.
func New(login string, details ...string) (Organisation, error) {
	organisation := Organisation{ID: id.New(), Login: strings.TrimSpace(login)}
	if len(details) > 0 {
		organisation.Name = strings.TrimSpace(details[0])
	}
	if len(details) > 1 {
		organisation.URL = strings.TrimSpace(details[1])
	}
	if len(details) > 2 {
		organisation.Avatar = strings.TrimSpace(details[2])
	}
	if err := organisation.Validate(); err != nil {
		return Organisation{}, err
	}
	return organisation, nil
}

func (value Organisation) Validate() error {
	if value.ID.IsZero() && strings.TrimSpace(value.Login) == "" {
		return errors.New("organisation requires an id or login")
	}
	if strings.TrimSpace(value.Login) == "" {
		return errors.New("organisation login cannot be empty")
	}
	return nil
}

func (value Organisation) Valid() bool { return value.Validate() == nil }

func (value Organisation) String() string { return value.Login }

func (value Organisation) MarshalJSON() ([]byte, error) {
	if err := value.Validate(); err != nil {
		return nil, fmt.Errorf("marshal organisation: %w", err)
	}
	type organisationJSON Organisation
	return json.Marshal(organisationJSON(value))
}
