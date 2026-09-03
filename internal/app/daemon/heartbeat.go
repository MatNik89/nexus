//go:build linux

package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"time"
)

// Heartbeat is the C8 positive liveness half: the daemon touches a file
// every interval; anyone can prove staleness from mtime alone.
type Heartbeat struct {
	path     string
	interval time.Duration
}

func NewHeartbeat(path string, interval time.Duration) *Heartbeat {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Heartbeat{path: path, interval: interval}
}

// Run beats until ctx ends. Failures to touch are retried next tick — a
// dying disk shows up as staleness, exactly as it should.
func (h *Heartbeat) Run(ctx context.Context) {
	t := time.NewTicker(h.interval)
	defer t.Stop()
	h.beat()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.beat()
		}
	}
}

func (h *Heartbeat) beat() {
	now := time.Now()
	if err := os.Chtimes(h.path, now, now); err != nil {
		os.WriteFile(h.path, []byte("nexus-heartbeat\n"), 0o600)
	}
}

// Stale reports whether the heartbeat at path stopped: a missing file or
// an mtime older than 2×interval is STALE (one missed beat tolerated,
// detection still within the check interval after that).
func Stale(path string, interval time.Duration) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) > 2*interval
}

// sha256Hex is a tiny local helper (content hashes for input blocks).
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
