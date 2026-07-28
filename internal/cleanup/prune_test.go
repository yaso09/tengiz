package cleanup

import (
	"testing"
)

func TestPruneOptionsDefaults(t *testing.T) {
	opts := DefaultPruneOptions()
	if opts.DryRun {
		t.Error("DefaultPruneOptions().DryRun should be false")
	}
	if !opts.Containers {
		t.Error("DefaultPruneOptions().Containers should default true (system prune includes containers)")
	}
	if !opts.Images {
		t.Error("DefaultPruneOptions().Images should default true")
	}
	if opts.Volumes {
		t.Error("DefaultPruneOptions().Volumes should default false (volumes are excluded from system prune)")
	}
	if opts.Networks {
		t.Error("DefaultPruneOptions().Networks should default false")
	}
	if opts.BuildCache {
		t.Error("DefaultPruneOptions().BuildCache should default false")
	}
	if !opts.TengizSafe {
		t.Error("DefaultPruneOptions().TengizSafe should be true")
	}
}

func TestPruneOptionAll(t *testing.T) {
	opts := AllPruneOptions()
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Error("AllPruneOptions() should enable all categories")
	}
	if opts.TengizSafe {
		t.Error("AllPruneOptions().TengizSafe should be false (--all bypasses tengiz-app filter)")
	}
}

func TestRenderFiltersTengizSafe(t *testing.T) {
	opts := DefaultPruneOptions()
	opts.TengizSafe = true
	filters := renderFilters(opts, CategoryContainers)
	if len(filters) == 0 {
		t.Error("expected at least one filter for tengiz-safe mode")
	}
	hasExcludeLabel := false
	for _, f := range filters {
		if f == "label!=tengiz-app" {
			hasExcludeLabel = true
		}
	}
	if !hasExcludeLabel {
		t.Error("expected label!=tengiz-app filter in tengiz-safe mode")
	}
}

func TestRenderFiltersDanglingOnly(t *testing.T) {
	opts := DefaultPruneOptions()
	opts.KeepDangling = true
	filters := renderFilters(opts, CategoryImages)
	hasDangling := false
	for _, f := range filters {
		if f == "dangling=false" {
			hasDangling = true
		}
	}
	if !hasDangling {
		t.Error("expected dangling=false filter when KeepDangling is true")
	}
}
