// Package trust enforces explicit user approval of distro boot scripts.
//
// Boot scripts are arbitrary Python executed unconditionally on kernel start.
// A trust DB at $TABULA_HOME/state/trust.json records the SHA256 of every
// distro source tree the user has approved. When the kernel boots, it
// recomputes the hash and compares; if the entry is missing or stale, it
// refuses to start and tells the operator how to re-trust.
//
// SHA scope: every *.py file plus distro.toml, recursively, deterministic
// order. Whitespace-only edits to any tracked file invalidate the trust.
// Files outside that filter (markdown docs, templates, prompts) are excluded
// because they are inert from a code-execution standpoint.
package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Record is one entry in the trust DB.
type Record struct {
	BootSHA256 string `json:"boot_sha256"`
	TrustedAt  string `json:"trusted_at"`
	TrustedBy  string `json:"trusted_by"`
}

// DB is the on-disk shape of $TABULA_HOME/state/trust.json.
type DB struct {
	Distros map[string]Record `json:"distros"`
}

// Meta is the on-disk shape of $TABULA_HOME/state/trust.meta.json. It is
// only used to record that the migration shim auto-trusted an existing
// installation, so the shim can be removed safely in a future release.
type Meta struct {
	MigratedAt string `json:"migrated_at,omitempty"`
}

// Load reads ``path`` and returns the parsed DB. A missing file yields an
// empty DB without error — callers want the same shape regardless.
func Load(path string) (DB, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DB{Distros: map[string]Record{}}, nil
		}
		return DB{}, fmt.Errorf("read trust db %s: %w", path, err)
	}
	var db DB
	if len(data) == 0 {
		return DB{Distros: map[string]Record{}}, nil
	}
	if err := json.Unmarshal(data, &db); err != nil {
		return DB{}, fmt.Errorf("parse trust db %s: %w", path, err)
	}
	if db.Distros == nil {
		db.Distros = map[string]Record{}
	}
	return db, nil
}

// Save writes ``db`` to ``path`` atomically (temp + rename) so a crash
// cannot leave a half-written file.
func Save(path string, db DB) error {
	if db.Distros == nil {
		db.Distros = map[string]Record{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("trust db parent dir: %w", err)
	}
	payload, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal trust db: %w", err)
	}
	payload = append(payload, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("trust db tempfile: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write trust db: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync trust db: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close trust db: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename trust db: %w", err)
	}
	return nil
}

