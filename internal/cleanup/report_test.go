package cleanup

import (
	"testing"
)

func TestFormatPruneReport(t *testing.T) {
	report := &PruneReport{
		ContainersRemoved: 3,
		ImagesRemoved:     5,
		VolumesRemoved:    1,
		NetworksRemoved:   0,
		BuildCacheFreed:   0,
		SpaceReclaimed:    1610612736,
	}
	before := &DiskInfo{ImagesTotal: 10, ImagesSize: "2GB"}
	after := &DiskInfo{ImagesTotal: 5, ImagesSize: "500MB"}

	formatted := report.Format(before, after)
	if !contains(formatted, "containers removed: 3") {
		t.Errorf("expected container count, got:\n%s", formatted)
	}
	if !contains(formatted, "images removed:     5") {
		t.Errorf("expected image count, got:\n%s", formatted)
	}
	if !contains(formatted, "1.5 GB") {
		t.Errorf("expected reclaimed space, got:\n%s", formatted)
	}
}

func TestFormatPruneReportDryRun(t *testing.T) {
	report := &PruneReport{
		ContainersRemoved: 3,
		ImagesRemoved:     5,
	}
	formatted := report.Format(nil, nil)
	if !contains(formatted, "DRY RUN") {
		t.Errorf("dry-run report should contain DRY RUN, got:\n%s", formatted)
	}
	if !contains(formatted, "containers removed: 3") {
		t.Errorf("expected container count in dry run, got:\n%s", formatted)
	}
	if !contains(formatted, "images removed:     5") {
		t.Errorf("expected image count in dry run, got:\n%s", formatted)
	}
}

func TestFormatPruneReportNoWork(t *testing.T) {
	report := &PruneReport{}
	formatted := report.Format(nil, nil)
	if !contains(formatted, "Nothing") {
		t.Errorf("empty report should show 'Nothing to clean', got:\n%s", formatted)
	}
}

func TestFormatPruneReportWithErrors(t *testing.T) {
	report := &PruneReport{
		ImagesRemoved: 2,
		Errors: []string{
			"container prune: exit status 1\nerror message",
		},
	}
	formatted := report.Format(nil, nil)
	if !contains(formatted, "Error") || !contains(formatted, "error message") {
		t.Errorf("expected errors in report, got:\n%s", formatted)
	}
}
