package kernel

import (
	"encoding/json"
	"testing"
)

func TestInitMessage_NoBootMeta_OmitsMeta(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("c", "main", []string{TopicMessageUser}, []string{TopicSessionInit})

	msg := readMsg(t, conn)
	if !isSessionInit(&msg) {
		t.Fatalf("expected session.init, got %+v", msg)
	}
	if len(msg.Meta) != 0 {
		t.Fatalf("expected empty meta when boot meta unset, got %s", string(msg.Meta))
	}
}

func TestInitMessage_ForwardsBootMeta(t *testing.T) {
	env := newTestEnv(t)
	env.Hub.SetInitMeta(json.RawMessage(`{"agents":[{"name":"build"}],"default_agent":"build"}`))

	conn := env.connectAndJoin("c", "main", []string{TopicMessageUser}, []string{TopicSessionInit})

	msg := readMsg(t, conn)
	if !isSessionInit(&msg) {
		t.Fatalf("expected session.init, got %+v", msg)
	}
	if len(msg.Meta) == 0 {
		t.Fatalf("expected meta to be populated")
	}
	var meta struct {
		Agents       []map[string]string `json:"agents"`
		DefaultAgent string              `json:"default_agent"`
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
}
