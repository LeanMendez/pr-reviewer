package pipeline

import "github.com/LeanMendez/pr-review/internal/config"

// EventKind is what happened in a review.
type EventKind int

const (
	StageStarted EventKind = iota + 1
	StageDone
	StageSkipped
	AgentStarted
	AgentActivity
	AgentDone
	AgentFailed
)

// Event is one progress update of a review. Agent events carry the agent
// name, its config role and model; StageStarted and StageDone of the
// validation stage carry the number of candidates.
type Event struct {
	Kind       EventKind
	Stage      Stage
	Agent      string
	Role       config.Role
	Model      config.Model
	Text       string
	Candidates int
	Err        error
}
