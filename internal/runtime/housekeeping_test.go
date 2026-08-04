package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPruneArgsDefaults(t *testing.T) {
	args := buildPruneArgs(HousekeepingOptions{})
	want := "system prune -f"
	if got := strings.Join(args, " "); got != want {
		t.Errorf("buildPruneArgs() = %q, want %q", got, want)
	}
}

func TestBuildPruneArgsAllVolumes(t *testing.T) {
	args := buildPruneArgs(HousekeepingOptions{All: true, Volumes: true})
	want := "system prune -f --all --volumes"
	if got := strings.Join(args, " "); got != want {
		t.Errorf("buildPruneArgs() = %q, want %q", got, want)
	}
}

func TestBuildPruneArgsUntilAndFilters(t *testing.T) {
	args := buildPruneArgs(HousekeepingOptions{
		Until:   "48h",
		Filters: []string{HousekeepingProtectFilter()},
	})
	want := "system prune -f --filter until=48h --filter label!=tengiz-app"
	if got := strings.Join(args, " "); got != want {
		t.Errorf("buildPruneArgs() = %q, want %q", got, want)
	}
}

func TestHousekeepingProtectFilter(t *testing.T) {
	if got := HousekeepingProtectFilter(); got != "label!=tengiz-app" {
		t.Errorf("HousekeepingProtectFilter() = %q, want %q", got, "label!=tengiz-app")
	}
}

func TestParseReclaimedSpace(t *testing.T) {
	output := "Deleted Containers: 2\nDeleted Images: 3\nTotal reclaimed space: 1.234GB\n"
	if got := parseReclaimedSpace(output); got != "1.234GB" {
		t.Errorf("parseReclaimedSpace() = %q, want %q", got, "1.234GB")
	}
}

func TestParseReclaimedSpaceEmpty(t *testing.T) {
	if got := parseReclaimedSpace("nothing to report"); got != "" {
		t.Errorf("parseReclaimedSpace() = %q, want empty", got)
	}
}
func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), HousekeepingOptions{})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res == nil || res.Output != "" || res.SpaceFreed != "" {
		t.Errorf("Cleanup() result = %+v, want empty result", res)
	}
}

func TestDockerCleanupRunsSystemPrune(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	fakeDocker := filepath.Join(dir, "docker")
	script := "#!/bin/sh\n" +
		fmt.Sprintf("printf '%%s\\n' \"$*\" > %s\n", argsFile) +
		"echo 'Deleted Containers: 2'\n" +
		"echo 'Deleted Images: 3'\n" +
		"echo 'Total reclaimed space: 1.234GB'\n"
	if err := os.WriteFile(fakeDocker, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	rt := &dockerRuntime{}
	res, err := rt.Cleanup(context.Background(), HousekeepingOptions{
		All:     true,
		Volumes: true,
		Until:   "48h",
		Filters: []string{HousekeepingProtectFilter()},
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.SpaceFreed != "1.234GB" {
		t.Errorf("SpaceFreed = %q, want %q", res.SpaceFreed, "1.234GB")
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "system prune -f --all --volumes --filter until=48h --filter label!=tengiz-app"
	if got := strings.TrimSpace(string(data)); got != want {
		t.Errorf("docker args = %q, want %q", got, want)
	}
}

func TestDockerCleanupDryRunRunsSystemDF(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	fakeDocker := filepath.Join(dir, "docker")
	script := "#!/bin/sh\n" +
		fmt.Sprintf("printf '%%s\\n' \"$*\" > %s\n", argsFile) +
		"echo 'TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE'\n" +
		"echo 'Images          5         2         1.2GB     800MB (66%)'\n"
	if err := os.WriteFile(fakeDocker, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	rt := &dockerRuntime{}
	res, err := rt.Cleanup(context.Background(), HousekeepingOptions{DryRun: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if !strings.Contains(res.Output, "RECLAIMABLE") {
		t.Errorf("Output missing df table: %q", res.Output)
	}
	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(data)); got != "system df" {
		t.Errorf("docker args = %q, want %q", got, "system df")
	}
}
