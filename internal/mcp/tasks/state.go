package tasks

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrInvalidTransition marks a status change the spec forbids: any change
// out of a terminal status.
var ErrInvalidTransition = errors.New("invalid task status transition")

// CanTransition reports whether a task may move from one status to another:
// working and input_required move to each other and to any terminal status;
// a terminal status never changes. Staying put is always allowed.
func CanTransition(from, to Status) bool {
	return from == to || !from.IsTerminal()
}

// Transition is what one observation of a task changed.
type Transition struct {
	// First is true when the task had not been observed before.
	First bool
	From  Status
	To    Status
}

// Changed reports whether the observation moved the task to a new status,
// counting the first observation as a change.
func (t Transition) Changed() bool {
	return t.First || t.From != t.To
}

// Tracker remembers the latest view of every task a client has seen and
// checks each new view against the state machine. Safe for concurrent use.
type Tracker struct {
	mu    sync.Mutex
	tasks map[string]Task
}

// NewTracker returns an empty tracker.
func NewTracker() *Tracker {
	return &Tracker{tasks: map[string]Task{}}
}

// Observe records t as the latest view of its task and returns the
// transition it represents. A transition the spec forbids is recorded all
// the same, since the server's view is what the user must see, and
// reported as ErrInvalidTransition.
func (tr *Tracker) Observe(t *Task) (Transition, error) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	prev, seen := tr.tasks[t.ID]
	tr.tasks[t.ID] = *t
	if !seen {
		return Transition{First: true, To: t.Status}, nil
	}
	tn := Transition{From: prev.Status, To: t.Status}
	if !CanTransition(prev.Status, t.Status) {
		return tn, fmt.Errorf("%w: %s → %s", ErrInvalidTransition, prev.Status, t.Status)
	}
	return tn, nil
}

// Get returns the latest view of a task, if it was observed.
func (tr *Tracker) Get(id string) (Task, bool) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	t, ok := tr.tasks[id]
	return t, ok
}

// Snapshot returns every observed task, oldest first.
func (tr *Tracker) Snapshot() []Task {
	tr.mu.Lock()
	out := make([]Task, 0, len(tr.tasks))
	for id := range tr.tasks {
		out = append(out, tr.tasks[id])
	}
	tr.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out
}
