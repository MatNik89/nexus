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

var noEnv []string

func TestPrecedenceTable(t *testing.T) {
	dir := t.TempDir()
	global := write(t, dir, "global.json", `{"provider_model":"from-global","provider_base_url":"https://g"}`)
	project := write(t, dir, "project.json", `{"provider_model":"from-project"}`)
	env := []string{"NEXUS_CFG_PROVIDER_BASE_URL=https://env", "UNRELATED=1"}
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

// F9: duplicate JSON member names are ambiguous (last-value-wins) and must be
// rejected before decoding — a second, security-sensitive value must never
// silently win.
func TestDuplicateJSONKeysRejected(t *testing.T) {
	dir := t.TempDir()
	g := write(t, dir, "dup.json", `{"provider_base_url":"https://a","provider_base_url":"https://b"}`)
	if _, err := Resolve(g, filepath.Join(dir, "missing.json"), noEnv, map[string]string{"default_profile": "work"}); err == nil {
		t.Fatalf("duplicate JSON keys accepted (ambiguous config)")
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

// Unknown NEXUS_CFG_* env vars fail closed like every other layer
// (Phase-1B codex #12: name-probing made them invisible).
func TestUnknownEnvVarRefused(t *testing.T) {
	_, err := Resolve("/nonexistent", "/nonexistent",
		[]string{"NEXUS_CFG_TOTALLY_NEW=1"}, nil)
	if err == nil {
		t.Fatal("unknown NEXUS_CFG_ variable accepted")
	}
}

// Per-key schema in EVERY layer (codex #11): wrong JSON types and
// non-literal booleans are refused, never coerced.
func TestPerKeySchemaStrict(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"bool-as-model":     `{"provider_model": true}`,
		"bool-as-egress":    `{"egress_allow": true}`,
		"string-as-sandbox": `{"sandbox_disabled": "yes"}`,
		"number-as-url":     `{"provider_base_url": 42}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			g := write(t, dir, name+".json", content)
			if _, err := Resolve(g, "/nonexistent", noEnv, nil); err == nil {
				t.Fatalf("mistyped value accepted: %s", content)
			}
		})
	}
	// Env/CLI: "TRUE"/"1" are NOT booleans (previously silently false).
	if _, err := Resolve("/nonexistent", "/nonexistent",
		[]string{"NEXUS_CFG_SANDBOX_DISABLED=TRUE"}, nil); err == nil {
		t.Fatal("non-literal boolean accepted from env")
	}
	if _, err := Resolve("/nonexistent", "/nonexistent", noEnv,
		map[string]string{"sandbox_disabled": "1"}); err == nil {
		t.Fatal("non-literal boolean accepted from CLI")
	}
}

// Egress entries stay a real array: whitespace and empty entries refused
// in every layer (codex #13 / kilo #3).
func TestEgressListHygiene(t *testing.T) {
	dir := t.TempDir()
	g := write(t, dir, "g.json", `{"egress_allow":["api.telegram.org", " padded.example"]}`)
	if _, err := Resolve(g, "/nonexistent", noEnv, nil); err == nil {
		t.Fatal("whitespace-padded host accepted from file")
	}
	if _, err := Resolve("/nonexistent", "/nonexistent",
		[]string{"NEXUS_CFG_EGRESS_ALLOW=a.example,, b.example"}, nil); err == nil {
		t.Fatal("empty/padded CSV entries accepted from env")
	}
	res, err := Resolve("/nonexistent", "/nonexistent",
		[]string{"NEXUS_CFG_EGRESS_ALLOW=a.example,b.example"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Config.EgressAllow) != 2 || res.Config.EgressAllow[1] != "b.example" {
		t.Fatalf("clean CSV mis-parsed: %v", res.Config.EgressAllow)
	}
}

// Bounds are enforced regardless of which layer supplied the value
// (codex #20: file-only bounds REDs were too narrow).
func TestBoundsEnforcedFromEveryLayer(t *testing.T) {
	if _, err := Resolve("/nonexistent", "/nonexistent",
		[]string{"NEXUS_CFG_EGRESS_ALLOW=*"}, nil); err == nil {
		t.Fatal("egress wildcard accepted from env")
	}
	if _, err := Resolve("/nonexistent", "/nonexistent", noEnv,
		map[string]string{"egress_allow": "api.x,*"}); err == nil {
		t.Fatal("egress wildcard accepted from CLI")
	}
	if _, err := Resolve("/nonexistent", "/nonexistent",
		[]string{"NEXUS_CFG_SANDBOX_DISABLED=true"}, nil); err == nil {
		t.Fatal("sandbox_disabled accepted from env")
	}
}

// Env names are canonical uppercase, once each: case-folded aliases and
// duplicates are refused in BOTH orders — no order-dependent overwrite
// channel (Phase-1B-r2 codex #9).
func TestEnvNamesCanonicalAndUnique(t *testing.T) {
	if _, err := Resolve("/nonexistent", "/nonexistent",
		[]string{"NEXUS_CFG_provider_model=b"}, nil); err == nil {
		t.Fatal("non-uppercase NEXUS_CFG_ name accepted")
	}
	for _, environ := range [][]string{
		{"NEXUS_CFG_PROVIDER_MODEL=a", "NEXUS_CFG_PROVIDER_MODEL=b"},
		{"NEXUS_CFG_PROVIDER_MODEL=b", "NEXUS_CFG_PROVIDER_MODEL=a"},
	} {
		if _, err := Resolve("/nonexistent", "/nonexistent", environ, nil); err == nil {
			t.Fatal("duplicate env variable accepted (last-write-wins)")
		}
	}
}

// Rejected raw values are NEVER echoed into diagnostics (Phase-1B-r2
// codex #10): a secret mistyped into a config key must not appear in the
// startup error.
func TestRejectedValueNotEchoed(t *testing.T) {
	const canary = "sk-SECRET-CANARY-VALUE"
	cases := map[string]func() error{
		"env-bool": func() error {
			_, err := Resolve("/nonexistent", "/nonexistent",
				[]string{"NEXUS_CFG_SANDBOX_DISABLED=" + canary}, nil)
			return err
		},
		"cli-bool": func() error {
			_, err := Resolve("/nonexistent", "/nonexistent", noEnv,
				map[string]string{"sandbox_disabled": canary})
			return err
		},
		"env-list-padded": func() error {
			_, err := Resolve("/nonexistent", "/nonexistent",
				[]string{"NEXUS_CFG_EGRESS_ALLOW=ok.example, " + canary}, nil)
			return err
		},
	}
	for name, do := range cases {
		t.Run(name, func(t *testing.T) {
			err := do()
			if err == nil {
				t.Fatal("invalid value accepted")
			}
			if strings.Contains(err.Error(), canary) {
				t.Fatalf("rejected raw value echoed into diagnostics: %v", err)
			}
		})
	}
}

// telegram_api_base is production-or-loopback ONLY (T27 codex #4: the
// bot token rides in the URL path — an arbitrary host is exfiltration).
func TestTelegramAPIBaseLoopbackOnly(t *testing.T) {
	base := Config{DefaultProfile: "private", ContextHardLimitTokens: 64000}
	ok := func(u string) error {
		c := base
		c.TelegramAPIBase = u
		return ValidateBounds(c)
	}
	for _, good := range []string{"", "https://api.telegram.org", "http://127.0.0.1:8081", "http://localhost:9000", "https://127.0.0.1:8443"} {
		if err := ok(good); err != nil {
			t.Fatalf("legit base %q rejected: %v", good, err)
		}
	}
	for _, bad := range []string{"http://evil.example.com", "https://attacker.tld/bot", "ftp://127.0.0.1", "http://10.0.0.5:1234", "not-a-url"} {
		if err := ok(bad); err == nil {
			t.Fatalf("token-exfiltration base %q accepted", bad)
		}
	}
}

// Slice C (AUDIT-FULL F5): the ONE configured context hard limit is a
// bounded positive integer. Zero and negative are REJECTED at Resolve — they
// must never reach the planner as "unlimited"; absent means the default.
func TestContextHardLimitBoundsAndDefault(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		dir := t.TempDir()
		p := filepath.Join(dir, "config.json")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := []struct {
		body    string
		wantErr bool
		want    int
	}{
		{`{}`, false, 64000},
		{`{"context_hard_limit_tokens": 12000}`, false, 12000},
		{`{"context_hard_limit_tokens": 0}`, true, 0},
		{`{"context_hard_limit_tokens": -1}`, true, 0},
		{`{"context_hard_limit_tokens": 2000001}`, true, 0},
		{`{"context_hard_limit_tokens": "abc"}`, true, 0},
		{`{"context_hard_limit_tokens": 64000.5}`, true, 0},
		{`{"context_hard_limit_tokens": 6.4e4}`, true, 0},
	}
	for _, tc := range cases {
		res, err := Resolve(write(t, tc.body), "/nonexistent", nil, nil)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%s: accepted (want rejection)", tc.body)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", tc.body, err)
		}
		if res.Config.ContextHardLimitTokens != tc.want {
			t.Fatalf("%s: limit = %d, want %d", tc.body, res.Config.ContextHardLimitTokens, tc.want)
		}
	}
	// Env layer: strict decimal only.
	if _, err := Resolve("/nonexistent", "/nonexistent", []string{"NEXUS_CFG_CONTEXT_HARD_LIMIT_TOKENS=0"}, nil); err == nil {
		t.Fatal("env zero limit accepted")
	}
	if _, err := Resolve("/nonexistent", "/nonexistent", []string{"NEXUS_CFG_CONTEXT_HARD_LIMIT_TOKENS= 1000"}, nil); err == nil {
		t.Fatal("env padded integer accepted")
	}
	res, err := Resolve("/nonexistent", "/nonexistent", []string{"NEXUS_CFG_CONTEXT_HARD_LIMIT_TOKENS=1000"}, nil)
	if err != nil || res.Config.ContextHardLimitTokens != 1000 {
		t.Fatalf("env limit: %v %d", err, res.Config.ContextHardLimitTokens)
	}
}

// Detector (S6 tool-boundary, piece 2): a profile with no configured
// coding_workspace_roots entry has no coding-tool access at all — the
// fail-closed default, never inferred from the profile id.
func TestCodingWorkspaceRootAbsentByDefault(t *testing.T) {
	dir := t.TempDir()
	res, err := Resolve(filepath.Join(dir, "missing.json"), filepath.Join(dir, "missing2.json"), noEnv, map[string]string{"default_profile": "work"})
	if err != nil {
		t.Fatal(err)
	}
	if root, ok := res.Config.CodingWorkspaceRoot("work"); ok {
		t.Fatalf("expected no root configured, got %q", root)
	}
}

// Detector: a valid "profile:absolute-path" entry resolves via
// CodingWorkspaceRoot for THAT profile only — a different profile still
// gets nothing.
func TestCodingWorkspaceRootResolvesPerProfile(t *testing.T) {
	dir := t.TempDir()
	global := write(t, dir, "g.json", `{"coding_workspace_roots":["work:/home/matej/HARNESS/nexus"]}`)
	res, err := Resolve(global, filepath.Join(dir, "missing.json"), noEnv, map[string]string{"default_profile": "work"})
	if err != nil {
		t.Fatal(err)
	}
	root, ok := res.Config.CodingWorkspaceRoot("work")
	if !ok || root != "/home/matej/HARNESS/nexus" {
		t.Fatalf("work root = %q (ok=%v), want /home/matej/HARNESS/nexus", root, ok)
	}
	if _, ok := res.Config.CodingWorkspaceRoot("private"); ok {
		t.Fatal("private profile must not inherit work's root")
	}
}

// Detector: malformed entries, wildcards, relative paths, invalid profile
// ids, and duplicate profile entries are all rejected (fail closed, same
// rigor as exec_allow).
func TestCodingWorkspaceRootsBoundsRejected(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name, body, wantErrSubstr string
	}{
		{"missing colon", `{"coding_workspace_roots":["work-no-colon"]}`, `must be "<profile_id>:<absolute path>"`},
		{"empty path", `{"coding_workspace_roots":["work:"]}`, `must be "<profile_id>:<absolute path>"`},
		{"empty profile", `{"coding_workspace_roots":[":/abs/path"]}`, `must be "<profile_id>:<absolute path>"`},
		{"relative path", `{"coding_workspace_roots":["work:relative/path"]}`, "must be an absolute path"},
		{"wildcard path", `{"coding_workspace_roots":["work:/abs/*"]}`, "must be an absolute path without wildcards"},
		{"invalid profile id", "{\"coding_workspace_roots\":[\"wor\\u0001k:/abs/path\"]}", "invalid profile id"},
		{"duplicate profile", `{"coding_workspace_roots":["work:/abs/one","work:/abs/two"]}`, "more than one entry"},
	}
	for _, tc := range cases {
		g := write(t, dir, tc.name+".json", tc.body)
		_, err := Resolve(g, filepath.Join(dir, "missing.json"), noEnv, nil)
		if err == nil || !strings.Contains(err.Error(), tc.wantErrSubstr) {
			t.Fatalf("%s: want error containing %q, got %v", tc.name, tc.wantErrSubstr, err)
		}
	}
}
