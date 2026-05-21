// Package localfs resolves BookWarehouse storage keys to local filesystem
// paths and verifies the resolved path stays inside an admin-configured
// library root.
//
// Resolution order:
//  1. If a path_remapping prefix matches the storage key (and the key looks
//     absolute), translate source_path → target_path.
//  2. Otherwise, treat the key as relative and join it to library_root.
//  3. Resolve symlinks on the result and confirm the real path is inside
//     library_root (or the remapping's target root), rejecting traversal.
//
// Errors returned by Resolve carry a structured *ResolveError so handlers
// can produce diagnostic responses without losing the source key.
package localfs

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Remapping rewrites an absolute BookWarehouse storage path prefix to a
// local mount path prefix. Used when the upstream returns absolute paths
// and the plugin sees the data at a different absolute path.
type Remapping struct {
	SourcePath string
	TargetPath string
}

// Resolver converts BookWarehouse storage keys into local paths.
type Resolver struct {
	libraryRoot string
	remappings  []Remapping
}

// New builds a Resolver. libraryRoot may be empty when only path remappings
// are configured; remappings may be empty when only libraryRoot is set.
// At least one must be provided for resolution to succeed.
func New(libraryRoot string, remappings []Remapping) *Resolver {
	return &Resolver{
		libraryRoot: filepath.Clean(libraryRoot),
		remappings:  remappings,
	}
}

// Configured reports whether the resolver has any way to translate keys to
// local paths. Used by handlers to short-circuit with a clearer 503.
func (r *Resolver) Configured() bool {
	return (r != nil) && (r.libraryRoot != "" && r.libraryRoot != "." || len(r.remappings) > 0)
}

// LibraryRoot returns the configured root, if any.
func (r *Resolver) LibraryRoot() string {
	if r == nil {
		return ""
	}
	return r.libraryRoot
}

// ResolveError captures enough context for an operator to fix the config.
type ResolveError struct {
	SourceKey string
	Reason    string
	Attempts  []string
}

func (e *ResolveError) Error() string {
	if len(e.Attempts) == 0 {
		return fmt.Sprintf("resolve %q: %s", e.SourceKey, e.Reason)
	}
	return fmt.Sprintf("resolve %q: %s (tried: %s)", e.SourceKey, e.Reason, strings.Join(e.Attempts, ", "))
}

// Resolve maps storageKey to an existing readable local path inside the
// configured library root. The returned path is the symlink-resolved real
// path; callers may pass it straight to os.Open.
func (r *Resolver) Resolve(storageKey string) (string, error) {
	if r == nil || !r.Configured() {
		return "", &ResolveError{SourceKey: storageKey, Reason: "library_root and path_remappings both empty"}
	}
	if storageKey == "" {
		return "", &ResolveError{SourceKey: storageKey, Reason: "empty storage key"}
	}

	cleaned := filepath.Clean(storageKey)
	attempts := make([]string, 0, 1+len(r.remappings))

	// Pass 1: try remapping rules for absolute keys.
	if filepath.IsAbs(cleaned) {
		for _, m := range r.remappings {
			src := filepath.Clean(m.SourcePath)
			tgt := filepath.Clean(m.TargetPath)
			if src == "." || tgt == "." {
				continue
			}
			if cleaned != src && !strings.HasPrefix(cleaned, src+string(filepath.Separator)) {
				continue
			}
			rel, err := filepath.Rel(src, cleaned)
			if err != nil || rel == "." {
				rel = ""
			}
			if strings.HasPrefix(rel, "..") {
				continue
			}
			candidate := filepath.Clean(filepath.Join(tgt, rel))
			attempts = append(attempts, candidate)
			if real, ok := resolveWithin(tgt, candidate); ok {
				return real, nil
			}
		}
		// Absolute key with no matching remap — try library_root as a fallback
		// (rare; usually absolute keys need a remap).
		if r.libraryRoot != "" {
			candidate := filepath.Clean(filepath.Join(r.libraryRoot, strings.TrimPrefix(cleaned, string(filepath.Separator))))
			attempts = append(attempts, candidate)
			if real, ok := resolveWithin(r.libraryRoot, candidate); ok {
				return real, nil
			}
		}
		return "", &ResolveError{
			SourceKey: storageKey,
			Reason:    "no path_remapping matched the absolute storage key",
			Attempts:  attempts,
		}
	}

	// Pass 2: relative key joined to library_root.
	if r.libraryRoot == "" || r.libraryRoot == "." {
		return "", &ResolveError{
			SourceKey: storageKey,
			Reason:    "relative storage key but library_root not configured",
		}
	}
	candidate := filepath.Clean(filepath.Join(r.libraryRoot, cleaned))
	attempts = append(attempts, candidate)
	if real, ok := resolveWithin(r.libraryRoot, candidate); ok {
		return real, nil
	}
	return "", &ResolveError{
		SourceKey: storageKey,
		Reason:    "file not found or unreadable",
		Attempts:  attempts,
	}
}

// AsResolveError unwraps an error chain looking for a ResolveError.
func AsResolveError(err error) *ResolveError {
	var re *ResolveError
	if errors.As(err, &re) {
		return re
	}
	return nil
}

// resolveWithin resolves symlinks in candidate and confirms the real path
// is still inside rootPrefix. Returns false when either path cannot be
// resolved (e.g. file missing) or when the resolved path escapes the root.
func resolveWithin(rootPrefix, candidate string) (string, bool) {
	realRoot, err := filepath.EvalSymlinks(rootPrefix)
	if err != nil {
		return "", false
	}
	realPath, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", false
	}
	if realPath != realRoot && !strings.HasPrefix(realPath, realRoot+string(filepath.Separator)) {
		return "", false
	}
	return realPath, true
}
