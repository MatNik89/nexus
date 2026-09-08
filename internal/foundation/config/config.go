// Package config owns the typed configuration resolver (S1.1-min, tasks-P0
// T09): precedence Default < Global < Project < Env < CLI, PER-KEY schema
// validation BEFORE merge in EVERY layer, per-key origin tracking, and
// ValidateBounds — configuration only NARROWS the kernel floor (E11).
// No hot reload in P0: restart-on-change (HARDQ B9).
//
// Phase-1B fold: generic string coercion replaced by a typed per-key
// schema (codex #11); the environment is ENUMERATED so an unknown
// NEXUS_CFG_* name fails closed like any other layer (codex #12); egress
// hosts stay a real array end-to-end with trimmed, non-empty entries
// (codex #13, kilo #3).
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

// Config is the single typed configuration (E3: no map[string]any).
type Config struct {
	ProviderBaseURL  string `json:"provider_base_url"`
	ProviderKeyEnv   string `json:"provider_key_env"`
	ProviderModel    string `json:"provider_model"`
	TelegramTokenEnv string `json:"telegram_token_env"`
	// TelegramAPIBase overrides the Bot API endpoint (local bot-api
	// server, acceptance harness); default is the production URL.
	TelegramAPIBase string              `json:"telegram_api_base"`
	DefaultProfile  contracts.ProfileID `json:"default_profile"`
	EgressAllow     []string            `json:"egress_allow"`
	// ExecAllow is the DENY-DEFAULT promoted-target allowlist for the
	// exec tool: absolute program paths the owner explicitly trusts.
	// Empty = exec refuses everything (Phase-6 codex #5: without it any
	// absolute ELF — /bin/sh -c included — was executable once approved).
	ExecAllow []string `json:"exec_allow"`
	// SandboxDisabled exists ONLY so an attempt to set it is caught and
	// rejected: the sandbox is kernel floor, not configuration.
	SandboxDisabled bool `json:"sandbox_disabled"`
	// Timezone is the IANA zone for the conversational time line the
	// model sees (single-owner). Default Europe/Zagreb; validated by
	// LoadLocation, "Local"/"" rejected (the reminder occurrences keep
	// their own zone — this is only the chat clock).
	Timezone string `json:"timezone"`
	// ContextHardLimitTokens is the ONE configured ContextBudget hard limit
	// (Slice C, AUDIT-FULL F5): the planner measures the FINAL wire messages
	// against it before every provider attempt and REFUSES over budget
	// (never trims). Default 64000; zero/negative is rejected at Resolve —
	// "unlimited" does not exist.
	ContextHardLimitTokens int `json:"context_hard_limit_tokens"`
}

// Origin records which layer supplied each key.
type Origin string

const (
	OriginDefault Origin = "default"
	OriginGlobal  Origin = "global"
	OriginProject Origin = "project"
	OriginEnv     Origin = "env"
	OriginCLI     Origin = "cli"
)

// Resolved is the merged configuration plus per-key provenance.
type Resolved struct {
	Config  Config
	Origins map[string]Origin
}

