package engine

import "time"

type State string

const (
	Free     State = "free"
	Running  State = "running"
	Finished State = "finished"
	Offline  State = "offline"
	Unknown  State = "unknown"
)

type Machine struct {
	State State
	Since time.Time

	LastReading  time.Time
	ShakingSince time.Time
	QuietSince   time.Time

	Prev      State
	PrevSince time.Time
}

type Transition struct {
	From, To State
	At       time.Time
}
