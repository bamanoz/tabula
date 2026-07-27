package kernel

import (
	"encoding/json"
	"testing"
)

func TestInitMessageMergesTenantInitMeta(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.SetInitMeta(json.RawMessage(`{"prompt_builder":"global.builder","workspace":{"path":"/global"}}`))
	hub.SetTenantInitMeta("claw-tabula", json.RawMessage(`{"prompt_builder":"claw_prompt.builder","workspace":{"path":"/repo"}}`))

	msg := hub.initMessage("", hub.initToolsJSON("claw-tabula"), hub.initMetaJSON("claw-tabula"))
	var meta map[string]any
	if err := json.Unmarshal(msg.Meta, &meta); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}
	if meta["prompt_builder"] != "claw_prompt.builder" {
		t.Fatalf("tenant prompt_builder did not override global: %#v", meta)
	}
	workspace := meta["workspace"].(map[string]any)
	if workspace["path"] != "/repo" {
		t.Fatalf("tenant workspace did not override global: %#v", workspace)
	}
}
