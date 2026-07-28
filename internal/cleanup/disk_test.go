package cleanup

import (
	"testing"
)

func TestDiskInfoFormat(t *testing.T) {
	info := &DiskInfo{
		ImagesTotal:     5,
		ImagesSize:      "1.2GB",
		ContainersTotal: 3,
		ContainersSize:  "50MB",
		VolumesTotal:    2,
		VolumesSize:     "800MB",
		BuildCacheSize:  "200MB",
	}
	formatted := info.Format()
	if !contains(formatted, "1.2GB") {
		t.Errorf("Format() missing Images size, got:\n%s", formatted)
	}
	if !contains(formatted, "Containers") {
		t.Errorf("Format() missing Containers section, got:\n%s", formatted)
	}
	if !contains(formatted, "200MB") {
		t.Errorf("Format() missing Build Cache size, got:\n%s", formatted)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
