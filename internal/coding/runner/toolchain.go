//go:build linux

package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// toolchainExecutables, resolved relative to GOROOT, that the coding-run
// actually execs beyond GOTOOLDIR's own compiler/linker/asm/cgo children
// (plan-review round 1, codex's independent finding: GOTOOLDIR alone
// misses the entry points the runner's own `go build`/`go test`
// invocation execs FIRST).
var toolchainExecutables = []string{"bin/go", "bin/gofmt"}

// ToolchainPin is the resolved, content-pinned Go toolchain visibility
// grant for a coding-run: which host directories to bind read-only, which
// environment variables to set, and a content-hash digest over the small,
// bounded set of executables that actually run inside the sandbox.
//
// ROBinds intentionally binds BOTH the resolved GOROOT and GOMODCACHE,
// always, rather than choosing one at resolution time: on a toolchain-
// managed install these nest (GOTOOLDIR ⊂ GOROOT ⊂ GOMODCACHE) and
// probe.Prepare's own canonical-path de-duplication collapses the pair to
// one effective bind; on a standalone Go install they don't nest, and
// both binds are needed. Either way this package never has to branch on
// the host's install layout (plan-review round 1).
type ToolchainPin struct {
	ROBinds    []string
	Env        map[string]string
	HashDigest string
}

// ResolveToolchain invokes `go env` via goBinary (an absolute path or a
// name resolvable on PATH) to discover GOROOT/GOMODCACHE/GOTOOLDIR, and
// content-hashes GOTOOLDIR's entries plus toolchainExecutables. It does
// NOT set GOCACHE/GOTMPDIR — those must point inside the coding-run's own
// disposable WorkDir, a decision the runner's own orchestration (not this
// package) makes once it lays out that directory.
func ResolveToolchain(goBinary string) (ToolchainPin, error) {
	env, err := goEnvJSON(goBinary, "GOROOT", "GOMODCACHE", "GOTOOLDIR")
	if err != nil {
		return ToolchainPin{}, fmt.Errorf("runner: resolve toolchain: %w", err)
	}
	goroot, gomodcache, gotooldir := env["GOROOT"], env["GOMODCACHE"], env["GOTOOLDIR"]
	if goroot == "" || gomodcache == "" || gotooldir == "" {
		return ToolchainPin{}, fmt.Errorf("runner: resolve toolchain: go env returned an empty GOROOT/GOMODCACHE/GOTOOLDIR (fail closed)")
	}

	digest, err := hashToolchainExecutables(goroot, gotooldir)
	if err != nil {
		return ToolchainPin{}, fmt.Errorf("runner: resolve toolchain: %w", err)
	}

	return ToolchainPin{
		ROBinds: []string{goroot, gomodcache},
		Env: map[string]string{
			"GOROOT":      goroot,
			"CGO_ENABLED": "0",
			"GOTOOLCHAIN": "local",
		},
		HashDigest: digest,
	}, nil
}

// goEnvJSON runs `go env -json <vars...>` and parses the result.
func goEnvJSON(goBinary string, vars ...string) (map[string]string, error) {
	args := append([]string{"env", "-json"}, vars...)
	out, err := exec.Command(goBinary, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("go env: %w", err)
	}
	var m map[string]string
	if err := json.Unmarshal(out, &m); err != nil {
		return nil, fmt.Errorf("go env: parse output: %w", err)
	}
	return m, nil
}

// hashToolchainExecutables computes a deterministic digest over the
// regular files directly inside gotooldir plus toolchainExecutables
// (resolved relative to goroot, skipped if absent) — a small, bounded set
// (measured ~8 files, ~60MB on the reference deployment host), unlike
// GOMODCACHE's own unbounded module-cache size, which is why this hash
// covers only these executables and not the whole toolchain tree.
func hashToolchainExecutables(goroot, gotooldir string) (string, error) {
	var files []string
	entries, err := os.ReadDir(gotooldir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", gotooldir, err)
	}
	for _, e := range entries {
		if e.Type().IsRegular() {
			files = append(files, filepath.Join(gotooldir, e.Name()))
		}
	}
	for _, rel := range toolchainExecutables {
		p := filepath.Join(goroot, rel)
		if _, statErr := os.Lstat(p); statErr == nil {
			files = append(files, p)
		}
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no toolchain executables found under %s", gotooldir)
	}
	sort.Strings(files)

	h := sha256.New()
	for _, f := range files {
		fh, openErr := openRegularNoFollow(f)
		if openErr != nil {
			return "", fmt.Errorf("%s: %w (fail closed)", f, openErr)
		}
		data, readErr := io.ReadAll(fh)
		fh.Close()
		if readErr != nil {
			return "", readErr
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(h, "%s\x00%s\n", filepath.Base(f), hex.EncodeToString(sum[:]))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
