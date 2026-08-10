package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestGetEnvDefault(t *testing.T) {
	deployCmd.ParseFlags([]string{})
	env, err := deployCmd.Flags().GetString("env")
	if err != nil {
		t.Fatal(err)
	}
	if env != "production" {
		t.Errorf("default env = %q, want %q", env, "production")
	}
}

func TestGetEnvCustom(t *testing.T) {
	deployCmd.ParseFlags([]string{"--env", "staging", "."})
	env, err := deployCmd.Flags().GetString("env")
	if err != nil {
		t.Fatal(err)
	}
	if env != "staging" {
		t.Errorf("env = %q, want %q", env, "staging")
	}
}

func TestNamedCommandsHaveEnvFlag(t *testing.T) {
	commands := []*cobra.Command{
		stopCmd, startCmd, rmCmd, logsCmd,
		rollbackCmd, buildLogsCmd, runCmd, healthCmd,
	}
	for _, cmd := range commands {
		t.Run(cmd.Use, func(t *testing.T) {
			if flag := cmd.Flags().Lookup("env"); flag == nil {
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
			if flag := cmd.Flags().Lookup("env"); flag == nil {
				t.Errorf("%s missing --env flag", cmd.Use)
			}
		})
	}
}
