package stream

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ContinuumApp/continuum-plugin-bookwarehouse-audio/internal/bookwarehouse"
)

// A symlink planted inside the admin-configured target root (or an
// attacker-influenced upstream source path) must not let a direct-serve
// resolve outside the root, even though it is lexically contained.
func TestRemapPath_SymlinkEscapeBlocked(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(filepath.Join(root, "audio"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	legit := filepath.Join(root, "audio", "book.m4b")
	if err := os.WriteFile(legit, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("top-secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	// root/escape -> base/outside (a symlink that points out of the root).
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	remaps := []PathRemapping{{SourcePath: "/wh", TargetPath: root}}

	// Legitimate file inside the root resolves and stays contained.
	got, ok := remapPath("/wh/audio/book.m4b", remaps)
	if !ok {
		t.Fatal("legitimate path should resolve")
	}
	realRoot, _ := filepath.EvalSymlinks(root)
	if got != realRoot && !filepathHasPrefix(got, realRoot) {
		t.Fatalf("resolved path %q escaped root %q", got, realRoot)
	}

	// Path that lexically lives under root but resolves through the symlink
	// to base/outside must be rejected.
	if got, ok := remapPath("/wh/escape/secret.txt", remaps); ok {
		t.Fatalf("symlink escape was allowed: %q", got)
	}
}

func filepathHasPrefix(p, prefix string) bool {
	return len(p) > len(prefix) && p[:len(prefix)] == prefix && p[len(prefix)] == filepath.Separator
}

// findFile must not fall back to positional indexing when the upstream
// populates explicit indices — that would silently serve a different track.
func TestFindFile_NoSilentWrongTrack(t *testing.T) {
	indexed := []bookwarehouse.File{
		{Index: 0, Filename: "a"},
		{Index: 1, Filename: "b"},
		{Index: 2, Filename: "c"},
	}
	if f, ok := findFile(indexed, 5); ok {
		t.Fatalf("idx=5 absent but served %q (must defer to upstream)", f.Filename)
	}
	if f, ok := findFile(indexed, 1); !ok || f.Filename != "b" {
		t.Fatalf("idx=1 should match the indexed file, got %q ok=%v", f.Filename, ok)
	}
	// Non-contiguous indices: exact match still wins.
	gapped := []bookwarehouse.File{{Index: 2, Filename: "x"}, {Index: 5, Filename: "y"}}
	if f, ok := findFile(gapped, 5); !ok || f.Filename != "y" {
		t.Fatalf("idx=5 should match Index:5, got %q ok=%v", f.Filename, ok)
	}

	// Upstream that sets no indices at all (all zero) keeps positional access.
	unindexed := []bookwarehouse.File{{Filename: "t0"}, {Filename: "t1"}, {Filename: "t2"}}
	if f, ok := findFile(unindexed, 2); !ok || f.Filename != "t2" {
		t.Fatalf("positional fallback broken, got %q ok=%v", f.Filename, ok)
	}
	if _, ok := findFile(unindexed, 9); ok {
		t.Fatal("out-of-range positional request should be not-found")
	}
}
