//go:build linux

package health

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The owner records a typed class, writes the journal-independent
// projection atomically, logs ONE redacted line per transition, and an
// invalid class fails closed to substrate.
func TestOwnerRecordsTransitionsAndProjection(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "channel_health.json")
	var log bytes.Buffer
	o, err := New(p, &log)
	if err != nil {
		t.Fatal(err)
	}
	c := time.Unix(1000, 0)
	o.SetClock(func() time.Time { return c })
	if err := o.Report("telegram.poll", ClassRemoteRejected, "http_4xx", "HTTP 401", true); err != nil {
		t.Fatal(err)
	}
	entries, err := Read(p)
	if err != nil || len(entries) != 1 || entries[0].Class != ClassRemoteRejected || !entries[0].Stopped || entries[0].Healthy {
		t.Fatalf("projection: %+v %v", entries, err)
	}
	if !strings.Contains(log.String(), "remote_rejected") || strings.Count(log.String(), "\n") != 1 {
		t.Fatalf("log: %q", log.String())
	}
	// Same state again: no second line.
	_ = o.Report("telegram.poll", ClassRemoteRejected, "http_4xx", "HTTP 401", true)
	if strings.Count(log.String(), "\n") != 1 {
		t.Fatalf("duplicate transition logged: %q", log.String())
	}
	if err := o.Healthy("telegram.poll"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "RECOVERED") {
		t.Fatal("recovery not logged")
	}
	entries, _ = Read(p)
	if len(entries) != 1 || !entries[0].Healthy {
		t.Fatalf("recovery not projected: %+v", entries)
	}
	// Unclassified -> substrate (fail closed).
	_ = o.Report("x", Class("bogus"), "", "", false)
	if o.Snapshot()["x"].Class != ClassSubstrate {
		t.Fatal("invalid class not coerced to substrate")
	}
	// Missing projection is (nil, nil); malformed is an error.
	if e, err := Read(filepath.Join(dir, "none.json")); e != nil || err != nil {
		t.Fatalf("missing projection: %v %v", e, err)
	}
	os.WriteFile(p, []byte("{not json"), 0o600)
	if _, err := Read(p); err == nil {
		t.Fatal("malformed projection accepted")
	}
	if _, err := New("", nil); err == nil {
		t.Fatal("empty path accepted")
	}
}
