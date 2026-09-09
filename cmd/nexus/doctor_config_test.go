package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MatNik89/nexus/internal/preflight/doctor"
)

// Slice E (AUDIT-FULL F7) through the REAL ordinary-doctor path: the
// configuration is resolved from XDG_CONFIG_HOME/nexus/config.json exactly as
// the daemon resolves it, and the secret checks follow the CONFIGURED names.
func TestDoctorUsesConfiguredSecretNames(t *testing.T) {
	find := func(checks []doctor.Check, name string) doctor.Check {
		for _, c := range checks {
			if c.Name == name {
				return c
			}
		}
		t.Fatalf("check %q missing in %+v", name, checks)
		return doctor.Check{}
	}
	cases := []struct {
		name   string
		config string
		env    map[string]string
		wantOK bool
	}{
		{"custom-configured-custom-populated",
			`{"provider_key_env":"CUSTOM_PROVIDER_KEY","telegram_token_env":"CUSTOM_TG"}`,
			map[string]string{"CUSTOM_PROVIDER_KEY": "k", "CUSTOM_TG": "t", "NEXUS_API_KEY": "", "NEXUS_TELEGRAM_TOKEN": ""}, true},
		{"custom-configured-default-populated",
			`{"provider_key_env":"CUSTOM_PROVIDER_KEY","telegram_token_env":"CUSTOM_TG"}`,
			map[string]string{"CUSTOM_PROVIDER_KEY": "", "CUSTOM_TG": "", "NEXUS_API_KEY": "k", "NEXUS_TELEGRAM_TOKEN": "t"}, false},
		{"default-configured-default-populated", `{}`,
			map[string]string{"CUSTOM_PROVIDER_KEY": "", "CUSTOM_TG": "", "NEXUS_API_KEY": "k", "NEXUS_TELEGRAM_TOKEN": "t"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conf := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", conf)
			if err := os.MkdirAll(filepath.Join(conf, "nexus"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(conf, "nexus", "config.json"), []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			checks := doctorChecks()
			for _, name := range []string{"provider-key", "telegram-token"} {
				c := find(checks, name)
				if (c.Status == doctor.StatusOK) != tc.wantOK {
					t.Fatalf("%s: status %v want ok=%v (%s)", name, c.Status, tc.wantOK, c.Detail)
				}
			}
		})
	}
	// A configuration that does not resolve is a FINDING, not a crash, and the
	// secret checks do not fall back to the default names.
	conf := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", conf)
	os.MkdirAll(filepath.Join(conf, "nexus"), 0o700)
	os.WriteFile(filepath.Join(conf, "nexus", "config.json"), []byte(`{"context_hard_limit_tokens": 0}`), 0o600)
	t.Setenv("NEXUS_API_KEY", "k")
	t.Setenv("NEXUS_TELEGRAM_TOKEN", "t")
	checks := doctorChecks()
	if c := find(checks, "config"); c.Status == doctor.StatusOK {
		t.Fatal("unresolvable configuration reported OK")
	}
	if c := find(checks, "provider-key"); c.Status == doctor.StatusOK {
		t.Fatal("provider-key reported ON from the default name while the configuration is unresolvable")
	}
	if doctor.Ready(checks) {
		t.Fatal("unresolvable configuration reported prerequisites-ready")
	}
}