// ConfigHash returns the sha256 digest of the CANONICAL resolved
// configuration — the config OWNER is the only principal that computes
// capability-attestation bindings (SPEC P0.5 amendment; Phase-1B-r3 codex
// #10: a free-floating caller string was assertion, not binding). It
// satisfies the closure package's ConfigBinding seam.
func (r Resolved) ConfigHash() string {
	// A typed struct of strings/bools/string-slices marshals
	// deterministically (fixed field order) and cannot fail.
	b, _ := json.Marshal(r.Config)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// keyKind is the per-key schema (codex #11: every layer parses strictly).
type keyKind int

const (
	kindString keyKind = iota
	kindStringList
	kindBool
	kindInt
)

var keySchema = map[string]keyKind{
	"provider_base_url":         kindString,
	"provider_key_env":          kindString,
	"provider_model":            kindString,
	"telegram_token_env":        kindString,
	"telegram_api_base":         kindString,
	"default_profile":           kindString,
	"egress_allow":              kindStringList,
	"exec_allow":                kindStringList,
	"sandbox_disabled":          kindBool,
	"timezone":                  kindString,
	"context_hard_limit_tokens": kindInt,
}

// value is one typed, presence-aware layer entry.
type value struct {
	str  string
	list []string
	b    bool
	n    int
	kind keyKind
}

type layer struct {
	origin Origin
	values map[string]value
}

func defaults() Config {
	return Config{
		ProviderKeyEnv:         "NEXUS_API_KEY",
		TelegramTokenEnv:       "NEXUS_TELEGRAM_TOKEN",
		TelegramAPIBase:        "https://api.telegram.org",
		DefaultProfile:         "private",
		Timezone:               "Europe/Zagreb",
		ContextHardLimitTokens: 64000,
	}
}

// parseIntStrict accepts only a plain decimal integer (no sign-less floats,
// no exponents, no whitespace) — a typo must fail closed, never parse to 0.
func parseIntStrict(origin Origin, key, raw string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || strings.TrimSpace(raw) != raw {
		return 0, fmt.Errorf("config %s: %s must be a decimal integer (rejected)", origin, key)
	}
	return n, nil
}

// parseList validates a host list: trimmed, non-empty entries only
// (kilo #3: an untrimmed " host" would silently break exact matching).
// Diagnostics carry the entry POSITION, never the raw value — a mistyped
// secret must not be echoed into startup errors (Phase-1B-r2 codex #10).
func parseList(origin Origin, key string, raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	for i, h := range raw {
		trimmed := strings.TrimSpace(h)
		if trimmed == "" {
			return nil, fmt.Errorf("config %s: %s: entry %d is empty (rejected)", origin, key, i)
		}
		if trimmed != h {
			return nil, fmt.Errorf("config %s: %s: entry %d has surrounding whitespace (rejected)", origin, key, i)
		}
		out = append(out, trimmed)
	}
	return out, nil
}

// parseBoolStrict never echoes the raw value (codex #10: a secret mistyped
// into a boolean key would land verbatim in the startup error).
func parseBoolStrict(origin Origin, key, raw string) (bool, error) {
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return false, fmt.Errorf("config %s: %s must be exactly true or false (value withheld; rejected)", origin, key)
}

// parseFileLayer reads a JSON config file with STRICT per-key types.
func parseFileLayer(path string, origin Origin) (layer, error) {
	l := layer{origin: origin, values: map[string]value{}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return l, nil
	}
	if err != nil {
		return l, fmt.Errorf("config %s: %w", origin, err)
	}
	// Reject duplicate JSON member names BEFORE decoding into the map (F9):
	// Go's map unmarshal is silently last-value-wins, which makes a
	// security-sensitive config key ambiguous. Reuse the kernel detector.
	if contracts.HasDuplicateJSONKeys(b) {
		return l, fmt.Errorf("config %s: duplicate JSON keys (ambiguous, fail closed)", origin)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return l, fmt.Errorf("config %s: invalid JSON: %w", origin, err)
	}
	for k, v := range raw {
		kind, known := keySchema[k]
		if !known {
			return l, fmt.Errorf("config %s: unknown key %q (fail closed)", origin, k)
		}
		switch kind {
		case kindString:
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return l, fmt.Errorf("config %s: %s must be a JSON string (rejected)", origin, k)
			}
			l.values[k] = value{kind: kindString, str: s}
		case kindStringList:
			var arr []string
			if err := json.Unmarshal(v, &arr); err != nil {
				return l, fmt.Errorf("config %s: %s must be a JSON string array (rejected)", origin, k)
			}
			list, err := parseList(origin, k, arr)
			if err != nil {
				return l, err
			}
			l.values[k] = value{kind: kindStringList, list: list}
		case kindBool:
			var bv bool
			if err := json.Unmarshal(v, &bv); err != nil {
				return l, fmt.Errorf("config %s: %s must be a JSON boolean (rejected)", origin, k)
			}
			l.values[k] = value{kind: kindBool, b: bv}
		case kindInt:
			// A JSON number is only accepted as an INTEGER literal
			// (64000, not 64000.0 / 6.4e4): json.Number keeps the text.
			var num json.Number
			if err := json.Unmarshal(v, &num); err != nil {
				return l, fmt.Errorf("config %s: %s must be a JSON integer (rejected)", origin, k)
			}
			n, err := parseIntStrict(origin, k, num.String())
			if err != nil {
				return l, err
			}
			l.values[k] = value{kind: kindInt, n: n}
		}
	}
	return l, nil
}

