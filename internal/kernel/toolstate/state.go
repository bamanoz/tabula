package toolstate

import (
	"fmt"
	"time"
)

const Kind = "tool.lifecycle"

type Event struct {
	State             string
	ToolID            string
	ToolName          string
	RunID             string
	Status            string
	ExchangeID        string
	Reason            string
	PreviousRunID     string
	TurnID            string
	AttemptID         string
	DriverInstanceID  string
	LeaseID           string
	DriverGeneration  uint64
	TurnCorrelationID string
}

type State struct {
	ToolID            string
	ToolName          string
	RunID             string
	TurnID            string
	AttemptID         string
	DriverInstanceID  string
	LeaseID           string
	DriverGeneration  uint64
	TurnCorrelationID string
	Terminal          bool
	Suspended         bool
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
		if event.TurnID != "" {
			state.TurnID = event.TurnID
		}
		if event.AttemptID != "" {
			state.AttemptID = event.AttemptID
		}
		if event.DriverInstanceID != "" {
			state.DriverInstanceID = event.DriverInstanceID
		}
		if event.LeaseID != "" {
			state.LeaseID = event.LeaseID
		}
		if event.DriverGeneration != 0 {
			state.DriverGeneration = event.DriverGeneration
		}
		if event.TurnCorrelationID != "" {
			state.TurnCorrelationID = event.TurnCorrelationID
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
