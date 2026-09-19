//go:build linux

package conf

import (
	"os"
	"testing"
)

func TestEnvConfig_Defaults(t *testing.T) {
	// Save and restore env
	origDebug := os.Getenv("DEBUG")
	origHostname := os.Getenv("PBSX_HOSTNAME")
	defer func() {
		os.Setenv("DEBUG", origDebug)
		os.Setenv("PBSX_HOSTNAME", origHostname)
	}()

	os.Unsetenv("DEBUG")
	os.Unsetenv("PBSX_HOSTNAME")

	cfg := loadEnvConfig()

	tests := []struct {
		name string
		got  bool
		want bool
	}{
		{"Debug defaults to false", cfg.Debug, false},
		{"StdoutOnly defaults to false", cfg.StdoutOnly, false},
		{"EnableAutoUpdate defaults to false", cfg.EnableAutoUpdate, false},
		{"InsideContainer defaults to false", cfg.InsideContainer, false},
		{"ForceDisableIRM defaults to false", cfg.ForceDisableIRM, false},
		{"PprofEnabled defaults to false", cfg.PprofEnabled, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}

	if cfg.Hostname != "" {
		t.Errorf("Hostname should be empty by default, got %q", cfg.Hostname)
	}
}

func TestEnvConfig_DebugEnabled(t *testing.T) {
	orig := os.Getenv("DEBUG")
	defer os.Setenv("DEBUG", orig)

	os.Setenv("DEBUG", "true")
	cfg := loadEnvConfig()
	if !cfg.Debug {
		t.Error("expected Debug to be true")
	}
}

func TestEnvConfig_HostnameSet(t *testing.T) {
	orig := os.Getenv("PBSX_HOSTNAME")
	defer os.Setenv("PBSX_HOSTNAME", orig)

	os.Setenv("PBSX_HOSTNAME", "test-host")
	cfg := loadEnvConfig()
	if cfg.Hostname != "test-host" {
		t.Errorf("expected 'test-host', got %q", cfg.Hostname)
	}
}
