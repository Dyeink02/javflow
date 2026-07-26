package crawlexecution

import "context"

// state_machine.go owns the minimal generic phase runner used by the Go crawl
// execution plan.
//
// Ownership summary:
// 1) provide the generic state-machine executor used by crawl execution plans
// 2) centralize transition and finish callbacks across ordered phases
// 3) keep generic machine mechanics separate from crawl-domain phase logic
//
// File map for maintainers:
// 1) generic machine transition/state DTOs
// 2) machine constructor and run loop
// 3) transition callback and finish hook helpers

// Transition describes one explicit edge in the generic execution machine.
type Transition struct {
	From    string
	To      string
	Label   string
	Skipped bool
}

// MachineState is one phase-state node in the generic execution machine.
type MachineState struct {
	Key         string
	Label       string
	Execute     func(ctx context.Context) error
	ShouldSkip  func() bool
	NextKey     string
	ResolveNext func() string
}

// MachineOptions and StateMachine form the generic non-crawl-specific phase
// runner used by the crawl execution package.
type MachineOptions struct {
	InitialState string
	States       []MachineState
	OnTransition func(transition Transition) error
	OnFinished   func() error
}

type StateMachine struct {
	options     MachineOptions
	statesByKey map[string]MachineState
	orderedKeys []string
	currentState string
}

func NewStateMachine(options MachineOptions) (*StateMachine, error) {
	statesByKey := make(map[string]MachineState, len(options.States))
	orderedKeys := make([]string, 0, len(options.States))

	for _, state := range options.States {
		statesByKey[state.Key] = state
		orderedKeys = append(orderedKeys, state.Key)
	}

	return &StateMachine{
		options:      options,
		statesByKey:  statesByKey,
		orderedKeys:  orderedKeys,
		currentState: "idle",
	}, nil
}

func (m *StateMachine) CurrentState() string {
	if m == nil {
		return "idle"
	}
	return m.currentState
}

// Run executes the plan-level state machine. Phase semantics remain outside
// this generic runner.
func (m *StateMachine) Run(ctx context.Context) error {
	if m == nil {
		return nil
	}

	nextStateKey := m.options.InitialState
	previousState := "idle"

	for nextStateKey != "" {
		if err := ctx.Err(); err != nil {
			return err
		}

		state, ok := m.statesByKey[nextStateKey]
		if !ok {
			return &UnknownStateError{Key: nextStateKey}
		}

		skipped := false
		if state.ShouldSkip != nil {
			skipped = state.ShouldSkip()
		}

		m.currentState = state.Key
		if m.options.OnTransition != nil {
			if err := m.options.OnTransition(Transition{
				From:    previousState,
				To:      state.Key,
				Label:   state.Label,
				Skipped: skipped,
			}); err != nil {
				return err
			}
		}

		if !skipped && state.Execute != nil {
			if err := state.Execute(ctx); err != nil {
				return err
			}
		}

		previousState = state.Key
		nextStateKey = m.resolveNextState(state)
	}

	m.currentState = "finished"
	if m.options.OnTransition != nil {
		if err := m.options.OnTransition(Transition{
			From:    previousState,
			To:      "finished",
			Label:   "finished",
			Skipped: false,
		}); err != nil {
			return err
		}
	}
	if m.options.OnFinished != nil {
		return m.options.OnFinished()
	}
	return nil
}

func (m *StateMachine) resolveNextState(state MachineState) string {
	if state.ResolveNext != nil {
		return state.ResolveNext()
	}
	if state.NextKey != "" {
		return state.NextKey
	}

	currentIndex := -1
	for index, key := range m.orderedKeys {
		if key == state.Key {
			currentIndex = index
			break
		}
	}
	if currentIndex == -1 || currentIndex >= len(m.orderedKeys)-1 {
		return ""
	}
	return m.orderedKeys[currentIndex+1]
}

// UnknownStateError is returned when plan construction and machine transitions
// disagree about an expected phase key.
type UnknownStateError struct {
	Key string
}

func (e *UnknownStateError) Error() string {
	return "unknown run state: " + e.Key
}
