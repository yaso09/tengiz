package cli

import (
	"os"
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
		t.Errorf("env = %q, want %q", env, "staging")
	}
}

func TestNamedCommandsHaveEnvFlag(t *testing.T) {
	commands := []*cobra.Command{
		stopCmd, startCmd, rmCmd, logsCmd,
		rollbackCmd, buildLogsCmd, healthCmd,
	}
	for _, cmd := range commands {
		t.Run(cmd.Use, func(t *testing.T) {
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
			flag := cmd.Flags().Lookup("env")
			if flag == nil {
				t.Errorf("%s missing --env flag", cmd.Use)
			}
		})
	}
}

func TestInitHasEnvFlag(t *testing.T) {
	flag := initCmd.Flags().Lookup("env")
	if flag == nil {
		t.Error("initCmd missing --env flag")
	}
}

func TestInitWithEnvCreatesEnvFile(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origWd) }()

	rootCmd.SetArgs([]string{"init", "--env", "staging"})
	err = rootCmd.Execute()
	if err != nil {
		t.Fatalf("init --env staging: %v", err)
	}

	if _, err := os.Stat(".tengiz.staging.yaml"); err != nil {
		t.Errorf("expected .tengiz.staging.yaml to be created, got error: %v", err)
	}
	if _, err := os.Stat(".tengiz.yaml"); err == nil {
		t.Error("did not expect .tengiz.yaml to be created for env-specific init")
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
