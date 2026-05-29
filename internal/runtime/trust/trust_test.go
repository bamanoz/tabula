package trust

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// makeDistro lays out a minimal distro tree under ``root`` and returns the
// dir path. Layout matches what tabula-install produces: top-level
// boot.py + distro.toml + application/ submodule.
func makeDistro(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "code-immune")
	mustMkdir(t, dir)
	mustWrite(t, filepath.Join(dir, "boot.py"), "print('hello')\n")
	mustWrite(t, filepath.Join(dir, "distro.toml"), "id = 'code-immune'\n")
	mustMkdir(t, filepath.Join(dir, "application"))
	mustWrite(t, filepath.Join(dir, "application", "main.py"), "x = 1\n")
	// Excluded: docs, derived caches, hidden files.
	mustWrite(t, filepath.Join(dir, "README.md"), "not hashed\n")
	mustMkdir(t, filepath.Join(dir, "__pycache__"))
	mustWrite(t, filepath.Join(dir, "__pycache__", "boot.cpython-313.pyc"), "skip me\n")
	mustMkdir(t, filepath.Join(dir, ".git"))
	mustWrite(t, filepath.Join(dir, ".git", "config"), "skip me\n")
	return dir
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func TestHashDir_IsDeterministic(t *testing.T) {
	root := t.TempDir()
	dir := makeDistro(t, root)

	h1, err := HashDir(dir)
	if err != nil {
		t.Fatalf("first hash: %v", err)
	}
	h2, err := HashDir(dir)
	if err != nil {
		t.Fatalf("second hash: %v", err)
	}
	if h1 != h2 {
		t.Errorf("hash is not deterministic: %s vs %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex digest, got %d chars", len(h1))
	}
}

func TestHashDir_DetectsBootPyChange(t *testing.T) {
	root := t.TempDir()
	dir := makeDistro(t, root)
	before, err := HashDir(dir)
	if err != nil {
		t.Fatalf("hash before: %v", err)
	}

	// Even a whitespace-only change must invalidate the hash.
	mustWrite(t, filepath.Join(dir, "boot.py"), "print('hello')\n ")
	after, err := HashDir(dir)
	if err != nil {
		t.Fatalf("hash after: %v", err)
	}
	if before == after {
		t.Errorf("hash did not change after editing boot.py")
	}
}

func TestHashDir_DetectsNestedPyChange(t *testing.T) {
	root := t.TempDir()
	dir := makeDistro(t, root)
	before, err := HashDir(dir)
	if err != nil {
		t.Fatalf("hash before: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "application", "main.py"), "x = 2\n")
	after, err := HashDir(dir)
	if err != nil {
		t.Fatalf("hash after: %v", err)
	}
	if before == after {
		t.Errorf("hash did not change after editing application/main.py")
	}
}

func TestHashDir_DetectsDistroTomlChange(t *testing.T) {
	root := t.TempDir()
	dir := makeDistro(t, root)
	before, err := HashDir(dir)
	if err != nil {
		t.Fatalf("hash before: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "distro.toml"), "id = 'code-immune'\nextra = 1\n")
	after, err := HashDir(dir)
	if err != nil {
		t.Fatalf("hash after: %v", err)
	}
	if before == after {
		t.Errorf("hash did not change after editing distro.toml")
	}
}

func TestHashDir_IgnoresExcludedFiles(t *testing.T) {
	root := t.TempDir()
	dir := makeDistro(t, root)
	before, err := HashDir(dir)
	if err != nil {
		t.Fatalf("hash before: %v", err)
	}

	// README, __pycache__, .git changes must not move the digest.
	mustWrite(t, filepath.Join(dir, "README.md"), "completely different\n")
	mustWrite(t, filepath.Join(dir, "__pycache__", "boot.cpython-313.pyc"), "changed\n")
	mustWrite(t, filepath.Join(dir, ".git", "config"), "changed\n")
	// Nested distro.toml outside top-level must also be ignored.
	mustMkdir(t, filepath.Join(dir, "fixtures"))
	mustWrite(t, filepath.Join(dir, "fixtures", "distro.toml"), "noise\n")

	after, err := HashDir(dir)
	if err != nil {
		t.Fatalf("hash after: %v", err)
	}
	if before != after {
		t.Errorf("hash changed after editing excluded files (%s -> %s)", before, after)
	}
}

func TestHashDir_MissingDirFails(t *testing.T) {
	_, err := HashDir(filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Errorf("expected error for missing dir, got nil")
	}
}

func TestLoadSave_RoundTrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state", "trust.json")

	// Missing file -> empty DB, not an error.
	db, err := Load(dbPath)
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if len(db.Distros) != 0 {
		t.Errorf("expected empty DB, got %d entries", len(db.Distros))
	}

	db.Distros["code-immune"] = Record{
		BootSHA256: "abc123",
		TrustedAt:  "2026-05-29T12:00:00Z",
		TrustedBy:  "user",
	}
	if err := Save(dbPath, db); err != nil {
		t.Fatalf("save: %v", err)
	}

	reloaded, err := Load(dbPath)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got, ok := reloaded.Distros["code-immune"]
	if !ok {
		t.Fatalf("entry missing after reload")
	}
	if got.BootSHA256 != "abc123" {
		t.Errorf("sha mismatch after reload: %s", got.BootSHA256)
	}
}

func TestSave_WritesPrettyJSON(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "trust.json")
	db := DB{Distros: map[string]Record{
		"x": {BootSHA256: "h", TrustedAt: "t", TrustedBy: "u"},
	}}
	if err := Save(dbPath, db); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Errorf("output is not valid JSON: %v", err)
	}
}

func TestCheck_NotTrusted(t *testing.T) {
	root := t.TempDir()
	dir := makeDistro(t, root)
	dbPath := filepath.Join(root, "state", "trust.json")

	err := Check(dbPath, "code-immune", dir)
	var ce *CheckError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CheckError, got %T: %v", err, err)
	}
	if ce.Kind != ErrNotTrusted {
		t.Errorf("expected ErrNotTrusted, got %v", ce.Kind)
	}
	if ce.DistroID != "code-immune" {
		t.Errorf("unexpected distro id in error: %s", ce.DistroID)
	}
}

