// Redaction is a trust boundary (HARDQ C1): expected values are literals
// from the contract, not recomputed.
package redact

import (
	"strings"
	"testing"
)

func TestKnownValueReplacedEverywhere(t *testing.T) {
	r := NewKnownRefs(map[string]string{"tg_token": "123:ABC"})
	out := string(r.Redact([]byte(`token=123:ABC and again "123:ABC"`)))
	if strings.Contains(out, "123:ABC") {
		t.Fatalf("secret survived: %s", out)
	}
	if strings.Count(out, "[REDACTED:tg_token]") != 2 {
		t.Fatalf("want both occurrences replaced: %s", out)
	}
}

func TestLongerSecretWinsOverOverlappingShorter(t *testing.T) {
	r := NewKnownRefs(map[string]string{"short": "SECRET", "long": "SECRET-EXTENDED"})
	out := string(r.Redact([]byte("x SECRET-EXTENDED y")))
	if !strings.Contains(out, "[REDACTED:long]") {
		t.Fatalf("longest-first ordering broken: %s", out)
	}
	if strings.Contains(out, "EXTENDED") {
		t.Fatalf("tail of the long secret leaked: %s", out)
	}
}

func TestEmptyValueIgnored(t *testing.T) {
	r := NewKnownRefs(map[string]string{"empty": ""})
	in := "untouched content"
	if got := string(r.Redact([]byte(in))); got != in {
		t.Fatalf("empty secret mangled input: %q", got)
	}
}
