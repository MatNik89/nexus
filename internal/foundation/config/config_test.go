// T09 RED table (tasks-P0): table-driven precedence; invalid config →
// startup refuses; bounds — egress "*" / sandbox-off attempts rejected
// (config only NARROWS the kernel floor). Anchored to E11 and the ledger.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func noEnv(string) (string, bool) { return "", false }

func TestPrecedenceTable(t *testing.T) {
	dir := t.TempDir()
	global := write(t, dir, "global.json", `{"provider_model":"from-global","provider_base_url":"https://g"}`)
	project := write(t, dir, "project.json", `{"provider_model":"from-project"}`)
	env := func(k string) (string, bool) {
		if k == "NEXUS_CFG_PROVIDER_BASE_URL" {
			return "https://env", true
		}
		return "", false
	}
	res, err := Resolve(global, project, env, map[string]string{"default_profile": "work"})
	if err != nil {
		t.Fatal(err)
	}
	// project beats global; env beats files; cli beats all; defaults fill.
	if res.Config.ProviderModel != "from-project" {
		t.Fatalf("project must beat global: %q", res.Config.ProviderModel)
	}
	if res.Config.ProviderBaseURL != "https://env" {
		t.Fatalf("env must beat files: %q", res.Config.ProviderBaseURL)
	}
	if string(res.Config.DefaultProfile) != "work" {
		t.Fatalf("cli must beat all: %q", res.Config.DefaultProfile)
	}
	if res.Config.ProviderKeyEnv != "NEXUS_API_KEY" {
		t.Fatalf("default must survive: %q", res.Config.ProviderKeyEnv)
	}
	// Origin tracking.
	if res.Origins["provider_model"] != OriginProject ||
		res.Origins["provider_base_url"] != OriginEnv ||
		res.Origins["default_profile"] != OriginCLI ||
		res.Origins["provider_key_env"] != OriginDefault {
		t.Fatalf("origin tracking wrong: %+v", res.Origins)
	}
}

func TestUnknownKeyRefusedBeforeMerge(t *testing.T) {
	dir := t.TempDir()
	global := write(t, dir, "g.json", `{"totally_new_knob": true}`)
	if _, err := Resolve(global, filepath.Join(dir, "missing.json"), noEnv, nil); err == nil {
		t.Fatal("unknown config key accepted")
	}
	if _, err := Resolve(filepath.Join(dir, "missing.json"), filepath.Join(dir, "m2.json"), noEnv,
		map[string]string{"nope": "x"}); err == nil {
		t.Fatal("unknown CLI key accepted")
	}
}

func TestInvalidJSONRefused(t *testing.T) {
	dir := t.TempDir()
	global := write(t, dir, "g.json", `{broken`)
	if _, err := Resolve(global, filepath.Join(dir, "missing.json"), noEnv, nil); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

// The ledger's literal bounds cases: egress "*" and sandbox-below-floor.
func TestBoundsEgressWildcardRejected(t *testing.T) {
	dir := t.TempDir()
	global := write(t, dir, "g.json", `{"egress_allow":["api.telegram.org","*"]}`)
	_, err := Resolve(global, filepath.Join(dir, "missing.json"), noEnv, nil)
	if err == nil || !strings.Contains(err.Error(), "widens the kernel floor") {
		t.Fatalf("egress wildcard must be rejected as a floor widening: %v", err)
	}
}

func TestBoundsSandboxCannotBeDisabled(t *testing.T) {
	dir := t.TempDir()
	global := write(t, dir, "g.json", `{"sandbox_disabled": true}`)
	if _, err := Resolve(global, filepath.Join(dir, "missing.json"), noEnv, nil); err == nil {
		t.Fatal("sandbox_disabled accepted — configuration widened the kernel floor")
	}
}

func TestMissingFilesAreEmptyLayers(t *testing.T) {
	dir := t.TempDir()
	res, err := Resolve(filepath.Join(dir, "no1.json"), filepath.Join(dir, "no2.json"), noEnv, nil)
	if err != nil {
		t.Fatalf("missing files must be empty layers: %v", err)
	}
	if string(res.Config.DefaultProfile) != "private" {
		t.Fatalf("defaults not applied: %+v", res.Config)
	}
}

func TestInvalidProfileRefused(t *testing.T) {
	if _, err := Resolve("/nonexistent", "/nonexistent", noEnv,
		map[string]string{"default_profile": "bad\x00id"}); err == nil {
		t.Fatal("control-char profile id accepted")
	}
}
