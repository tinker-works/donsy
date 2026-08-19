package domain

import "time"

type Project struct {
	ID           uint
	Name         string
	LastOpenedAt time.Time
}
