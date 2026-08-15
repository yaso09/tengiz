package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeDockerForCLI(t *testing.T, dir, logPath string) {
	t.Helper()
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$TENGIZ_TEST_DOCKER_LOG"
case "$*" in
  "ps -a -q --filter status=exited --filter status=created")
    printf 'abc123\nxyz789\n'
    ;;
  "images --filter dangling=true -q")
    printf 'img1\n'
    ;;
  "volume ls --filter dangling=true -q")
    printf 'vol1\n'
    ;;
  "network ls --filter dangling=true -q")
    printf 'net1\n'
    ;;
esac
exit 0
`
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("TENGIZ_TEST_DOCKER_LOG", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestCleanupCmdRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
	for _, flag := range []string{"containers", "images", "volumes", "networks", "cache", "all", "dry-run"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Errorf("cleanup missing --%s flag", flag)
		}
	}
}

func TestCleanupDryRunOutput(t *testing.T) {
	dir := t.TempDir()
	dataDir = dir
	writeFakeDockerForCLI(t, dir, filepath.Join(dir, "calls.log"))

	rootCmd.SetArgs([]string{"cleanup", "--dry-run"})
	output := captureOutput(func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	})

	for _, want := range []string{
		"container (2): abc123, xyz789",
		"image (1): img1",
		"volume (1): vol1",
		"network (1): net1",
		"cache (1): build-cache",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("cleanup output missing %q, got:\n%s", want, output)
		}
	}
}
