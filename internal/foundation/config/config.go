// Package config owns the typed configuration resolver (S1.1-min, tasks-P0
// T09): precedence Default < Global < Project < Env < CLI, schema
// validation BEFORE merge, per-key origin tracking, and ValidateBounds —
// configuration may only NARROW the kernel floor, never widen it (E11).
// No hot reload in P0: restart-on-change (HARDQ B9).
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

// Config is the single typed configuration (E3: no map[string]any).
type Config struct {
	// Provider (P0: one OpenAI-compatible endpoint; the KEY itself never
	// lives in config — only the env var NAME holding it).
	ProviderBaseURL   string `json:"provider_base_url"`
	ProviderKeyEnv    string `json:"provider_key_env"`
	ProviderModel     string `json:"provider_model"`
	// Telegram bot token env var NAME.
	TelegramTokenEnv string `json:"telegram_token_env"`
	// Default profile for local sessions.
	DefaultProfile contracts.ProfileID `json:"default_profile"`
	// Egress allowlist (hosts). The kernel floor forbids "*" (E11).
	EgressAllow []string `json:"egress_allow"`
	// SandboxDisabled exists ONLY so that a config trying to set it is
	// caught and rejected: the sandbox is kernel floor, not configuration.
	SandboxDisabled bool `json:"sandbox_disabled"`
}

// Origin records which layer supplied each key (observability of merges).
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

// layer is one partially-specified source: pointers mark presence.
type layer struct {
	origin Origin
	values map[string]string // key -> raw value (strings; typed on apply)
}

// knownKeys is the closed key set — an unknown key in any layer is a
// validation error BEFORE merge (fail closed).
var knownKeys = map[string]bool{
	"provider_base_url": true, "provider_key_env": true, "provider_model": true,
	"telegram_token_env": true, "default_profile": true, "egress_allow": true,
	"sandbox_disabled": true,
}

func defaults() Config {
	return Config{
		ProviderBaseURL:  "",
		ProviderKeyEnv:   "NEXUS_API_KEY",
		ProviderModel:    "",
		TelegramTokenEnv: "NEXUS_TELEGRAM_TOKEN",
		DefaultProfile:   "private",
		EgressAllow:      nil,
	}
}

// parseFileLayer reads a JSON config file into a validated layer. A missing
// file is an EMPTY layer; an unreadable or invalid one is an error (never
// silently skipped).
func parseFileLayer(path string, origin Origin) (layer, error) {
	l := layer{origin: origin, values: map[string]string{}}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return l, nil
	}
	if err != nil {
		return l, fmt.Errorf("config %s: %w", origin, err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return l, fmt.Errorf("config %s: invalid JSON: %w", origin, err)
	}
	for k, v := range raw {
		if !knownKeys[k] {
			return l, fmt.Errorf("config %s: unknown key %q (fail closed)", origin, k)
		}
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			l.values[k] = s
			continue
		}
		var b2 bool
		if err := json.Unmarshal(v, &b2); err == nil {
			l.values[k] = fmt.Sprintf("%t", b2)
			continue
		}
		var arr []string
		if err := json.Unmarshal(v, &arr); err == nil {
			l.values[k] = strings.Join(arr, ",")
			continue
		}
		return l, fmt.Errorf("config %s: key %q has an unsupported value type", origin, k)
	}
	return l, nil
}

// envLayer reads NEXUS_CFG_<KEY> variables.
func envLayer(lookup func(string) (string, bool)) layer {
	l := layer{origin: OriginEnv, values: map[string]string{}}
	for k := range knownKeys {
		if v, ok := lookup("NEXUS_CFG_" + strings.ToUpper(k)); ok {
			l.values[k] = v
		}
	}
	return l
}

// cliLayer wraps explicit -c key=value overrides.
func cliLayer(overrides map[string]string) (layer, error) {
	l := layer{origin: OriginCLI, values: map[string]string{}}
	for k, v := range overrides {
		if !knownKeys[k] {
			return l, fmt.Errorf("config cli: unknown key %q (fail closed)", k)
		}
		l.values[k] = v
	}
	return l, nil
}

func applyKey(c *Config, key, val string) error {
	switch key {
	case "provider_base_url":
		c.ProviderBaseURL = val
	case "provider_key_env":
		c.ProviderKeyEnv = val
	case "provider_model":
		c.ProviderModel = val
	case "telegram_token_env":
		c.TelegramTokenEnv = val
	case "default_profile":
		p := contracts.ProfileID(val)
		if !p.Valid() {
			return fmt.Errorf("default_profile: invalid profile id")
		}
		c.DefaultProfile = p
	case "egress_allow":
		if val == "" {
			c.EgressAllow = nil
		} else {
			c.EgressAllow = strings.Split(val, ",")
		}
	case "sandbox_disabled":
		c.SandboxDisabled = val == "true"
	default:
		return fmt.Errorf("unknown key %q", key)
	}
	return nil
}

// ValidateBounds enforces the kernel floor: configuration only NARROWS.
func ValidateBounds(c Config) error {
	var errs []error
	for _, h := range c.EgressAllow {
		if strings.TrimSpace(h) == "*" || strings.Contains(h, "*") {
			errs = append(errs, fmt.Errorf("egress_allow: wildcard %q widens the kernel floor (rejected)", h))
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
// violation refuses the WHOLE configuration (startup refuses — B9 restart
// semantics; no partial application).
func Resolve(globalPath, projectPath string, lookupEnv func(string) (string, bool), cliOverrides map[string]string) (Resolved, error) {
	cfg := defaults()
	origins := map[string]Origin{}
	for k := range knownKeys {
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
	env := envLayer(lookupEnv)
	cli, err := cliLayer(cliOverrides)
	if err != nil {
		return Resolved{}, err
	}
	for _, l := range []layer{global, project, env, cli} {
		for k, v := range l.values {
			if err := applyKey(&cfg, k, v); err != nil {
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
