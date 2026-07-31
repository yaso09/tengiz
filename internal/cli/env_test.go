package cli

import (
	"testing"

	"github.com/yaso09/tengiz/internal/config"
)

func TestEnvFlagDefault(t *testing.T) {
	cmd := deployCmd
	cmd.ParseFlags([]string{})
	env, _ := cmd.Flags().GetString("env")
	if env != "production" {
		t.Errorf("deployCmd --env default = %q, want %q", env, "production")
	}
}

func TestEnvFlagCustom(t *testing.T) {
	cmd := deployCmd
	cmd.ParseFlags([]string{"--env", "staging", "."})
	env, _ := cmd.Flags().GetString("env")
	if env != "staging" {
		t.Errorf("env = %q, want %q", env, "staging")
	}
}

func TestEnvQualifiedName(t *testing.T) {
	tests := []struct {
		name, env, expected string
	}{
		{"myapp", "production", "myapp"},
		{"myapp", "staging", "myapp-staging"},
		{"myapp", "development", "myapp-development"},
	}
	for _, tc := range tests {
		got := config.AppQualifiedName(tc.name, tc.env)
		if got != tc.expected {
			t.Errorf("AppQualifiedName(%q, %q) = %q, want %q", tc.name, tc.env, got, tc.expected)
		}
	}
}
