package toolresult

import (
	"encoding/json"
	"strings"
)

func Render(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "OK", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	return strings.TrimSpace(string(raw)), nil
}
