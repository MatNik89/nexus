// Command probehelper performs single, deliberate actions from INSIDE the
// sandbox so the hostile conformance suite can observe the boundary from
// within. It is a static CGO_ENABLED=0 binary bound into the sandbox by the
// probe runner. Subcommands exit 0 when the action SUCCEEDED and 1 when it
// was denied; the suite decides which outcome is the passing one per case.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		fail("usage: probehelper <readfile|writefile|dial|exec|spawn-sleep|printenv> ...")
	}
	switch os.Args[1] {
	case "readfile": // readfile <path> — succeeds only if content is readable
		b, err := os.ReadFile(arg(2))
		if err != nil {
			fail("read denied: %v", err)
		}
		fmt.Printf("read %d bytes\n", len(b))
	case "writefile": // writefile <path> <content>
		if err := os.WriteFile(arg(2), []byte(arg(3)), 0o644); err != nil {
			fail("write denied: %v", err)
		}
		fmt.Println("write ok")
	case "dial": // dial <host:port> — TCP connect with short timeout
		conn, err := net.DialTimeout("tcp", arg(2), 3*time.Second)
		if err != nil {
			fail("dial denied: %v", err)
		}
		conn.Close()
		fmt.Println("dial ok")
	case "exec": // exec <path> [args...] — run a child binary
		cmd := exec.Command(arg(2), os.Args[3:]...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fail("exec denied: %v", err)
		}
		fmt.Println("exec ok")
	case "spawn-sleep": // spawn-sleep <seconds> — detach a sleeping child, print its PID
		secs, err := strconv.Atoi(arg(2))
		if err != nil {
			fail("bad seconds: %v", err)
		}
		cmd := exec.Command("/proc/self/exe", "sleep", strconv.Itoa(secs))
		if err := cmd.Start(); err != nil {
			fail("spawn denied: %v", err)
		}
		fmt.Printf("child %d\n", cmd.Process.Pid)
		time.Sleep(time.Duration(secs) * time.Second) // stay alive with the child
	case "sleep":
		secs, _ := strconv.Atoi(arg(2))
		time.Sleep(time.Duration(secs) * time.Second)
	case "syscall-ptrace": // attempts PTRACE_TRACEME; exit 0 = allowed, 1 = denied
		sentinel, err := ptraceTraceme()
		if err != nil {
			if sentinel != "" {
				fmt.Fprintln(os.Stderr, sentinel)
				os.Exit(1)
			}
			fail("ptrace failed differently: %v", err)
		}
		fmt.Println("ptrace ok")
	case "print": // print <text> — echoes argv to stdout (redaction fixture)
		fmt.Println(strings.Join(os.Args[2:], " "))
	case "spew": // spew <bytes> — floods stdout (bounded-capture fixture)
		n, err := strconv.Atoi(os.Args[2])
		if err != nil {
			fail("spew: %v", err)
		}
		chunk := bytes.Repeat([]byte("x"), 4096)
		for written := 0; written < n; written += len(chunk) {
			os.Stdout.Write(chunk)
		}
	case "printenv": // printenv <NAME> — prints the value, or fails if unset
		v, ok := os.LookupEnv(arg(2))
		if !ok {
			fail("env %s not set", arg(2))
		}
		fmt.Println(v)
	case "cat-stdin": // cat-stdin — echoes each stdin line back on stdout,
		// prefixed "echo: ", until EOF (InteractiveProcess fixture: proves
		// a live stdin write is actually visible to the sandboxed child,
		// and a live stdout read sees each reply before EOF, not just a
		// buffer inspected after the process exits).
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			fmt.Printf("echo: %s\n", scanner.Text())
			os.Stdout.Sync()
		}
	case "hang": // never exits — timeout/cleanup fixture. NOT select{}: an
		// empty select trips Go's deadlock detector and self-terminates,
		// making the timeout test vacuous (Phase-0 r2 kilo #1).
		for {
			time.Sleep(time.Hour)
		}
	default:
		fail("unknown subcommand %q", os.Args[1])
	}
}

func arg(i int) string {
	if len(os.Args) <= i {
		fail("missing argument %d", i)
	}
	return os.Args[i]
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
