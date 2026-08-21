package clock

import (
	"time"

	"github.com/tinker-works/donsy/internal/application"
)

var _ application.Clock = Real{}

type Real struct{}

func (Real) Now() time.Time {
	return time.Now().UTC()
}
