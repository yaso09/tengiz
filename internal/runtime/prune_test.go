package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeDocker(t *testing.T, dir, logPath string) {
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

func readDockerCalls(t *testing.T, logPath string) string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read docker log: %v", err)
	}
	return string(data)
}

func TestParseIDList(t *testing.T) {
	got := parseIDList([]byte("abc123\nxyz789\n"))
	if len(got) != 2 || got[0] != "abc123" || got[1] != "xyz789" {
		t.Fatalf("parseIDList() = %v, want [abc123 xyz789]", got)
	}

	if empty := parseIDList([]byte("")); len(empty) != 0 {
		t.Fatalf("parseIDList(empty) = %v, want []", empty)
	}
}

func TestCleanRemovesResources(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	writeFakeDocker(t, dir, logPath)

	r := &dockerRuntime{}
	result, err := r.Clean(context.Background(), CleanOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
		Cache:      true,
	})
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if len(result.Items) != 6 {
		t.Fatalf("Clean() Items = %d, want 6: %+v", len(result.Items), result.Items)
	}
	if result.DryRun {
		t.Fatal("Clean() DryRun = true, want false")
	}

	calls := readDockerCalls(t, logPath)
	for _, want := range []string{
		"rm -f abc123",
		"rm -f xyz789",
		"rmi -f img1",
		"volume rm vol1",
		"network rm net1",
		"builder prune -f",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("docker log missing %q in:\n%s", want, calls)
		}
	}
}

func TestCleanDryRunDoesNotRemove(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	writeFakeDocker(t, dir, logPath)

	r := &dockerRuntime{}
	result, err := r.Clean(context.Background(), CleanOptions{
		Containers: true,
		Images:     true,
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if !result.DryRun {
		t.Fatal("Clean() DryRun = false, want true")
	}
	if len(result.Items) != 3 {
		t.Fatalf("Clean() Items = %d, want 3: %+v", len(result.Items), result.Items)
	}

	calls := readDockerCalls(t, logPath)
	for _, forbid := range []string{"rm -f abc123", "rmi -f img1"} {
		if strings.Contains(calls, forbid) {
			t.Errorf("dry-run must not call %q, log:\n%s", forbid, calls)
		}
	}
}

func TestCleanNoSelectionDoesNothing(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	writeFakeDocker(t, dir, logPath)

	r := &dockerRuntime{}
	result, err := r.Clean(context.Background(), CleanOptions{})
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if len(result.Items) != 0 {
		t.Fatalf("Clean() Items = %d, want 0", len(result.Items))
	}
}

func TestCleanProtectsTengizContainers(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$TENGIZ_TEST_DOCKER_LOG"
case "$*" in
  "ps -a -q --filter status=exited --filter status=created")
    printf 'abc123\nxyz789\n'
    ;;
  "ps -a -q --filter status=exited --filter status=created --filter label=tengiz-app")
    printf 'abc123\n'
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

	r := &dockerRuntime{}
	result, err := r.Clean(context.Background(), CleanOptions{Containers: true})
	if err != nil {
		t.Fatalf("Clean() error = %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("Clean() Items = %d, want 1 (protected container excluded): %+v", len(result.Items), result.Items)
	}
	if result.Items[0].ID != "xyz789" {
		t.Fatalf("Clean() Items[0] = %+v, want container xyz789", result.Items[0])
	}

	calls := readDockerCalls(t, logPath)
	if !strings.Contains(calls, "rm -f xyz789") {
		t.Errorf("docker log missing removal of xyz789 in:\n%s", calls)
	}
	if strings.Contains(calls, "rm -f abc123") {
		t.Errorf("protected container abc123 must not be removed, log:\n%s", calls)
	}
}

func TestExcludeIDs(t *testing.T) {
	got := excludeIDs([]string{"a", "b", "c"}, []string{"b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("excludeIDs() = %v, want [a c]", got)
	}
	if empty := excludeIDs([]string{"a"}, nil); len(empty) != 1 {
		t.Fatalf("excludeIDs(no excludes) = %v, want [a]", empty)
	}
}
