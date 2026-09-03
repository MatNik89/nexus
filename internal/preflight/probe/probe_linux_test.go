//go:build linux

// Hostile conformance suite v1 (tasks-P0 T02) against the REAL bwrap backend
// on the REAL host, through the UNMODIFIED production Prepare/Start path.
// Spec anchors: PRD §6 item 6, HARDQ B4 (synthetic content-pinned closure),
// HARDQ D1, T02 RED (fail-closed availability + hazard-safe negative control
// per boundary: FS canary, loopback sink, test-owned process tree, seccomp
// loosening). Phase-0 review fixes: codex #1-#5, #7, #8, #12; kilo #2, #3.
package probe

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func helperPath(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "probehelper")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/MatNik89/nexus/cmd/probehelper")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building probehelper: %v\n%s", err, out)
	}
	return bin
}

func mustDetect(t *testing.T) Availability {
	t.Helper()
	av, err := Detect()
	if err != nil {
		t.Skipf("bwrap unavailable (fail-closed path covered by TestDetect*): %v", err)
	}
	return av
}

// wdir returns a guard-compliant disposable workdir (0700 under TempDir):
// t.TempDir() subdirs are 0775 on umask-002 hosts and the guard now rejects
// group-writable grants.
func wdir(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "wd")
	if err := os.Mkdir(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

func run(t *testing.T, av Availability, spec Spec) (string, error) {
	t.Helper()
	if spec.Timeout == 0 {
		spec.Timeout = 15 * time.Second
	}
	return Run(av, spec)
}

// --- availability + floor fail-closed ---

func TestDetectFailClosedWhenBwrapAbsent(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Detect(); err == nil || !strings.Contains(err.Error(), "SANDBOX_CAPABILITY_UNAVAILABLE") {
		t.Fatalf("want fail-closed UNAVAILABLE, got: %v", err)
	}
}

func TestDetectRejectsBelowFloorVersion(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "bwrap")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho bubblewrap 0.5.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if _, err := Detect(); err == nil || !strings.Contains(err.Error(), "below floor") {
		t.Fatalf("bwrap 0.5.0 must be rejected below floor %s, got: %v", MinBwrapVersion, err)
	}
}

func TestFloorProbeFunctionalOnThisHost(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	if err := FloorProbe(av, hp, []string{"syscall-ptrace"}); err != nil {
		t.Fatalf("kernel floor probe failed on the deployment host: %v", err)
	}
}

// --- FS_RO boundary ---

// The PRD-literal case: /etc/shadow must exist host-side (precondition, so
// the test cannot pass vacuously on a shadow-less host) and be unreadable
// from inside.
func TestShadowNotReadableInsideSandbox(t *testing.T) {
	av := mustDetect(t)
	if _, err := os.Stat("/etc/shadow"); err != nil {
		t.Fatalf("precondition: /etc/shadow must exist on the host: %v", err)
	}
	hp := helperPath(t)
	out, err := run(t, av, Spec{Target: hp, Args: []string{"readfile", "/etc/shadow"}, WorkDir: wdir(t)})
	if err == nil {
		t.Fatalf("/etc/shadow READABLE inside the sandbox:\n%s", out)
	}
}

