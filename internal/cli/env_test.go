package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
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
		t.Errorf("deployCmd --env = %q, want %q", env, "staging")
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

func TestNamedCommandsHaveEnvFlag(t *testing.T) {
	commands := []*cobra.Command{
		stopCmd, startCmd, rmCmd, logsCmd,
		rollbackCmd, buildLogsCmd, runCmd, healthCmd,
	}
	for _, cmd := range commands {
		t.Run(cmd.Use, func(t *testing.T) {
			cmd.ParseFlags([]string{})
			flag := cmd.Flags().Lookup("env")
			if flag == nil {
				t.Errorf("%s missing --env flag", cmd.Use)
			}
		})
	}
}

func TestSubCommandsHaveEnvFlag(t *testing.T) {
	subCommands := []*cobra.Command{
		configSetCmd, configGetCmd, configUnsetCmd, configShowCmd,
		domainAddCmd, domainRemoveCmd, domainListCmd,
		volumeAddCmd, volumeRemoveCmd, volumeListCmd,
	}
	for _, cmd := range subCommands {
		t.Run(cmd.Use, func(t *testing.T) {
			cmd.ParseFlags([]string{})
			flag := cmd.Flags().Lookup("env")
			if flag == nil {
				t.Errorf("%s missing --env flag", cmd.Use)
			}
		})
	}
}

func TestInitHasEnvFlag(t *testing.T) {
	initCmd.ParseFlags([]string{})
	flag := initCmd.Flags().Lookup("env")
	if flag == nil {
		t.Error("initCmd missing --env flag")
	}
}

func TestProxyHasEnvFlag(t *testing.T) {
	proxyCmd.ParseFlags([]string{})
	flag := proxyCmd.Flags().Lookup("env")
	if flag == nil {
		t.Error("proxyCmd missing --env flag")
	}
}

func TestDeployWithEnvUsesQualifiedName(t *testing.T) {
	qualified := config.AppQualifiedName("myapp", "staging")
	if qualified != "myapp-staging" {
		t.Errorf("expected myapp-staging, got %s", qualified)
	}

	prod := config.AppQualifiedName("myapp", "production")
	if prod != "myapp" {
		t.Errorf("expected myapp, got %s", prod)
	}
}

func TestConfigLoadWithEnvMerge(t *testing.T) {
	dir := t.TempDir()
	base := `name: app
port: 3000
env:
  DATABASE_URL: postgres://localhost/mydb
  API_KEY: base-key
`
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(base), 0644)

	staging := `port: 4000
env:
  API_KEY: staging-key
`
	os.WriteFile(filepath.Join(dir, ".tengiz.staging.yaml"), []byte(staging), 0644)

	cfg, err := config.LoadWithEnv(dir, "staging")
	if err != nil {
		t.Fatalf("LoadWithEnv: %v", err)
	}
	if cfg.Port != 4000 {
		t.Errorf("Port = %d, want 4000", cfg.Port)
	}
	if cfg.Env["database_url"] != "postgres://localhost/mydb" {
		t.Errorf("DATABASE_URL env lost")
	}
	if cfg.Env["api_key"] != "staging-key" {
		t.Errorf("API_KEY env = %q, want %q", cfg.Env["api_key"], "staging-key")
	}
}
