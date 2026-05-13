package tenant

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const migrationMarker = ".migrated_v1"

var (
	ErrExists   = errors.New("tenant exists")
	ErrNotFound = errors.New("tenant not found")
)

// Store persists and enumerates tenant metadata.
type Store interface {
	List() ([]Tenant, error)
	Get(id string) (Tenant, bool, error)
	Create(Tenant) error
	Delete(id string) error
}

// FSStore stores tenants under $TABULA_HOME/tenants/<id>/.
type FSStore struct {
	home string
	now  func() time.Time
}

type tenantFile struct {
	Tenant tenantFileData `toml:"tenant"`
}

type tenantFileData struct {
	ID          string `toml:"id"`
	DisplayName string `toml:"display_name,omitempty"`
	CreatedAt   string `toml:"created_at"`
}

// NewFSStore creates a filesystem-backed tenant store rooted at tabulaHome.
func NewFSStore(tabulaHome string) *FSStore {
	return &FSStore{home: filepath.Clean(strings.TrimSpace(tabulaHome)), now: time.Now}
}

// PrepareBootLayout migrates a legacy flat install if needed and ensures the default tenant exists.
func PrepareBootLayout(tabulaHome string) error {
	store := NewFSStore(tabulaHome)
	if err := store.MigrateLegacyFlatLayout(); err != nil {
		return err
	}
	_, ok, err := store.Get(DefaultID)
	if err != nil {
		return err
	}
	if !ok {
		if err := store.Create(Tenant{ID: DefaultID, DisplayName: "Default"}); err != nil {
			return err
		}
	}
	return EnsureDefaultWorkspaceRoot(tabulaHome)
}

// EnsureDefaultWorkspaceRoot gives the default tenant a workspace fallback.
func EnsureDefaultWorkspaceRoot(tabulaHome string) error {
	return NewFSStore(tabulaHome).ensureDefaultWorkspaceRoot()
}

func (s *FSStore) tenantsDir() string         { return filepath.Join(s.home, "tenants") }
func (s *FSStore) tenantDir(id string) string { return filepath.Join(s.tenantsDir(), id) }

func (s *FSStore) clock() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

// List returns all tenants sorted by id.
func (s *FSStore) List() ([]Tenant, error) {
	entries, err := os.ReadDir(s.tenantsDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	out := make([]Tenant, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		t, ok, err := s.Get(entry.Name())
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Get reads one tenant by id.
func (s *FSStore) Get(id string) (Tenant, bool, error) {
	if err := ValidateID(id); err != nil {
		return Tenant{}, false, err
	}
	path := filepath.Join(s.tenantDir(id), "tenant.toml")
	var raw tenantFile
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Tenant{}, false, nil
		}
		return Tenant{}, false, fmt.Errorf("read tenant %q: %w", id, err)
	}
	created, err := time.Parse(time.RFC3339, raw.Tenant.CreatedAt)
	if err != nil {
		return Tenant{}, false, fmt.Errorf("read tenant %q created_at: %w", id, err)
	}
	t, err := normalize(Tenant{ID: raw.Tenant.ID, DisplayName: raw.Tenant.DisplayName, CreatedAt: created}, s.clock())
	if err != nil {
		return Tenant{}, false, err
	}
	if t.ID != id {
		return Tenant{}, false, fmt.Errorf("tenant file id %q does not match directory %q", t.ID, id)
	}
	return t, true, nil
}

// Create writes the tenant directory layout and metadata.
func (s *FSStore) Create(t Tenant) error {
	t, err := normalize(t, s.clock())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.tenantsDir(), 0o700); err != nil {
		return fmt.Errorf("create tenants dir: %w", err)
	}
	root := s.tenantDir(t.ID)
	if _, err := os.Stat(filepath.Join(root, "tenant.toml")); err == nil {
		return fmt.Errorf("%w: %s", ErrExists, t.ID)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, dir := range tenantDirs(root) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create tenant dir %s: %w", dir, err)
		}
	}
	return writeTenantFile(filepath.Join(root, "tenant.toml"), t)
}

// Delete removes one tenant subtree.
func (s *FSStore) Delete(id string) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	root := s.tenantDir(id)
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return err
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("delete tenant %q: %w", id, err)
	}
	return nil
}

