package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
)

func hasEnvFlag(cmd *cobra.Command) bool {
	if cmd.Flags().Lookup("env") != nil {
		return true
	}
	return cmd.InheritedFlags().Lookup("env") != nil
}

func resetEnvFlag() {
	rootCmd.PersistentFlags().Set("env", "production")
}

func TestEnvFlagDefault(t *testing.T) {
	defer resetEnvFlag()
	cmd := deployCmd
	cmd.ParseFlags([]string{})
	env, _ := cmd.Flags().GetString("env")
	if env != "production" {
		t.Errorf("default env = %q, want %q", env, "production")
	}
}

func TestDeployCmdHasEnvFlag(t *testing.T) {
	if !hasEnvFlag(deployCmd) {
		t.Error("deployCmd missing --env flag")
	}
}

func TestNamedCommandsHaveEnvFlag(t *testing.T) {
	commands := []*cobra.Command{
		stopCmd, startCmd, rmCmd, logsCmd,
		rollbackCmd, buildLogsCmd, runCmd, healthCmd,
	}
	for _, cmd := range commands {
		t.Run(cmd.Use, func(t *testing.T) {
			if !hasEnvFlag(cmd) {
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
			if !hasEnvFlag(cmd) {
				t.Errorf("%s missing --env flag", cmd.Use)
			}
		})
	}
}

func TestInitHasEnvFlag(t *testing.T) {
	if !hasEnvFlag(initCmd) {
		t.Error("initCmd missing --env flag")
	}
}

func TestProxyHasEnvFlag(t *testing.T) {
	if !hasEnvFlag(proxyCmd) {
		t.Error("proxyCmd missing --env flag")
	}
}

func TestRunCmdEnvFlag(t *testing.T) {
	defer resetEnvFlag()
	if err := runCmd.ParseFlags([]string{"--env", "staging", "myapp", "--", "echo"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	env, err := runCmd.Flags().GetString("env")
	if err != nil {
		t.Fatalf("GetString(env) error: %v", err)
	}
	if env != "staging" {
		t.Errorf("env = %q, want %q", env, "staging")
	}
}

func TestRunCmdEnvVarFlag(t *testing.T) {
	defer resetEnvFlag()
	if err := runCmd.ParseFlags([]string{"-e", "FOO=bar", "myapp", "--", "echo"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	vars, err := runCmd.Flags().GetStringArray("env-var")
	if err != nil {
		t.Fatalf("GetStringArray(env-var) error: %v", err)
	}
	if len(vars) != 1 || vars[0] != "FOO=bar" {
		t.Errorf("env-var = %v, want [FOO=bar]", vars)
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