package clientmeta

import (
	"encoding/json"
	"strings"

	"github.com/bamanoz/tabula/internal/runtime/wire"
)

type Meta struct {
	Role      string `json:"tabula.client_role"`
	RuntimeID string `json:"tabula.runtime_id"`
}

func Decode(raw json.RawMessage) Meta {
	if len(raw) == 0 {
		return Meta{}
	}
	var meta Meta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return Meta{}
	}
	meta.RuntimeID = NormalizeRuntimeID(meta.RuntimeID)
	return meta
}

func NormalizeRuntimeID(runtimeID string) string {
	runtimeID = strings.TrimSpace(runtimeID)
	if runtimeID == "" || wire.ValidateRuntimeID(runtimeID) != nil {
		return ""
	}
	return runtimeID
}