// MigrateLegacyFlatLayout creates the tenant tree for a flat install once.
// TODO(M5): remove this migration shim after the M4 tenant layout has shipped
// through one stable migration window.
func (s *FSStore) MigrateLegacyFlatLayout() error {
	tdir := s.tenantsDir()
	marker := filepath.Join(tdir, migrationMarker)
	if _, err := os.Stat(marker); err == nil {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(tdir, 0o700); err != nil {
		return fmt.Errorf("create tenants dir: %w", err)
	}
	if !hasLegacyFlatLayout(s.home) {
		return writeMarker(marker)
	}
	defaultRoot := s.tenantDir(DefaultID)
	for _, dir := range tenantMetadataDirs(defaultRoot) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(defaultRoot, "tenant.toml")); errors.Is(err, os.ErrNotExist) {
		defaultTenant, err := normalize(Tenant{ID: DefaultID, DisplayName: "Default", CreatedAt: s.clock().UTC()}, s.clock())
		if err != nil {
			return err
		}
		if err := writeTenantFile(filepath.Join(defaultRoot, "tenant.toml"), defaultTenant); err != nil {
			return err
		}
	}
	for _, dir := range tenantDirs(defaultRoot) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return writeMarker(marker)
}

func tenantDirs(root string) []string {
	dirs := tenantBaseDirs(root)
	dirs = append(dirs,
		filepath.Join(root, "skills"),
		filepath.Join(root, "plugins"),
		filepath.Join(root, "clients"),
		filepath.Join(root, "templates"),
	)
	return dirs
}

func tenantBaseDirs(root string) []string {
	dirs := tenantMetadataDirs(root)
	dirs = append(dirs,
		filepath.Join(root, "state"),
		filepath.Join(root, "state", "plugins"),
		filepath.Join(root, "state", "skills"),
		filepath.Join(root, "state", "sessions"),
	)
	return dirs
}

func tenantMetadataDirs(root string) []string {
	return []string{
		root,
		filepath.Join(root, "config"),
		filepath.Join(root, "config", "plugins"),
		filepath.Join(root, "cache"),
		filepath.Join(root, "logs"),
	}
}

func writeTenantFile(path string, t Tenant) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return toml.NewEncoder(f).Encode(tenantFile{Tenant: tenantFileData{ID: t.ID, DisplayName: t.DisplayName, CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339)}})
}

func (s *FSStore) ensureDefaultWorkspaceRoot() error {
	return ensureWorkspaceProjectRoot(filepath.Join(s.tenantDir(DefaultID), "config", "tenant.toml"), "${TABULA_HOME}")
}

func ensureWorkspaceProjectRoot(path, projectRoot string) error {
	var raw struct {
		Workspace struct {
			ProjectRoot string `toml:"project_root"`
		} `toml:"workspace"`
	}
	if _, err := toml.DecodeFile(path, &raw); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read tenant workspace: %w", err)
	}
	if strings.TrimSpace(raw.Workspace.ProjectRoot) != "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	text := string(data)
	escaped := strings.ReplaceAll(projectRoot, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	line := fmt.Sprintf("project_root = \"%s\"", escaped)
	if strings.Contains(text, "[workspace]") {
		lines := strings.Split(text, "\n")
		out := make([]string, 0, len(lines)+1)
		inWorkspace := false
		inserted := false
		for _, item := range lines {
			trimmed := strings.TrimSpace(item)
			if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
				if inWorkspace && !inserted {
					out = append(out, line)
					inserted = true
				}
				inWorkspace = trimmed == "[workspace]"
			}
			out = append(out, item)
		}
		if inWorkspace && !inserted {
			out = append(out, line)
		}
		text = strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
	} else {
		if strings.TrimSpace(text) != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += "\n[workspace]\n" + line + "\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(text), 0o600)
}

func hasLegacyFlatLayout(home string) bool {
	for _, name := range []string{"skills", "plugins", "clients", "templates", "state"} {
		if _, err := os.Stat(filepath.Join(home, name)); err == nil {
			return true
		}
	}
	return false
}

func writeMarker(path string) error {
	return os.WriteFile(path, []byte("M4-01 tenant layout migration completed\n"), 0o600)
}
