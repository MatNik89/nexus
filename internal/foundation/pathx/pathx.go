// Package pathx owns cross-platform data/config path resolution (S1.2
// half): stdlib os.UserConfigDir-based layout, per-profile subtrees
// matching the physical-isolation rule (HARDQ B3: one database file per
// profile lives under its own directory).
package pathx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

// profileSlugOK: a profile id used as a PATH COMPONENT must be one
// canonical segment — the generic ID validator allows '/'  and '..', which
// would escape the profile subtree (Phase-1B codex #14: profile "../system"
// resolved into the system dir).
func profileSlugOK(p contracts.ProfileID) bool {
	s := string(p)
	if !p.Valid() || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

// Layout resolves every NEXUS directory from one base.
type Layout struct {
	Base string // e.g. ~/.config/nexus
}

// Default resolves the user-scoped layout.
func Default() (Layout, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return Layout{}, fmt.Errorf("pathx: %w", err)
	}
	return Layout{Base: filepath.Join(base, "nexus")}, nil
}

// ProfileDir is the per-profile subtree (physical isolation, B3). The id
// must be a canonical slug; containment is additionally PROVEN with
// filepath.Rel (defense in depth).
func (l Layout) ProfileDir(p contracts.ProfileID) (string, error) {
	if !profileSlugOK(p) {
		return "", fmt.Errorf("pathx: profile id is not a canonical path slug (fail closed)")
	}
	dir := filepath.Join(l.Base, "profiles", string(p))
	rel, err := filepath.Rel(filepath.Join(l.Base, "profiles"), dir)
	if err != nil || rel != string(p) || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("pathx: profile path escapes its subtree (fail closed)")
	}
	return dir, nil
}

// ProfileJournal is the profile's journal database path.
func (l Layout) ProfileJournal(p contracts.ProfileID) (string, error) {
	d, err := l.ProfileDir(p)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "journal.db"), nil
}

// SystemDir holds non-profile state (zero profile payloads — B3).
func (l Layout) SystemDir() string { return filepath.Join(l.Base, "system") }

// EnsureDir creates a directory and VERIFIES the result is a private,
// caller-owned real directory (Phase-1B codex #15: MkdirAll leaves a
// pre-existing 0777 dir untouched and follows symlinked components — that
// must refuse, not report success).
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("pathx: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("pathx: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("pathx: %s is a symlink — refused (fail closed)", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("pathx: %s is not a directory", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("pathx: %s permissions %o are too open (need 0700) — refused", path, info.Mode().Perm())
	}
	if sys, ok := info.Sys().(*syscall.Stat_t); ok && int(sys.Uid) != os.Geteuid() {
		return fmt.Errorf("pathx: %s not owned by the current user — refused", path)
	}
	return nil
}
