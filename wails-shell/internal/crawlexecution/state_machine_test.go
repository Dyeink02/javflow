package crawlexecution

import (
	"context"
	"reflect"
	"testing"
)

func TestStateMachineRunsAndSkipsState(t *testing.T) {
	steps := make([]string, 0)

	machine, err := NewStateMachine(MachineOptions{
		InitialState: "boot",
		States: []MachineState{
			{
				Key:   "boot",
				Label: "启动准备",
				Execute: func(ctx context.Context) error {
					steps = append(steps, "execute:boot")
					return nil
				},
				NextKey: "queue_setup",
			},
			{
				Key:   "queue_setup",
				Label: "初始化队列",
				ShouldSkip: func() bool {
					return true
				},
				Execute: func(ctx context.Context) error {
					steps = append(steps, "execute:queue_setup")
					return nil
				},
				NextKey: "final_drain",
			},
			{
				Key:   "final_drain",
				Label: "收尾输出",
				Execute: func(ctx context.Context) error {
					steps = append(steps, "execute:final_drain")
					return nil
				},
			},
		},
		OnTransition: func(transition Transition) error {
			steps = append(steps, "transition:"+transition.To)
			return nil
		},
		OnFinished: func() error {
			steps = append(steps, "finished")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("new state machine: %v", err)
	}

	if err := machine.Run(context.Background()); err != nil {
		t.Fatalf("run state machine: %v", err)
	}

	expected := []string{
		"transition:boot",
		"execute:boot",
		"transition:queue_setup",
		"transition:final_drain",
		"execute:final_drain",
		"transition:finished",
		"finished",
	}
	if !reflect.DeepEqual(steps, expected) {
		t.Fatalf("unexpected execution steps: %#v", steps)
	}
}
