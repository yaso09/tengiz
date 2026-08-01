package runtime

import (
	"context"
	"testing"
)

func TestStubHousekeeperSatisfiesInterface(t *testing.T) {
	var iface Housekeeper = NewStubHousekeeper()
	if iface == nil {
		t.Fatal("stubHousekeeper does not implement Housekeeper")
	}
}

func TestNewStubHousekeeper(t *testing.T) {
	h := NewStubHousekeeper()
	if h == nil {
		t.Fatal("NewStubHousekeeper() returned nil")
	}
}

func TestPruneOptionsAny(t *testing.T) {
	if (PruneOptions{}).Any() {
		t.Error("empty PruneOptions should return false for Any()")
	}
	for name, opts := range map[string]PruneOptions{
		"containers": {Containers: true},
		"images":     {Images: true},
		"all-images": {AllImages: true},
		"volumes":    {Volumes: true},
		"networks":   {Networks: true},
		"cache":      {Cache: true},
	} {
		if !opts.Any() {
			t.Errorf("PruneOptions{%s}.Any() = false, want true", name)
		}
	}
}

func TestStubHousekeeperMethods(t *testing.T) {
	h := NewStubHousekeeper()
	ctx := context.Background()
	opts := PruneOptions{Containers: true, Images: true, Volumes: true, Networks: true, Cache: true}

	res, err := h.Prune(ctx, opts)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.ContainerOutput != "" || res.ImageOutput != "" || res.VolumeOutput != "" ||
		res.NetworkOutput != "" || res.CacheOutput != "" {
		t.Errorf("Prune() = %+v, want empty PruneResult", res)
	}

	dr, err := h.DryRun(ctx, opts)
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	if dr.Containers != 0 || dr.Images != 0 || dr.Volumes != 0 || dr.Networks != 0 || dr.Cache != 0 {
		t.Errorf("DryRun() = %+v, want zero DryRunResult", dr)
	}

	du, err := h.DiskUsage(ctx)
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if du != "" {
		t.Errorf("DiskUsage() = %q, want empty string", du)
	}
}
