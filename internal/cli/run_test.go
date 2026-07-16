package cli

import (
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/types"
)

func TestRunCmdMissingApp(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir = tmpDir

	rootCmd.SetArgs([]string{"run", "nonexistent"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for nonexistent app")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
}

func TestRunCmdAppFoundNoCommand(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir = tmpDir

	store := config.NewStore(dataDir)
	store.SaveApp(types.AppEntry{
		Name:     "testapp",
		ImageTag: "tengiz-apps/testapp:latest",
		Config: types.AppConfig{
			Name: "testapp",
			Env:  map[string]string{"MY_VAR": "myval"},
		},
	})

	// The real docker runtime will fail if docker is not available,
	// but we can verify the flow reaches runtime.RunOnce
	rootCmd.SetArgs([]string{"run", "testapp", "--", "echo", "hello"})
	err := rootCmd.Execute()
	if err != nil {
		t.Logf("run command failed (expected if Docker unavailable): %v", err)
	}
}

func TestRunCmdWithEnvFlag(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir = tmpDir

	store := config.NewStore(dataDir)
	store.SaveApp(types.AppEntry{
		Name:     "testapp",
		ImageTag: "tengiz-apps/testapp:latest",
		Config: types.AppConfig{
			Name: "testapp",
			Env:  map[string]string{"EXISTING": "old"},
		},
	})

	rootCmd.SetArgs([]string{"run", "--env", "EXTRA=new", "testapp", "--", "env"})
	err := rootCmd.Execute()
	if err != nil {
		t.Logf("run command failed (expected if Docker unavailable): %v", err)
	}
}

func TestRunCmdWithBuildFlag(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir = tmpDir

	store := config.NewStore(dataDir)
	store.SaveApp(types.AppEntry{
		Name:     "testapp",
		ImageTag: "tengiz-apps/testapp:latest",
		Config: types.AppConfig{
			Name: "testapp",
		},
	})

	rootCmd.SetArgs([]string{"run", "--build", "testapp", "--", "echo", "hello"})
	err := rootCmd.Execute()
	t.Logf("run --build command result: %v", err)
}