const envPrefix = "NEXUS_CFG_"

// envLayer ENUMERATES environ: every NEXUS_CFG_* name must be a known key
// (codex #12: probing known names let unknown prefixed vars fail open).
// The suffix must be CANONICAL UPPERCASE and each normalized key may appear
// once — Linux env names are case-sensitive, so silent case folding was an
// order-dependent duplicate/overwrite channel (Phase-1B-r2 codex #9).
func envLayer(environ []string) (layer, error) {
	l := layer{origin: OriginEnv, values: map[string]value{}}
	for _, kv := range environ {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 || !strings.HasPrefix(kv, envPrefix) {
			continue
		}
		name, raw := kv[:eq], kv[eq+1:]
		suffix := strings.TrimPrefix(name, envPrefix)
		if suffix != strings.ToUpper(suffix) {
			return l, fmt.Errorf("config env: %s is not canonical uppercase (fail closed)", name)
		}
		key := strings.ToLower(suffix)
		kind, known := keySchema[key]
		if !known {
			return l, fmt.Errorf("config env: unknown variable %s (fail closed)", name)
		}
		if _, dup := l.values[key]; dup {
			return l, fmt.Errorf("config env: %s set more than once (fail closed)", name)
		}
		val, err := parseRawLayerValue(OriginEnv, key, kind, raw)
		if err != nil {
			return l, err
		}
		l.values[key] = val
	}
	return l, nil
}

// parseRawLayerValue applies the per-key schema to a raw string (env/CLI).
func parseRawLayerValue(origin Origin, key string, kind keyKind, raw string) (value, error) {
	switch kind {
	case kindString:
		return value{kind: kindString, str: raw}, nil
	case kindStringList:
		if raw == "" {
			return value{kind: kindStringList, list: nil}, nil
		}
		list, err := parseList(origin, key, strings.Split(raw, ","))
		if err != nil {
			return value{}, err
		}
		return value{kind: kindStringList, list: list}, nil
	case kindBool:
		bv, err := parseBoolStrict(origin, key, raw)
		if err != nil {
			return value{}, err
		}
		return value{kind: kindBool, b: bv}, nil
	case kindInt:
		n, err := parseIntStrict(origin, key, raw)
		if err != nil {
			return value{}, err
		}
		return value{kind: kindInt, n: n}, nil
	}
	return value{}, fmt.Errorf("config %s: %s has an unknown schema kind", origin, key)
}

func cliLayer(overrides map[string]string) (layer, error) {
	l := layer{origin: OriginCLI, values: map[string]value{}}
	for k, raw := range overrides {
		kind, known := keySchema[k]
		if !known {
			return l, fmt.Errorf("config cli: unknown key %q (fail closed)", k)
		}
		val, err := parseRawLayerValue(OriginCLI, k, kind, raw)
		if err != nil {
			return l, err
		}
		l.values[k] = val
	}
	return l, nil
}

func applyValue(c *Config, key string, v value) error {
	switch key {
	case "provider_base_url":
		c.ProviderBaseURL = v.str
	case "provider_key_env":
		c.ProviderKeyEnv = v.str
	case "provider_model":
		c.ProviderModel = v.str
	case "telegram_token_env":
		c.TelegramTokenEnv = v.str
	case "telegram_api_base":
		c.TelegramAPIBase = v.str
	case "default_profile":
		p := contracts.ProfileID(v.str)
		if !p.Valid() {
			return fmt.Errorf("default_profile: invalid profile id")
		}
		c.DefaultProfile = p
	case "timezone":
		if v.str == "" || v.str == "Local" {
			return fmt.Errorf("timezone: must be a real IANA zone (not %q)", v.str)
		}
		if _, err := time.LoadLocation(v.str); err != nil {
			return fmt.Errorf("timezone: %v", err)
		}
		c.Timezone = v.str
	case "egress_allow":
		c.EgressAllow = v.list
	case "exec_allow":
		c.ExecAllow = v.list
	case "sandbox_disabled":
		c.SandboxDisabled = v.b
	case "context_hard_limit_tokens":
		c.ContextHardLimitTokens = v.n
	default:
		return fmt.Errorf("unknown key %q", key)
	}
	return nil
}

