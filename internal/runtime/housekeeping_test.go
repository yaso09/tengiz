package runtime

import "testing"

func TestCleanupResultEmpty(t *testing.T) {
	if !(CleanupResult{}).Empty() {
		t.Error("CleanupResult{} should be empty")
	}
	if (CleanupResult{Containers: []string{"abc"}}).Empty() {
		t.Error("result with containers should not be empty")
	}
	if (CleanupResult{Images: []string{"tag"}}).Empty() {
		t.Error("result with images should not be empty")
	}
	if (CleanupResult{Networks: []string{"net"}}).Empty() {
		t.Error("result with networks should not be empty")
	}
	if (CleanupResult{Volumes: []string{"vol"}}).Empty() {
		t.Error("result with volumes should not be empty")
	}
	if (CleanupResult{BuildCache: true}).Empty() {
		t.Error("result with build cache should not be empty")
	}
}
