package cli

import (
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1500, "1.50 KB"},
		{2500000, "2.50 MB"},
		{3500000000, "3.50 GB"},
	}
	for _, tt := range tests {
		got := formatBytes(tt.input)
		if got != tt.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPrintCleanupReportEmpty(t *testing.T) {
	r := &types.CleanupReport{}
	printCleanupReport(r)
}

func TestPrintCleanupReportDryRun(t *testing.T) {
	r := &types.CleanupReport{
		ContainersRemoved: 0,
		ImagesRemoved:     0,
		VolumesRemoved:    0,
		BuildCacheFreed:   0,
		TotalSpaceFreed:   0,
		DryRun:            true,
	}
	printCleanupReport(r)
}

func TestPrintCleanupReportFull(t *testing.T) {
	r := &types.CleanupReport{
		ContainersRemoved: 3,
		ImagesRemoved:     12,
		VolumesRemoved:    1,
		BuildCacheFreed:   500000000,
		TotalSpaceFreed:   1500000000,
	}
	printCleanupReport(r)
}
