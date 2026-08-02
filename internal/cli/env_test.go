package cli

import (
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

func envFlagValue(t *testing.T, cmd *cobra.Command) string {
	t.Helper()
	if err := cmd.ParseFlags(nil); err != nil {
		t.Fatalf("parse flags for %s: %v", cmd.Use, err)
	}
	env, err := cmd.Flags().GetString("env")
	if err != nil {
		t.Fatalf("%s missing --env flag: %v", cmd.Use, err)
	}
	return env
}

func TestNamedCommandsHaveEnvFlag(t *testing.T) {
	commands := []*cobra.Command{
		stopCmd, startCmd, rmCmd, logsCmd,
		rollbackCmd, buildLogsCmd, healthCmd,
	}
	for _, cmd := range commands {
		t.Run(cmd.Use, func(t *testing.T) {
			if env := envFlagValue(t, cmd); env != "production" {
				t.Errorf("%s --env default = %q, want %q", cmd.Use, env, "production")
			}
		})
	}
}

func TestRunCmdHasEnvStringArrayFlag(t *testing.T) {
	flag := runCmd.Flags().Lookup("env")
	if flag == nil {
		t.Fatal("runCmd missing --env flag")
	}
	if flag.Value.Type() != "stringArray" {
		t.Errorf("runCmd --env type = %q, want stringArray (extra env vars)", flag.Value.Type())
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
			if env := envFlagValue(t, cmd); env != "production" {
				t.Errorf("%s --env default = %q, want %q", cmd.Use, env, "production")
			}
		})
	}
}

func TestInitHasEnvFlag(t *testing.T) {
	if env := envFlagValue(t, initCmd); env != "production" {
		t.Errorf("initCmd --env default = %q, want %q", env, "production")
	}
}

func TestProxyHasEnvFlag(t *testing.T) {
	if env := envFlagValue(t, proxyCmd); env != "production" {
		t.Errorf("proxyCmd --env default = %q, want %q", env, "production")
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
