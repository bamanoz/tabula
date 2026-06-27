package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	runtimeinstance "github.com/bamanoz/tabula/internal/runtime/instance"
	"github.com/bamanoz/tabula/internal/runtime/paths"
)

// RuntimeIDMetaKey is the hello metadata key apps should send when they want
// the kernel to prefer the co-installed runtime for later session work.
const RuntimeIDMetaKey = "tabula.runtime_id"

// LoadLocalRuntimeID reads the optional local runtime instance metadata from
// $TABULA_HOME/run/runtime-instance.json. Missing metadata is treated as a
// normal no-affinity case.
func LoadLocalRuntimeID() (string, error) {
	meta, err := runtimeinstance.Load(paths.RuntimeInstanceFile())
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return meta.RuntimeID, nil
}

// WithRuntimeAffinity returns hello metadata with the local runtime id injected
// under tabula.runtime_id when local runtime metadata is present.
//
// Non-Go clients should follow the same contract: send a hello data.meta JSON
// object and set tabula.runtime_id to the runtime_id loaded from
// $TABULA_HOME/run/runtime-instance.json when that file exists.
func WithRuntimeAffinity(raw json.RawMessage) (json.RawMessage, error) {
	runtimeID, err := LoadLocalRuntimeID()
	if err != nil {
		return nil, err
	}
	if runtimeID == "" {
		return cloneRawMessage(raw), nil
	}

	meta := map[string]any{}
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &meta); err != nil {
			return nil, fmt.Errorf("parse hello meta: %w", err)
		}
	}
	meta[RuntimeIDMetaKey] = runtimeID
	merged, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("encode hello meta: %w", err)
	}
	return merged, nil
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}