func TestCheck_ApprovedThenMatches(t *testing.T) {
	root := t.TempDir()
	dir := makeDistro(t, root)
	dbPath := filepath.Join(root, "state", "trust.json")

	if _, err := Approve(dbPath, "code-immune", dir, "user"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := Check(dbPath, "code-immune", dir); err != nil {
		t.Errorf("check after approve: %v", err)
	}
}

func TestCheck_DetectsMismatchAfterEdit(t *testing.T) {
	root := t.TempDir()
	dir := makeDistro(t, root)
	dbPath := filepath.Join(root, "state", "trust.json")

	if _, err := Approve(dbPath, "code-immune", dir, "user"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "boot.py"), "print('hijacked')\n")
	err := Check(dbPath, "code-immune", dir)
	var ce *CheckError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CheckError, got %T: %v", err, err)
	}
	if ce.Kind != ErrSHAMismatch {
		t.Errorf("expected ErrSHAMismatch, got %v", ce.Kind)
	}
	if ce.Expected == ce.Actual {
		t.Errorf("expected/actual should differ in mismatch error")
	}
}

func TestRevoke(t *testing.T) {
	root := t.TempDir()
	dir := makeDistro(t, root)
	dbPath := filepath.Join(root, "state", "trust.json")

	if _, err := Approve(dbPath, "code-immune", dir, "user"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if err := Revoke(dbPath, "code-immune"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	err := Check(dbPath, "code-immune", dir)
	var ce *CheckError
	if !errors.As(err, &ce) || ce.Kind != ErrNotTrusted {
		t.Errorf("expected ErrNotTrusted after revoke, got %v", err)
	}
	// Revoking a missing entry is a no-op.
	if err := Revoke(dbPath, "code-immune"); err != nil {
		t.Errorf("revoke missing: %v", err)
	}
}

func TestCheck_EmptyDistroIDFails(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "trust.json")
	if err := Check(dbPath, "", "/some/dir"); err == nil {
		t.Errorf("expected error for empty distro id")
	}
}

func TestCheck_EmptyDirFails(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "trust.json")
	if err := Check(dbPath, "x", ""); err == nil {
		t.Errorf("expected error for empty dir")
	}
}

// makeDistroPinned mirrors the fixture in
// tools/tabula-distro/tests/test_trust.py::_make_distro byte-for-byte.
// Used by TestHashDir_PinnedFixtureMatchesPython to pin the cross-language
// digest. Do NOT add .git or any other entries here — the Python fixture
// does not have them, and any drift breaks the pinned constant.
func makeDistroPinned(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "code-immune")
	mustMkdir(t, dir)
	mustWrite(t, filepath.Join(dir, "boot.py"), "print('hello')\n")
	mustWrite(t, filepath.Join(dir, "distro.toml"), "id = 'code-immune'\n")
	mustMkdir(t, filepath.Join(dir, "application"))
	mustWrite(t, filepath.Join(dir, "application", "main.py"), "x = 1\n")
	mustWrite(t, filepath.Join(dir, "README.md"), "docs\n")
	mustMkdir(t, filepath.Join(dir, "__pycache__"))
	mustWrite(t, filepath.Join(dir, "__pycache__", "boot.cpython-313.pyc"), "skip\n")
	return dir
}

// TestHashDir_PinnedFixtureMatchesPython locks the Go implementation to the
// exact digest the Python side asserts on in
// tools/tabula-distro/tests/test_trust.py::HashDirCrossLanguageTests.
// If either implementation drifts, one of the two tests breaks loudly —
// which is what we want, because silent drift would corrupt every user's
// trust DB on the next reinstall.
func TestHashDir_PinnedFixtureMatchesPython(t *testing.T) {
	const expectedSHA256 = "05c8091020bb6dd91bcadd486ab5abe23f2fadce3bcb8d3bb7ec54ebbdfd3498"
	root := t.TempDir()
	dir := makeDistroPinned(t, root)
	got, err := HashDir(dir)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if got != expectedSHA256 {
		t.Errorf("pinned digest drift:\n  expected (matches Python): %s\n  got      (Go HashDir):     %s\nIf you changed the hash algorithm or fixture, regenerate BOTH constants together.", expectedSHA256, got)
	}
}
