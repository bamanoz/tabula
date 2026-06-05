package artifacts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	RefPrefix           = "artifact://"
	ThresholdChars      = 12000
	PreviewChars        = 4000
	MaxReadLimitChars   = 12000
	artifactFilenameExt = ".txt"
)

var invalidStem = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

type Metadata struct {
	ID           string  `json:"id"`
	Ref          string  `json:"ref"`
	Session      string  `json:"session"`
	TenantID     string  `json:"tenant_id"`
	ToolID       string  `json:"tool_id"`
	ToolName     string  `json:"tool_name"`
	Filename     string  `json:"filename"`
	MimeType     string  `json:"mime_type"`
	Chars        int     `json:"chars"`
	Bytes        int     `json:"bytes"`
	SHA256       string  `json:"sha256"`
	PreviewChars int     `json:"preview_chars"`
	CreatedAt    float64 `json:"created_at"`
}

type indexFile struct {
	Version   int                  `json:"version"`
	Artifacts map[string]*Metadata `json:"artifacts"`
}

func SafeArtifactID(toolName, toolID, output string) string {
	stem := strings.Trim(toolName+"-"+toolID, "-._")
	stem = invalidStem.ReplaceAllString(stem, "-")
	stem = strings.Trim(stem[:min(len(stem), 80)], "-._")
	if stem == "" {
		stem = "tool-result"
	}
	sum := sha256.Sum256([]byte(output))
	return fmt.Sprintf("%s-%s", stem, hex.EncodeToString(sum[:])[:16])
}

func ArtifactRef(id string) string {
	return RefPrefix + strings.TrimSpace(id)
}

func HistoryDir(tabulaHome, session string) string {
	return filepath.Join(strings.TrimSpace(tabulaHome), "data", "sessions", filepath.FromSlash(session))
}

func ArtifactDir(tabulaHome, session string) string {
	return filepath.Join(HistoryDir(tabulaHome, session), "artifacts")
}

func ArtifactIndexFile(tabulaHome, session string) string {
	return filepath.Join(ArtifactDir(tabulaHome, session), "index.json")
}

func WriteToolResultArtifact(tabulaHome, session, tenantID, toolID, toolName, output string) (*Metadata, error) {
	if strings.TrimSpace(session) == "" {
		return nil, fmt.Errorf("artifact session is required")
	}
	artifactID := SafeArtifactID(toolName, toolID, output)
	root := ArtifactDir(tabulaHome, session)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	filename := artifactID + artifactFilenameExt
	path := filepath.Join(root, filename)
	if err := os.WriteFile(path, []byte(output), 0o644); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(output))
	meta := &Metadata{
		ID:           artifactID,
		Ref:          ArtifactRef(artifactID),
		Session:      session,
		TenantID:     defaultString(strings.TrimSpace(tenantID), "default"),
		ToolID:       toolID,
		ToolName:     toolName,
		Filename:     filename,
		MimeType:     "text/plain; charset=utf-8",
		Chars:        utf8.RuneCountInString(output),
		Bytes:        len(output),
		SHA256:       hex.EncodeToString(sum[:]),
		PreviewChars: min(PreviewChars, utf8.RuneCountInString(output)),
		CreatedAt:    float64(time.Now().UnixNano()) / 1e9,
	}
	index, err := readIndex(tabulaHome, session)
	if err != nil {
		return nil, err
	}
	if existing := index.Artifacts[artifactID]; existing != nil && existing.CreatedAt > 0 {
		meta.CreatedAt = existing.CreatedAt
	}
	index.Artifacts[artifactID] = meta
	if err := writeIndex(tabulaHome, session, index); err != nil {
		return nil, err
	}
	return meta, nil
}

func readIndex(tabulaHome, session string) (*indexFile, error) {
	path := ArtifactIndexFile(tabulaHome, session)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &indexFile{Version: 1, Artifacts: map[string]*Metadata{}}, nil
		}
		return nil, err
	}
	var idx indexFile
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	if idx.Artifacts == nil {
		idx.Artifacts = map[string]*Metadata{}
	}
	if idx.Version == 0 {
		idx.Version = 1
	}
	return &idx, nil
}

func writeIndex(tabulaHome, session string, idx *indexFile) error {
	path := ArtifactIndexFile(tabulaHome, session)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".artifact-index-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
