package domain

import fixture "forgeflow-eval-fixture"

type Status = fixture.Status

const (
	StatusPending   = fixture.StatusPending
	StatusRunning   = fixture.StatusRunning
	StatusSucceeded = fixture.StatusSucceeded
	StatusFailed    = fixture.StatusFailed
	StatusCancelled = fixture.StatusCancelled
)

func IsTransitionAllowed(from, to Status) bool { return fixture.IsTransitionAllowed(from, to) }
