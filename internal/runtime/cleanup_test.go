package runtime

import (
	"context"
	"testing"
)

func TestStubRemoveImage(t *testing.T) {
	m := NewStub()
	if err := m.RemoveImage(context.Background(), "tengiz-apps/testapp:v1"); err != nil {
		t.Fatalf("RemoveImage() error = %v", err)
	}
}

func TestStubKeepLastNImages(t *testing.T) {
	m := NewStub()
	if err := m.KeepLastNImages(context.Background(), "testapp", 5); err != nil {
		t.Fatalf("KeepLastNImages() error = %v", err)
	}
}

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	report, err := m.Cleanup(context.Background(), CleanupOptions{Volumes: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if report == nil {
		t.Fatal("Cleanup() returned nil report")
	}
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 ||
		report.NetworksRemoved != 0 || report.VolumesRemoved != 0 || report.ReclaimedSpace != "" {
		t.Errorf("expected empty report, got %+v", report)
	}
}

func TestBuildCleanupArgs(t *testing.T) {
	args := buildCleanupArgs(CleanupOptions{})
	expected := []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"}
	if len(args) != len(expected) {
		t.Fatalf("buildCleanupArgs() = %v, want %v", args, expected)
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], expected[i])
		}
	}
}

func TestBuildCleanupArgsVolumes(t *testing.T) {
	args := buildCleanupArgs(CleanupOptions{Volumes: true})
	expected := []string{"system", "prune", "-f", "--volumes", "--filter", "label!=tengiz-app"}
	if len(args) != len(expected) {
		t.Fatalf("buildCleanupArgs() = %v, want %v", args, expected)
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], expected[i])
		}
	}
}

func TestParsePruneSummary(t *testing.T) {
	output := `WARNING! This will remove:
  - all stopped containers
  - all networks not used by at least one container
  - all dangling images
  - all dangling build cache

Deleted Containers:
f44f9b81948b3919590d5f79a680d8378f1139b41952e219830a33027c80c867
792776e68ac9d75bce4092bc1b5cc17b779bc926ab04f4185aec9bf1c0d4641f

Deleted Networks:
network1
network2

Deleted Images:
untagged: hello-world@sha256:f3b3b28a45160805bb16542c9531888519430e9e6d6ffc09d72261b0d26ff74f
deleted: sha256:1815c82652c03bfd8644afda26fb184f2ed891d921b20a0703b46768f9755c57
deleted: sha256:45761469c965421a92a69cc50e92c01e0cfa94fe026cdd1233445ea00e96289a

Deleted build cache objects:
zkvg3lzxi1fxy4r0e1hwaop6o

Total reclaimed space: 1.84kB
`
	report := parsePruneSummary(output)
	if report.ContainersRemoved != 2 {
		t.Errorf("ContainersRemoved = %d, want 2", report.ContainersRemoved)
	}
	if report.NetworksRemoved != 2 {
		t.Errorf("NetworksRemoved = %d, want 2", report.NetworksRemoved)
	}
	if report.ImagesRemoved != 3 {
		t.Errorf("ImagesRemoved = %d, want 3", report.ImagesRemoved)
	}
	if report.VolumesRemoved != 0 {
		t.Errorf("VolumesRemoved = %d, want 0", report.VolumesRemoved)
	}
	if report.ReclaimedSpace != "1.84kB" {
		t.Errorf("ReclaimedSpace = %q, want %q", report.ReclaimedSpace, "1.84kB")
	}
}

func TestParsePruneSummaryVolumes(t *testing.T) {
	output := `Deleted Volumes:
demo-app_postgres_data

Total reclaimed space: 1.24GB
`
	report := parsePruneSummary(output)
	if report.VolumesRemoved != 1 {
		t.Errorf("VolumesRemoved = %d, want 1", report.VolumesRemoved)
	}
	if report.ReclaimedSpace != "1.24GB" {
		t.Errorf("ReclaimedSpace = %q, want %q", report.ReclaimedSpace, "1.24GB")
	}
}

func TestParsePruneSummaryEmpty(t *testing.T) {
	report := parsePruneSummary("")
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 ||
		report.NetworksRemoved != 0 || report.VolumesRemoved != 0 || report.ReclaimedSpace != "" {
		t.Errorf("expected empty report, got %+v", report)
	}
}
