package instance

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/wire"
)

const MetadataVersion = 1

// Metadata records the stable local runtime instance identity shared by the
// runtime daemon and co-installed apps.
type Metadata struct {
	Version   int       `json:"version"`
	RuntimeID string    `json:"runtime_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Load reads and validates runtime instance metadata from path.
func Load(path string) (Metadata, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Metadata{}, fmt.Errorf("runtime instance metadata path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, err
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return Metadata{}, fmt.Errorf("parse runtime instance metadata %s: %w", path, err)
	}
	if err := validate(meta); err != nil {
		return Metadata{}, fmt.Errorf("validate runtime instance metadata %s: %w", path, err)
	}
	return meta, nil
}

// Ensure returns existing validated metadata from path or bootstraps a fresh
// stable runtime id when the file is absent.
func Ensure(path string, now time.Time) (Metadata, error) {
	meta, err := Load(path)
	if err == nil {
		return meta, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Metadata{}, err
	}
	return create(path, now)
}

// GenerateRuntimeID returns a new stable runtime id suitable for persisted
// local instance metadata.
func GenerateRuntimeID() (string, error) {
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate runtime id: %w", err)
	}
	id := "rt-" + hex.EncodeToString(raw)
	if err := wire.ValidateRuntimeID(id); err != nil {
		return "", err
	}
	return id, nil
}

func create(path string, now time.Time) (Metadata, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Metadata{}, fmt.Errorf("runtime instance metadata path is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	runtimeID, err := GenerateRuntimeID()
	if err != nil {
		return Metadata{}, err
	}
	meta := Metadata{Version: MetadataVersion, RuntimeID: runtimeID, CreatedAt: now.UTC()}
	if err := write(path, meta); err != nil {
		if errors.Is(err, os.ErrExist) {
			return Load(path)
		}
		return Metadata{}, err
	}
	return meta, nil
}

func write(path string, meta Metadata) error {
	if err := validate(meta); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create runtime instance metadata dir: %w", err)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("encode runtime instance metadata: %w", err)
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("write runtime instance metadata: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write runtime instance metadata: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close runtime instance metadata: %w", err)
	}
	return nil
}

func validate(meta Metadata) error {
	if meta.Version != MetadataVersion {
		return fmt.Errorf("unsupported version %d", meta.Version)
	}
	if err := wire.ValidateRuntimeID(strings.TrimSpace(meta.RuntimeID)); err != nil {
		return err
	}
	if meta.CreatedAt.IsZero() {
		return fmt.Errorf("created_at is required")
	}
	return nil
}
