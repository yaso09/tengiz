package cli

import (
	"strings"
	"testing"
)

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatalf("cleanup command not found: %v", err)
	}
	if cmd == nil || cmd.Use != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	expected := []string{
		"containers", "images", "all-images", "volumes", "networks", "cache",
		"all", "dry-run", "force",
	}
	for _, name := range expected {
		if cleanupCmd.Flags().Lookup(name) == nil {
			t.Errorf("cleanupCmd missing --%s flag", name)
		}
	}
}

func TestCleanupDryRun(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	output := captureOutput(func() {
		rootCmd.Execute()
	})
	for _, want := range []string{
		"docker container prune -f --filter label!=tengiz-app --filter label!=tengiz-env",
		"docker image prune -f",
		"docker network prune -f",
		"docker builder prune -f",
		"nothing was removed",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("dry-run output missing %q; got:\n%s", want, output)
		}
	}
}

func TestCleanupDryRunAll(t *testing.T) {
	rootCmd.SetArgs([]string{"cleanup", "--dry-run", "--all"})
	output := captureOutput(func() {
		rootCmd.Execute()
	})
	for _, want := range []string{
		"docker volume prune -f",
		"docker rmi -f",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("dry-run --all output missing %q; got:\n%s", want, output)
		}
	}
}

func confirmResult(input string) bool {
	var accepted bool
	captureOutput(func() {
		accepted = confirmCleanup(strings.NewReader(input))
	})
	return accepted
}

func TestConfirmCleanup(t *testing.T) {
	if !confirmResult("y\n") {
		t.Error("confirmCleanup should accept 'y'")
	}
	if !confirmResult("YES\n") {
		t.Error("confirmCleanup should accept 'YES'")
	}
	if confirmResult("n\n") {
		t.Error("confirmCleanup should reject 'n'")
	}
	if confirmResult("\n") {
		t.Error("confirmCleanup should reject empty input")
	}
}
