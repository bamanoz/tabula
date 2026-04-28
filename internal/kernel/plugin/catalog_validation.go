package plugin

import (
	"fmt"
	"strings"
)

// NormalizeRegisterParams validates and sanitizes the dynamic catalog a plugin
// returns during the register handshake. It intentionally covers only protocol-
// local invariants; the parent kernel may add event-name policy checks via
// SpawnOptions.ValidateRegister without creating an import cycle.
func NormalizeRegisterParams(reg *RegisterParams) (*RegisterParams, error) {
	if reg == nil {
		return nil, fmt.Errorf("register params are required")
	}
	tools, err := normalizeToolSpecs("tools", reg.Tools)
	if err != nil {
		return nil, err
	}
	subs, err := normalizeSubscriptionSpecs("subscriptions", reg.Subscriptions)
	if err != nil {
		return nil, err
	}
	out := *reg
	out.PluginID = strings.TrimSpace(out.PluginID)
	out.Tools = tools
	out.Subscriptions = subs
	return &out, nil
}

// NormalizeUpdateToolsParams validates and sanitizes an update_tools payload.
// Callers should apply the returned payload atomically: invalid updates must not
// replace the previous live catalog.
func NormalizeUpdateToolsParams(p *UpdateToolsParams) (*UpdateToolsParams, error) {
	if p == nil {
		return nil, fmt.Errorf("update_tools params are required")
	}
	tools, err := normalizeToolSpecs("tools", p.Tools)
	if err != nil {
		return nil, err
	}
	out := *p
	out.Tools = tools
	out.Removed = append([]string(nil), p.Removed...)
	return &out, nil
}

func normalizeToolSpecs(field string, tools []ToolSpec) ([]ToolSpec, error) {
	seen := make(map[string]int, len(tools))
	out := make([]ToolSpec, 0, len(tools))
	for i, tool := range tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			return nil, fmt.Errorf("%s[%d].name is required", field, i)
		}
		if first, ok := seen[name]; ok {
			return nil, fmt.Errorf("%s[%d].name duplicates %s[%d].name %q", field, i, field, first, name)
		}
		if tool.DeadlineMs < 0 {
			return nil, fmt.Errorf("%s[%d].deadline_ms must be >= 0", field, i)
		}
		seen[name] = i
		tool.Name = name
		out = append(out, tool)
	}
	return out, nil
}

func normalizeSubscriptionSpecs(field string, subs []SubscriptionSpec) ([]SubscriptionSpec, error) {
	out := make([]SubscriptionSpec, 0, len(subs))
	for i, sub := range subs {
		event := strings.TrimSpace(sub.Event)
		if event == "" {
			return nil, fmt.Errorf("%s[%d].event is required", field, i)
		}
		if sub.TimeoutMs != nil && *sub.TimeoutMs < 0 {
			return nil, fmt.Errorf("%s[%d].timeout_ms must be >= 0", field, i)
		}
		sub.Event = event
		out = append(out, sub)
	}
	return out, nil
}
