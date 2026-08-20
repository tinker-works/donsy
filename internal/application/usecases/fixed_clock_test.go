package usecases

import "time"

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time { return c.now }
