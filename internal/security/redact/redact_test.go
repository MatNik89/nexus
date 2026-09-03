// Redaction is a trust boundary (HARDQ C1): expected values are literals
// from the contract, not recomputed.
package redact

import (
	"strings"
	"testing"
)

func mustRedact(t *testing.T, r Redactor, b []byte) string {
	t.Helper()
	out, err := r.Redact(b)
	if err != nil {
		t.Fatalf("redact failed: %v", err)
	}
	return string(out)
}

func TestKnownValueReplacedEverywhere(t *testing.T) {
	r := NewKnownRefs(map[string]string{"tg_token": "123:ABC"})
	out := (mustRedact(t, r, []byte(`token=123:ABC and again "123:ABC"`)))
	if strings.Contains(out, "123:ABC") {
		t.Fatalf("secret survived: %s", out)
	}
	if strings.Count(out, "[REDACTED:tg_token]") != 2 {
		t.Fatalf("want both occurrences replaced: %s", out)
	}
}

func TestLongerSecretWinsOverOverlappingShorter(t *testing.T) {
	r := NewKnownRefs(map[string]string{"short": "SECRET", "long": "SECRET-EXTENDED"})
	out := (mustRedact(t, r, []byte("x SECRET-EXTENDED y")))
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
	if got := string(mustRedact(t, r, []byte(in))); got != in {
		t.Fatalf("empty secret mangled input: %q", got)
	}
}

func TestUnicodeEscapeVariantRedacted(t *testing.T) {
	r := NewKnownRefs(map[string]string{"key": "secret"})
	// Semantically identical JSON spelled with a \u escape (r2 codex #5).
	encoded := `{"v":"s\u0065cret"}`
	out := (mustRedact(t, r, []byte(encoded)))
	if strings.Contains(out, "ecret") && !strings.Contains(out, "[REDACTED:key]") {
		t.Fatalf("unicode-escaped spelling bypassed redaction: %s", out)
	}
}

func TestJSONEscapedSecretRedacted(t *testing.T) {
	secret := `pa"ss\word`
	r := NewKnownRefs(map[string]string{"weird": secret})
	// As it appears inside marshaled JSON:
	encoded := `{"v":"pa\"ss\\word"}`
	out := (mustRedact(t, r, []byte(encoded)))
	if strings.Contains(out, `pa\"ss`) {
		t.Fatalf("escaped form of the secret survived: %s", out)
	}
	if !strings.Contains(out, "[REDACTED:weird]") {
		t.Fatalf("marker missing: %s", out)
	}
}

// Over-budget VALID JSON must be a typed error, never a byte-match
// fallback (r4 codex #6: escapes would slip through).
func TestOverBudgetValidJSONRejected(t *testing.T) {
	r := NewKnownRefs(map[string]string{"key": "secret"})
	big := `{"pad":"` + strings.Repeat("a", maxJSONBytes) + `","v":"s\u0065cret"}`
	if _, err := r.Redact([]byte(big)); err == nil {
		t.Fatal("over-budget valid JSON accepted (fail-open byte match)")
	}
}

func TestOverDepthValidJSONRejected(t *testing.T) {
	r := NewKnownRefs(map[string]string{"key": "secret"})
	deep := strings.Repeat("[", maxJSONDepth+2) + strings.Repeat("]", maxJSONDepth+2)
	if _, err := r.Redact([]byte(deep)); err == nil {
		t.Fatal("over-depth valid JSON accepted")
	}
}
