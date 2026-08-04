package runtime

import (
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