package cleanup

import (
	"context"
	"testing"
)

func TestStubSatisfiesInterface(t *testing.T) {
	m := NewStub()
	var iface Manager = m
	if iface == nil {
		t.Fatal("Manager interface not satisfied")
	}
}

func TestStubCleanupReturnsEmptyReport(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), Options{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 ||
		report.VolumesRemoved != 0 || report.NetworksRemoved != 0 {
		t.Fatalf("Cleanup() = %+v, want empty report", *report)
	}
}
