package runtime

import (
	"context"
	"strings"
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

func TestPruneContainersArgs(t *testing.T) {
	expected := []string{
		"container", "prune", "-f",
		"--filter", "label!=tengiz-app",
		"--filter", "label!=tengiz-env",
	}
	got := pruneContainersArgs()
	if len(got) != len(expected) {
		t.Fatalf("pruneContainersArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestPruneImagesArgs(t *testing.T) {
	got := pruneImagesArgs()
	expected := []string{"image", "prune", "-f"}
	if len(got) != len(expected) {
		t.Fatalf("pruneImagesArgs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestPruneVolumesNetworksCacheArgs(t *testing.T) {
	if got := pruneVolumesArgs(); strings.Join(got, " ") != "volume prune -f" {
		t.Fatalf("pruneVolumesArgs() = %v", got)
	}
	if got := pruneNetworksArgs(); strings.Join(got, " ") != "network prune -f" {
		t.Fatalf("pruneNetworksArgs() = %v", got)
	}
	if got := pruneCacheArgs(); strings.Join(got, " ") != "builder prune -f" {
		t.Fatalf("pruneCacheArgs() = %v", got)
	}
}

func TestReclaimedFromOutput(t *testing.T) {
	tests := []struct {
		name string
		out  []byte
		want string
	}{
		{"empty output", []byte(""), ""},
		{"container prune", []byte("Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 1.234GB\n"), "1.234GB"},
		{"builder prune", []byte("Total:\t0B\n"), "0B"},
		{"no reclaim line", []byte("nothing happened here"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := reclaimedFromOutput(tt.out); got != tt.want {
				t.Errorf("reclaimedFromOutput(%q) = %q, want %q", tt.out, got, tt.want)
			}
		})
	}
}

func TestNonTengizImageRefs(t *testing.T) {
	out := `tengiz-apps/myapp:latest
tengiz-apps/myapp:1700000000
ghcr.io/foo/bar:latest
node:20
<none>:<none>
`
	got := nonTengizImageRefs(out)
	expected := []string{"ghcr.io/foo/bar:latest", "node:20"}
	if len(got) != len(expected) {
		t.Fatalf("nonTengizImageRefs() = %v, want %v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("ref[%d] = %q, want %q", i, got[i], expected[i])
		}
	}
}

func TestCleanupCommands(t *testing.T) {
	cmds := CleanupCommands(CleanupOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
		Cache:      true,
	})
	if len(cmds) != 5 {
		t.Fatalf("expected 5 commands, got %d: %+v", len(cmds), cmds)
	}
	if strings.Join(cmds[0].Args, " ") != "container prune -f --filter label!=tengiz-app --filter label!=tengiz-env" {
		t.Fatalf("first command = %v", cmds[0].Args)
	}
	if strings.Join(cmds[4].Args, " ") != "builder prune -f" {
		t.Fatalf("last command = %v", cmds[4].Args)
	}
	if len(CleanupCommands(CleanupOptions{})) != 0 {
		t.Fatal("expected no commands for empty options")
	}
}
