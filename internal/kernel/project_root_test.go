package kernel

import (
	"encoding/json"
	"testing"
)

// Tests for the project_root extension to the init message metadata.
// See docs/plans/CODER_DISTRO_PLAN.md (Phase 1).

func TestInitMessage_NoProjectRoot_OmitsMeta(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("c", "main", []string{"message"}, []string{"init"})

	msg := readMsg(t, conn)
	if msg.Type != "init" {
		t.Fatalf("expected init, got %s", msg.Type)
	}
	if len(msg.Meta) != 0 {
		t.Fatalf("expected empty meta when ProjectRoot unset, got %s", string(msg.Meta))
	}
}

func TestInitMessage_WithProjectRoot_EmitsMeta(t *testing.T) {
	env := newTestEnv(t)
	env.Hub.ProjectRoot = "/abs/work/repo"

	conn := env.connectAndJoin("c", "main", []string{"message"}, []string{"init"})

	msg := readMsg(t, conn)
	if msg.Type != "init" {
		t.Fatalf("expected init, got %s", msg.Type)
	}
	if len(msg.Meta) == 0 {
		t.Fatalf("expected meta to be populated when ProjectRoot is set")
	}
	var meta map[string]string
	if err := json.Unmarshal(msg.Meta, &meta); err != nil {
		t.Fatalf("meta is not a JSON object: %v (raw=%s)", err, string(msg.Meta))
	}
	if got := meta["project_root"]; got != "/abs/work/repo" {
		t.Fatalf("expected project_root=/abs/work/repo, got %q", got)
	}
}

func TestInitMessage_WithBootMeta_MergesProjectRoot(t *testing.T) {
	env := newTestEnv(t)
	env.Hub.SetInitMeta(json.RawMessage(`{"agents":[{"name":"build"}],"default_agent":"build"}`))
	env.Hub.ProjectRoot = "/abs/work/repo"

	conn := env.connectAndJoin("c", "main", []string{"message"}, []string{"init"})

	msg := readMsg(t, conn)
	if msg.Type != "init" {
		t.Fatalf("expected init, got %s", msg.Type)
	}
	if len(msg.Meta) == 0 {
		t.Fatalf("expected meta to be populated")
	}
	var meta struct {
		Agents       []map[string]string `json:"agents"`
		DefaultAgent string              `json:"default_agent"`
		ProjectRoot  string              `json:"project_root"`
	}
	if err := json.Unmarshal(msg.Meta, &meta); err != nil {
		t.Fatalf("meta is not a JSON object: %v (raw=%s)", err, string(msg.Meta))
	}
	if len(meta.Agents) != 1 || meta.Agents[0]["name"] != "build" {
		t.Fatalf("expected build agent in meta, got %+v", meta.Agents)
	}
	if meta.DefaultAgent != "build" {
		t.Fatalf("expected default_agent=build, got %q", meta.DefaultAgent)
	}
	if meta.ProjectRoot != "/abs/work/repo" {
		t.Fatalf("expected project_root=/abs/work/repo, got %q", meta.ProjectRoot)
	}
}
