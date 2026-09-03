// Package pathx owns cross-platform data/config path resolution (S1.2
// half): stdlib os.UserConfigDir-based layout, per-profile subtrees
// matching the physical-isolation rule (HARDQ B3: one database file per
// profile lives under its own directory).
package pathx

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

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

// ProfileDir is the per-profile subtree (physical isolation, B3).
func (l Layout) ProfileDir(p contracts.ProfileID) (string, error) {
	if !p.Valid() {
		return "", fmt.Errorf("pathx: invalid profile id")
	}
	return filepath.Join(l.Base, "profiles", string(p)), nil
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

// EnsureDir creates a directory 0700 (private by construction).
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("pathx: %w", err)
	}
	return nil
}
