package toolresult

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/bamanoz/tabula/internal/runtime/artifacts"
)

type Envelope struct {
	Output    string              `json:"output"`
	Artifact  *artifacts.Metadata `json:"artifact,omitempty"`
	Truncated bool                `json:"truncated,omitempty"`
}

func Materialize(tabulaHome, session, tenantID, toolID, toolName string, raw json.RawMessage) (json.RawMessage, error) {
	output, err := Render(raw)
	if err != nil {
		return nil, err
	}
	env := Envelope{Output: output}
	if utf8.RuneCountInString(output) > artifacts.ThresholdChars {
		artifact, err := artifacts.WriteToolResultArtifact(tabulaHome, session, tenantID, toolID, toolName, output)
		if err != nil {
			return nil, err
		}
		env.Output = Preview(output, artifact)
		env.Artifact = artifact
		env.Truncated = true
	}
	return json.Marshal(env)
}

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

func Preview(output string, artifact *artifacts.Metadata) string {
	if artifact == nil {
		return output
	}
	preview := firstRunes(output, artifacts.PreviewChars)
	omitted := max(0, utf8.RuneCountInString(output)-utf8.RuneCountInString(preview))
	return fmt.Sprintf(
		"%s\n\n[Tabula stored the full tool result as artifact %s. Preview chars: %d; omitted chars: %d; sha256: %s. Use session_artifact_read with offset and limit_chars to inspect it in bounded chunks.]",
		preview,
		artifact.Ref,
		utf8.RuneCountInString(preview),
		omitted,
		artifact.SHA256,
	)
}

func firstRunes(text string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
