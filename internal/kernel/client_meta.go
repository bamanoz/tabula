package kernel

import (
	"encoding/json"
	"strings"

	"github.com/bamanoz/tabula/internal/runtime/wire"
)

type clientMeta struct {
	Role      string `json:"tabula.client_role"`
	Managed   bool   `json:"tabula.managed"`
	RuntimeID string `json:"tabula.runtime_id"`
}

func decodeClientMeta(raw json.RawMessage) clientMeta {
	if len(raw) == 0 {
		return clientMeta{}
	}
	var meta clientMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return clientMeta{}
	}
	meta.RuntimeID = normalizeClientRuntimeID(meta.RuntimeID)
	return meta
}

func normalizeClientRuntimeID(runtimeID string) string {
	runtimeID = strings.TrimSpace(runtimeID)
	if runtimeID == "" || wire.ValidateRuntimeID(runtimeID) != nil {
		return ""
	}
	return runtimeID
}