// HashDir computes a deterministic SHA256 over every ``*.py`` file and the
// top-level ``distro.toml`` inside ``dir``. Paths are recorded relative to
// ``dir`` so renames of the distro home directory do not invalidate trust.
//
// Layout of the hashed stream, for each file in sorted-relative-path order:
//
//	"<rel-path>\x00<len>\x00<bytes>\n"
//
// The ``len`` prefix prevents a payload from spilling into the next entry
// if someone manages to slip a NUL into a filename. Symlinks are skipped.
// Files under ``__pycache__``, ``.git``, and any dot-prefixed directory
// are skipped because they are derived or unrelated artifacts.
func HashDir(dir string) (string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("trust hash: stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("trust hash: %s is not a directory", dir)
	}
	var entries []string
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if path != dir && (name == "__pycache__" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		name := d.Name()
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		// distro.toml only matters at the top of the tree. Anywhere else
		// (a fixture or test asset) we leave it alone.
		if name == "distro.toml" && filepath.Dir(rel) == "." {
			entries = append(entries, rel)
			return nil
		}
		if strings.HasSuffix(name, ".py") {
			entries = append(entries, rel)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("trust hash: walk %s: %w", dir, err)
	}
	sort.Strings(entries)

	h := sha256.New()
	for _, rel := range entries {
		full := filepath.Join(dir, rel)
		data, err := os.ReadFile(full)
		if err != nil {
			return "", fmt.Errorf("trust hash: read %s: %w", full, err)
		}
		// Normalise rel to forward slashes so a Windows install computes
		// the same hash as a Unix one. We do not currently ship Windows
		// but the cost is one strings.ReplaceAll.
		relSlash := filepath.ToSlash(rel)
		if _, err := io.WriteString(h, relSlash); err != nil {
			return "", err
		}
		if _, err := io.WriteString(h, "\x00"); err != nil {
			return "", err
		}
		if _, err := fmt.Fprintf(h, "%d", len(data)); err != nil {
			return "", err
		}
		if _, err := io.WriteString(h, "\x00"); err != nil {
			return "", err
		}
		if _, err := h.Write(data); err != nil {
			return "", err
		}
		if _, err := io.WriteString(h, "\n"); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Check returns nil when ``dir`` matches the trust DB entry for
// ``distroID``. The returned error is always wrapped with one of the
// sentinel values below so callers can branch on intent.
//
// Error semantics:
//
//   - ErrNotTrusted   — no entry exists; user must run `tabula distro trust`.
//   - ErrSHAMismatch  — entry exists but the distro changed.
//   - other errors    — I/O or hash failures, surface unchanged.
func Check(dbPath, distroID, dir string) error {
	if strings.TrimSpace(distroID) == "" {
		return fmt.Errorf("trust check: empty distro id")
	}
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("trust check: empty distro dir")
	}
	db, err := Load(dbPath)
	if err != nil {
		return err
	}
	record, ok := db.Distros[distroID]
	if !ok {
		return &CheckError{Kind: ErrNotTrusted, DistroID: distroID, Dir: dir}
	}
	actual, err := HashDir(dir)
	if err != nil {
		return err
	}
	if actual != record.BootSHA256 {
		return &CheckError{
			Kind:     ErrSHAMismatch,
			DistroID: distroID,
			Dir:      dir,
			Expected: record.BootSHA256,
			Actual:   actual,
		}
	}
	return nil
}

// Approve writes (or refreshes) the trust record for ``distroID`` after
// computing the current hash of ``dir``. ``trustedBy`` is a free-form
// label, typically "user" or "installer-cold-start".
func Approve(dbPath, distroID, dir, trustedBy string) (Record, error) {
	digest, err := HashDir(dir)
	if err != nil {
		return Record{}, err
	}
	db, err := Load(dbPath)
	if err != nil {
		return Record{}, err
	}
	if db.Distros == nil {
		db.Distros = map[string]Record{}
	}
	record := Record{
		BootSHA256: digest,
		TrustedAt:  time.Now().UTC().Format(time.RFC3339),
		TrustedBy:  trustedBy,
	}
	db.Distros[distroID] = record
	if err := Save(dbPath, db); err != nil {
		return Record{}, err
	}
	return record, nil
}

// Revoke deletes the trust record for ``distroID``. Missing entries are a
// no-op.
func Revoke(dbPath, distroID string) error {
	db, err := Load(dbPath)
	if err != nil {
		return err
	}
	if _, ok := db.Distros[distroID]; !ok {
		return nil
	}
	delete(db.Distros, distroID)
	return Save(dbPath, db)
}

// ErrKind enumerates trust-check failure categories. Callers use it via
// errors.As(err, &*CheckError{}) and switch on Kind.
type ErrKind int

const (
	// ErrNotTrusted means the trust DB has no record for the distro.
	ErrNotTrusted ErrKind = iota + 1
	// ErrSHAMismatch means the recorded SHA does not match the on-disk one.
	ErrSHAMismatch
)

// CheckError is the structured error returned by Check.
type CheckError struct {
	Kind     ErrKind
	DistroID string
	Dir      string
	Expected string
	Actual   string
}

func (e *CheckError) Error() string {
	switch e.Kind {
	case ErrNotTrusted:
		return fmt.Sprintf(
			"distro %q has not been trusted; run `tabula distro trust %s` after reviewing %s",
			e.DistroID, e.DistroID, e.Dir,
		)
	case ErrSHAMismatch:
		return fmt.Sprintf(
			"distro %q boot tree changed since last trust\n  expected SHA: %s\n  actual SHA:   %s\n  run `tabula distro trust %s` to approve",
			e.DistroID, e.Expected, e.Actual, e.DistroID,
		)
	default:
		return fmt.Sprintf("trust check failed for %q", e.DistroID)
	}
}