// The red-capable pair (codex #4): the SAME deny assertion against a
// host-readable canary outside the closure...
func TestCanaryOutsideClosureNotReadable(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	canaryDir := t.TempDir()
	canary := filepath.Join(canaryDir, "canary.txt")
	if err := os.WriteFile(canary, []byte("CANARY"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, av, Spec{Target: hp, Args: []string{"readfile", canary}, WorkDir: wdir(t)})
	if err == nil {
		t.Fatalf("canary outside the closure was readable — FS isolation broken:\n%s", out)
	}
}

// ...must flip to readable when the RO boundary is loosened (negative
// control, test-owned dir only).
func TestNegativeControlCanaryReadableWhenROLoosened(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	canaryDir := t.TempDir()
	canary := filepath.Join(canaryDir, "canary.txt")
	if err := os.WriteFile(canary, []byte("CANARY"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := Spec{Target: hp, Args: []string{"readfile", canary}, WorkDir: wdir(t)}
	spec.loosen.roBind = canaryDir
	out, err := run(t, av, spec)
	if err != nil {
		t.Fatalf("negative control broken — loosened RO did not expose the canary (deny assertion would be vacuous): %v\n%s", err, out)
	}
}

// --- FS_RW ---

func TestWorkdirWritable(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	wd := wdir(t)
	out, err := run(t, av, Spec{Target: hp, Args: []string{"writefile", "/work/out.txt", "hello"}, WorkDir: wd})
	if err != nil {
		t.Fatalf("write in bound workdir must succeed: %v\n%s", err, out)
	}
	if b, err := os.ReadFile(filepath.Join(wd, "out.txt")); err != nil || string(b) != "hello" {
		t.Fatalf("host must observe the sandbox write: %v %q", err, b)
	}
}

// --- NET_DENY ---

func TestNetworkDeniedInsideSandbox(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	out, err := run(t, av, Spec{Target: hp, Args: []string{"dial", ln.Addr().String()}, WorkDir: wdir(t)})
	if err == nil {
		t.Fatalf("sandboxed dial SUCCEEDED — egress boundary broken:\n%s", out)
	}
}

func TestNegativeControlDialSucceedsWhenNetLoosened(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	spec := Spec{Target: hp, Args: []string{"dial", ln.Addr().String()}, WorkDir: wdir(t)}
	spec.loosen.net = true
	out, err := run(t, av, spec)
	if err != nil {
		t.Fatalf("negative control broken — loosened net did not allow the dial: %v\n%s", err, out)
	}
}

// --- SYSCALL floor ---

func TestSyscallFloorDeniesPtrace(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	out, err := run(t, av, Spec{Target: hp, Args: []string{"syscall-ptrace"}, WorkDir: wdir(t)})
	if err == nil {
		t.Fatalf("ptrace ALLOWED under the syscall floor:\n%s", out)
	}
	if !strings.Contains(out, PtraceDeniedSentinel) {
		t.Fatalf("expected the exact denial sentinel, got: %v\n%s", err, out)
	}
}

func TestNegativeControlPtraceAllowedWhenSeccompLoosened(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	spec := Spec{Target: hp, Args: []string{"syscall-ptrace"}, WorkDir: wdir(t)}
	spec.loosen.seccomp = true
	out, err := run(t, av, spec)
	if err != nil {
		t.Fatalf("negative control broken — without the filter ptrace must succeed: %v\n%s", err, out)
	}
}

// --- ELF-only launcher + synthetic closure ---

func TestDynamicELFRuns(t *testing.T) {
	av := mustDetect(t)
	out, err := run(t, av, Spec{Target: "/bin/ls", Args: []string{"/"}, WorkDir: wdir(t)})
	if err != nil {
		t.Fatalf("dynamically linked /bin/ls must run under the pinned closure: %v\n%s", err, out)
	}
}

func TestShebangRejectedBeforeSandbox(t *testing.T) {
	av := mustDetect(t)
	script := filepath.Join(t.TempDir(), "evil.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho pwned\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(av, Spec{Target: script}); err == nil {
		t.Fatal("shebang script accepted as launch target — must be rejected (ELF-only)")
	}
}

// The fixed hostile case (codex #2 repro): a binary that EXISTS on the host
// under /usr is still not executable inside, because /usr is not bound.
func TestUndeclaredChildExecFailsForUsrBinary(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	out, err := run(t, av, Spec{Target: hp, Args: []string{"exec", "/usr/bin/id"}, WorkDir: wdir(t)})
	if err == nil {
		t.Fatalf("undeclared /usr/bin/id EXECUTED inside the sandbox — closure too wide:\n%s", out)
	}
}

// Content pinning: modifying the target after Prepare must not change what
// runs (the bytes were pinned at hash time via --ro-bind-data).
func TestTargetSwapAfterPrepareIsInert(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	copyPath := filepath.Join(t.TempDir(), "target")
	if b, err := os.ReadFile(hp); err != nil {
		t.Fatal(err)
	} else if err := os.WriteFile(copyPath, b, 0o755); err != nil {
		t.Fatal(err)
	}
	h, err := Prepare(av, Spec{Target: copyPath, Args: []string{"writefile", "/work/x", "ok"}, WorkDir: wdir(t), Timeout: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	// Swap the file on disk between Prepare and Start.
	if err := os.WriteFile(copyPath, []byte("#!/bin/sh\necho pwned\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.Start(); err != nil {
		t.Fatalf("pinned launch must still start: %v", err)
	}
	if err := h.Wait(); err != nil {
		t.Fatalf("pinned bytes must run unaffected by the swap: %v", err)
	}
}

// --- PROC_TREE: production kill path, host-side (pid,starttime) oracle ---

// descendants returns host (pid, starttime) pairs for every live process
// whose ancestry chain reaches root.
func descendants(t *testing.T, root int) map[string]bool {
	t.Helper()
	type pinfo struct {
		ppid  int
		start string
	}
	procs := map[int]pinfo{}
	entries, _ := os.ReadDir("/proc")
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue
		}
		// stat: pid (comm) state ppid ... field 22 = starttime
		s := string(b)
		close := strings.LastIndex(s, ")")
		if close < 0 {
			continue
		}
		fields := strings.Fields(s[close+1:])
		if len(fields) < 20 {
			continue
		}
		ppid, _ := strconv.Atoi(fields[1])
		procs[pid] = pinfo{ppid: ppid, start: fields[19]}
	}
	out := map[string]bool{}
	var mark func(int)
	mark = func(p int) {
		for pid, info := range procs {
			if info.ppid == p {
				out[fmt.Sprintf("%d:%s", pid, info.start)] = true
				mark(pid)
			}
		}
	}
	mark(root)
	return out
}

func stillAlive(keys map[string]bool) []string {
	var alive []string
	for k := range keys {
		parts := strings.SplitN(k, ":", 2)
		b, err := os.ReadFile("/proc/" + parts[0] + "/stat")
		if err != nil {
			continue
		}
		s := string(b)
		close := strings.LastIndex(s, ")")
		fields := strings.Fields(s[close+1:])
		if len(fields) >= 20 && fields[19] == parts[1] {
			alive = append(alive, k)
		}
	}
	return alive
}

func TestKillReapsWholeTree(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	h, err := Prepare(av, Spec{Target: hp, Args: []string{"spawn-sleep", "30"}, WorkDir: wdir(t), Timeout: 60 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	tree := descendants(t, h.Pid())
	if len(tree) == 0 {
		t.Fatal("oracle vacuous: no descendants observed before kill (spawn failed?)")
	}
	h.Kill()
	h.Wait()
	time.Sleep(500 * time.Millisecond)
	if alive := stillAlive(tree); len(alive) != 0 {
		t.Fatalf("descendants survived the production kill: %v", alive)
	}
}

func TestNegativeControlChildSurvivesWhenPIDNSLoosened(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	spec := Spec{Target: hp, Args: []string{"spawn-sleep", "30"}, WorkDir: wdir(t), Timeout: 60 * time.Second}
	spec.loosen.pidNS = true
	h, err := Prepare(av, spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1500 * time.Millisecond)
	tree := descendants(t, h.Pid())
	if len(tree) == 0 {
		t.Fatal("no descendants observed before kill")
	}
	h.cmd.Process.Kill() // kill ONLY the leader, not the group (in-package test access)
	h.cmd.Wait()
	h.Close()
	time.Sleep(500 * time.Millisecond)
	alive := stillAlive(tree)
	// Clean up the test-owned survivors regardless of outcome.
	defer func() {
		for _, k := range alive {
			pid, _ := strconv.Atoi(strings.SplitN(k, ":", 2)[0])
			if p, err := os.FindProcess(pid); err == nil {
				p.Kill()
			}
		}
	}()
	if len(alive) == 0 {
		t.Fatal("negative control broken — loosened PID isolation still reaped everything " +
			"(the PROC_TREE assertion would be vacuous)")
	}
}

// --- bounded cleanup of a hanging target (codex #12) ---

func TestHangingTargetCleanedUpWithinTimeout(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	start := time.Now()
	h, err := Prepare(av, Spec{Target: hp, Args: []string{"hang"}, WorkDir: wdir(t), Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	tree := descendants(t, h.Pid())
	h.Wait() // must return via context timeout, not hang
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Fatalf("hanging target not cleaned up within bounds: %v", elapsed)
	}
	time.Sleep(500 * time.Millisecond)
	if alive := stillAlive(tree); len(alive) != 0 {
		t.Fatalf("hanging target left survivors: %v", alive)
	}
}

// --- sealed-memfd pin: post-Prepare writes must be impossible (r2 codex #6) ---

func TestMemfdSealedAgainstPostPrepareWrite(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	h, err := Prepare(av, Spec{Target: hp, Args: []string{"sleep", "0"}, WorkDir: wdir(t), Timeout: 15 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if len(h.closers) == 0 {
		t.Fatal("no pinned closure files")
	}
	if _, err := h.closers[0].WriteAt([]byte{0x00}, 0); err == nil {
		t.Fatal("pinned memfd accepted a post-Prepare write — seals not effective")
	}
}

// --- fd hygiene: repeated launches must not leak descriptors (r2 codex #8) ---

func countFDs(t *testing.T) int {
	t.Helper()
	ents, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(ents)
}

func TestNoFDLeakAcrossRuns(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	spec := Spec{Target: hp, Args: []string{"sleep", "0"}, WorkDir: wdir(t)}
	if _, err := run(t, av, spec); err != nil { // warm-up
		t.Fatal(err)
	}
	before := countFDs(t)
	for i := 0; i < 5; i++ {
		if _, err := run(t, av, spec); err != nil {
			t.Fatal(err)
		}
	}
	if after := countFDs(t); after > before+2 {
		t.Fatalf("descriptor leak across runs: %d -> %d", before, after)
	}
}

// --- RW grant guard: hazardous workdirs refused (r2 codex #2) ---

func TestWorkdirRootRefused(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	if _, err := Prepare(av, Spec{Target: hp, Args: []string{"sleep", "0"}, WorkDir: "/"}); err == nil {
		t.Fatal("WorkDir '/' accepted — the whole host would be RW under /work")
	}
	link := filepath.Join(t.TempDir(), "to-root")
	if err := os.Symlink("/", link); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(av, Spec{Target: hp, Args: []string{"sleep", "0"}, WorkDir: link}); err == nil {
		t.Fatal("symlink-to-/ workdir accepted")
	}
}

// --- floor-probe sentinel strictness (r3 codex #1) ---

// A bwrap that fails BEFORE launching the target — with "Permission denied"
// in its diagnostics — must yield floor FAILURE, never success.
func TestFloorProbeRejectsPrelaunchPermissionDenied(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "bwrap")
	script := "#!/bin/sh\necho 'bwrap: setup failed: Permission denied' >&2\nexit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	hp := helperPath(t)
	av := Availability{BwrapPath: fake, BwrapVersion: "bubblewrap fake"}
	if err := FloorProbe(av, hp, []string{"syscall-ptrace"}); err == nil {
		t.Fatal("prelaunch 'Permission denied' diagnostics accepted as a floor pass")
	}
}

// --- workdir guard: allowed-root + writability REDs (r3 codex #2) ---

func TestWorkdirOutsideAllowedRootsRefused(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if _, err := Prepare(av, Spec{Target: hp, Args: []string{"sleep", "0"}, WorkDir: home}); err == nil {
		t.Fatal("home directory accepted as a disposable RW grant")
	}
}

func TestWorkdirGroupWritableRefused(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	d := filepath.Join(t.TempDir(), "gw")
	if err := os.Mkdir(d, 0o770); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(av, Spec{Target: hp, Args: []string{"sleep", "0"}, WorkDir: d}); err == nil {
		t.Fatal("group-writable workdir accepted")
	}
}

// --- handle lifecycle (r3 codex #5) ---

func TestHandleRejectsRepeatStart(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	h, err := Prepare(av, Spec{Target: hp, Args: []string{"sleep", "2"}, WorkDir: wdir(t), Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Start(); err != nil {
		t.Fatal(err)
	}
	if err := h.Start(); err == nil {
		t.Fatal("repeat Start accepted — first launch ownership lost")
	}
	if err := h.Wait(); err != nil { // first launch must still be reapable
		t.Fatalf("first launch not cleanly reaped after rejected repeat Start: %v", err)
	}
}

func TestHandleRejectsStartAfterClose(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	h, err := Prepare(av, Spec{Target: hp, Args: []string{"sleep", "0"}, WorkDir: wdir(t)})
	if err != nil {
		t.Fatal(err)
	}
	h.Close()
	if err := h.Start(); err == nil {
		t.Fatal("Start after Close accepted")
	}
}

// --- resolver semantics (r3 codex #3 / kilo #9-#11) ---

func TestExpandRunpathsSemantics(t *testing.T) {
	got := expandRunpaths("/opt/app/bin", []string{"$ORIGIN/../lib:${ORIGIN}/x::/abs", "$LIB/z", "lib:../rel"})
	want := []string{"/opt/app/lib", "/opt/app/bin/x", "/abs"} // relative components dropped (never cwd)
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("component %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

// End-to-end $ORIGIN fixture (the r3 codex repro shape); needs a C compiler.
func TestOriginRunpathBinaryResolves(t *testing.T) {
	av := mustDetect(t)
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler for the $ORIGIN fixture")
	}
	root := t.TempDir()
	libDir := filepath.Join(root, "lib")
	binDir := filepath.Join(root, "bin")
	for _, d := range []string{libDir, binDir} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	libC := filepath.Join(root, "foo.c")
	os.WriteFile(libC, []byte("int foo(void){return 42;}\n"), 0o644)
	if out, err := exec.Command(cc, "-shared", "-fPIC", "-o", filepath.Join(libDir, "libnexusfoo.so"), libC).CombinedOutput(); err != nil {
		t.Skipf("cc -shared failed: %v\n%s", err, out)
	}
	mainC := filepath.Join(root, "main.c")
	os.WriteFile(mainC, []byte("int foo(void); int main(void){return foo()==42?0:1;}\n"), 0o644)
	bin := filepath.Join(binDir, "app")
	if out, err := exec.Command(cc, "-o", bin, mainC,
		"-L", libDir, "-lnexusfoo", "-Wl,--enable-new-dtags,-rpath,$ORIGIN/../lib").CombinedOutput(); err != nil {
		t.Skipf("cc link failed: %v\n%s", err, out)
	}
	out, err := run(t, av, Spec{Target: bin, WorkDir: wdir(t)})
	if err != nil {
		t.Fatalf("$ORIGIN RUNPATH binary must resolve and run: %v\n%s", err, out)
	}
}

// --- rootfs is read-only: no library shadowing (r4 kilo #7) ---

// buildDynamicWriter compiles a DYNAMIC C fixture (its closure materializes
// /nexus-libs and /lib inside), so the write attempts hit real read-only
// directories — a static helper got ENOENT and proved nothing (r5 codex #2).
func buildDynamicWriter(t *testing.T) string {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler for the dynamic writer fixture")
	}
	root := t.TempDir()
	src := filepath.Join(root, "w.c")
	os.WriteFile(src, []byte(`#include <stdio.h>
int main(int argc, char **argv){ if(argc<2) return 2; FILE *f=fopen(argv[1],"w"); if(!f){perror("open"); return 1;} fputs("evil",f); fclose(f); return 0; }
`), 0o644)
	bin := filepath.Join(root, "writer")
	if out, err := exec.Command(cc, "-o", bin, src).CombinedOutput(); err != nil {
		t.Skipf("cc: %v\n%s", err, out)
	}
	return bin
}

func TestRootfsReadOnlyPreventsLibShadowing(t *testing.T) {
	av := mustDetect(t)
	writer := buildDynamicWriter(t)
	for _, target := range []string{"/nexus-libs/libc.so.6", "/lib/evil"} {
		out, err := run(t, av, Spec{Target: writer, Args: []string{target}, WorkDir: wdir(t)})
		if err == nil {
			t.Fatalf("write into %s SUCCEEDED — shadowing possible:\n%s", target, out)
		}
		if !strings.Contains(out, "Read-only file system") {
			t.Fatalf("write into %s must fail with EROFS (read-only), got: %v\n%s", target, err, out)
		}
	}
}

// Negative control: with the remount loosened, the SAME writes succeed —
// proving the assertion above is causal, not vacuous.
func TestNegativeControlLibWriteSucceedsWithoutRemountRO(t *testing.T) {
	av := mustDetect(t)
	writer := buildDynamicWriter(t)
	spec := Spec{Target: writer, Args: []string{"/nexus-libs/injected.so"}, WorkDir: wdir(t)}
	spec.loosen.remountRW = true
	out, err := run(t, av, spec)
	if err != nil {
		t.Fatalf("negative control broken — without remount-ro the write must succeed: %v\n%s", err, out)
	}
}

func TestQueueBudgetRejectsBeforeAllocation(t *testing.T) {
	if err := checkQueueBudget(0, maxDepQueue+1); err == nil {
		t.Fatal("oversized dependency table accepted")
	}
	if err := checkQueueBudget(maxDepQueue-1, 2); err == nil {
		t.Fatal("queue overflow accepted")
	}
	if err := checkQueueBudget(0, maxDepQueue); err != nil {
		t.Fatalf("exact-capacity table refused: %v", err)
	}
}

func TestLoadBoundedHonorsAggregateAllowance(t *testing.T) {
	f := filepath.Join(t.TempDir(), "blob")
	if err := os.WriteFile(f, make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadBounded(f, 1024); err == nil {
		t.Fatal("file exceeding the remaining aggregate allowance accepted")
	}
	if _, err := loadBounded(f, 0); err == nil {
		t.Fatal("exhausted allowance accepted")
	}
	if b, err := loadBounded(f, 8192); err != nil || len(b) != 4096 {
		t.Fatalf("within-allowance load failed: %v", err)
	}
}

// --- ambient env cannot widen the workdir allowlist (r4 codex #3) ---

func TestHostileXDGRuntimeDirCannotWidenRoots(t *testing.T) {
	av := mustDetect(t)
	hp := helperPath(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home")
	}
	t.Setenv("XDG_RUNTIME_DIR", home) // hostile: home is not a valid runtime root shape
	// Fresh, uniquely named fixture — NEVER a fixed path that could already
	// exist and get recursively deleted (r5 codex #1).
	sub, err := os.MkdirTemp(home, ".nexus-probe-red-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sub)
	if _, err := Prepare(av, Spec{Target: hp, Args: []string{"sleep", "0"}, WorkDir: sub}); err == nil {
		t.Fatal("hostile XDG_RUNTIME_DIR widened the workdir allowlist to $HOME")
	}
}

// --- old-dtags transitive RPATH (r4 codex #1 fixture shape) ---

func TestTransitiveRpathBinaryResolves(t *testing.T) {
	av := mustDetect(t)
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("no C compiler")
	}
	root := t.TempDir()
	dirA := filepath.Join(root, "a")
	dirB := filepath.Join(root, "b")
	binDir := filepath.Join(root, "bin")
	for _, d := range []string{dirA, dirB, binDir} {
		os.Mkdir(d, 0o755)
	}
	os.WriteFile(filepath.Join(root, "b.c"), []byte("int bee(void){return 7;}\n"), 0o644)
	if out, err := exec.Command(cc, "-shared", "-fPIC", "-o", filepath.Join(dirB, "libnexusb.so"), filepath.Join(root, "b.c")).CombinedOutput(); err != nil {
		t.Skipf("cc: %v\n%s", err, out)
	}
	os.WriteFile(filepath.Join(root, "a.c"), []byte("int bee(void); int aye(void){return bee();}\n"), 0o644)
	// liba NEEDS libnexusb but carries NO runpath of its own.
	if out, err := exec.Command(cc, "-shared", "-fPIC", "-o", filepath.Join(dirA, "libnexusa.so"), filepath.Join(root, "a.c"), "-L", dirB, "-lnexusb").CombinedOutput(); err != nil {
		t.Skipf("cc: %v\n%s", err, out)
	}
	os.WriteFile(filepath.Join(root, "m.c"), []byte("int aye(void); int main(void){return aye()==7?0:1;}\n"), 0o644)
	bin := filepath.Join(binDir, "app")
	// main carries old-dtags RPATH covering BOTH dirs; loader semantics make
	// it apply transitively to liba's own lookup of libnexusb.
	if out, err := exec.Command(cc, "-o", bin, filepath.Join(root, "m.c"),
		"-L", dirA, "-lnexusa",
		"-Wl,--disable-new-dtags,-rpath,$ORIGIN/../a:$ORIGIN/../b").CombinedOutput(); err != nil {
		t.Skipf("cc link: %v\n%s", err, out)
	}
	out, err := run(t, av, Spec{Target: bin, WorkDir: wdir(t)})
	if err != nil {
		t.Fatalf("transitive-RPATH binary must resolve and run: %v\n%s", err, out)
	}
}