// ValidateBounds enforces the kernel floor: configuration only NARROWS.
func ValidateBounds(c Config) error {
	var errs []error
	// The context hard limit must be a positive, bounded token count: zero or
	// negative would silently mean "unlimited" downstream (F5), and an absurd
	// ceiling is a typo, not a policy.
	if c.ContextHardLimitTokens <= 0 || c.ContextHardLimitTokens > 2_000_000 {
		errs = append(errs, fmt.Errorf("context_hard_limit_tokens: %d is outside (0, 2000000] (rejected — unlimited context does not exist)", c.ContextHardLimitTokens))
	}
	for _, h := range c.EgressAllow {
		if strings.Contains(h, "*") {
			errs = append(errs, fmt.Errorf("egress_allow: wildcard %q widens the kernel floor (rejected)", h))
		}
	}
	// telegram_api_base carries the bot token in its URL path (T27 codex
	// #4): only the production endpoint or an explicit http(s) LOOPBACK
	// endpoint (local bot-api server, tests) is accepted — a config-layer
	// injection can never exfiltrate the token to an arbitrary host.
	if c.TelegramAPIBase != "" && c.TelegramAPIBase != "https://api.telegram.org" {
		u, uerr := url.Parse(c.TelegramAPIBase)
		if uerr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			errs = append(errs, fmt.Errorf("telegram_api_base: %q is not a valid http(s) URL (rejected)", c.TelegramAPIBase))
		} else if ip := net.ParseIP(u.Hostname()); (ip == nil || !ip.IsLoopback()) && u.Hostname() != "localhost" {
			errs = append(errs, fmt.Errorf("telegram_api_base: %q — only the production endpoint or a loopback override is allowed (the bot token rides in the URL path; rejected)", c.TelegramAPIBase))
		}
	}
	for _, p := range c.ExecAllow {
		if strings.Contains(p, "*") || !strings.HasPrefix(p, "/") {
			errs = append(errs, fmt.Errorf("exec_allow: %q must be an absolute path without wildcards (rejected)", p))
		}
	}
	if c.SandboxDisabled {
		errs = append(errs, errors.New("sandbox_disabled: the sandbox is kernel floor, not configuration (rejected)"))
	}
	if !c.DefaultProfile.Valid() {
		errs = append(errs, errors.New("default_profile is required"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("config bounds: %w", errors.Join(errs...))
	}
	return nil
}

// Resolve merges all layers in precedence order. Any layer error or bounds
// violation refuses the WHOLE configuration (B9: startup refuses; restart
// after fixing — no partial application).
func Resolve(globalPath, projectPath string, environ []string, cliOverrides map[string]string) (Resolved, error) {
	cfg := defaults()
	origins := map[string]Origin{}
	for k := range keySchema {
		origins[k] = OriginDefault
	}
	global, err := parseFileLayer(globalPath, OriginGlobal)
	if err != nil {
		return Resolved{}, err
	}
	project, err := parseFileLayer(projectPath, OriginProject)
	if err != nil {
		return Resolved{}, err
	}
	env, err := envLayer(environ)
	if err != nil {
		return Resolved{}, err
	}
	cli, err := cliLayer(cliOverrides)
	if err != nil {
		return Resolved{}, err
	}
	for _, l := range []layer{global, project, env, cli} {
		for k, v := range l.values {
			if err := applyValue(&cfg, k, v); err != nil {
				return Resolved{}, fmt.Errorf("config %s: %w", l.origin, err)
			}
			origins[k] = l.origin
		}
	}
	if err := ValidateBounds(cfg); err != nil {
		return Resolved{}, err
	}
	return Resolved{Config: cfg, Origins: origins}, nil
}
