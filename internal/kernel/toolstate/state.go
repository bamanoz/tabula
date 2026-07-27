package toolstate

import (
	"fmt"
	"time"
)

const Kind = "tool.lifecycle"

type Event struct {
	State         string
	ToolID        string
	ToolName      string
	RunID         string
	Status        string
	ExchangeID    string
	Reason        string
	PreviousRunID string
}

type State struct {
	ToolID    string
	ToolName  string
	RunID     string
	Terminal  bool
	Suspended bool
}

type Store interface {
	AppendToolLifecycle(session, tenantID string, event Event) error
	LoadToolLifecycle(session, tenantID string) ([]Event, error)
}

func NewRunID() string {
	return fmt.Sprintf("kernel-%d", time.Now().UnixNano())
}

func States(events []Event) map[string]State {
	states := map[string]State{}
	for _, event := range events {
		if event.ToolID == "" {
			continue
		}
		state := states[event.ToolID]
		state.ToolID = event.ToolID
		if event.ToolName != "" {
			state.ToolName = event.ToolName
		}
		switch event.State {
		case "started":
			state.RunID = event.RunID
			state.Terminal = false
			state.Suspended = false
		case "suspended":
			state.Suspended = true
		case "terminal":
			state.Terminal = true
			state.Suspended = false
		}
		states[event.ToolID] = state
	}
	return states
}
