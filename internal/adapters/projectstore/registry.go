package projectstore

import (
	"gorm.io/gorm"

	"github.com/tinker-works/donsy/internal/application"
)

// Registry satisfies the full composite the daemon wires it in as, so signature
// drift in any embedded port breaks here rather than at the composition site.
var _ application.Registry = (*Registry)(nil)

// Registry stores daemon-wide and agent runtime state in one SQLite database.
type Registry struct {
	db *gorm.DB
}
