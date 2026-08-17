package runtime

import (
	"context"
	"testing"
)

func TestPruneCandidates(t *testing.T) {
	// Format: {{.ID}}|{{.Names}}|{{.State}}|{{.Labels}}
	psOutput := `abc123|tengiz-myapp|exited|tengiz-app=myapp,tengiz-env=production
def456|orphan-helper|exited|
ghi789|tengiz-preview|exited|tengiz-app=preview,tengiz-deployment=42
jkl012|running-other|running|
mno345|tengiz-myapp-12345|exited|tengiz-app=myapp`
	got := pruneCandidates(psOutput)
	want := []string{"def456"}
	if len(got) != len(want) {
		t.Fatalf("pruneCandidates() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pruneCandidates()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParsePruneDeleted(t *testing.T) {
	imageOut := `Deleted Images:
untagged: foo:latest
deleted: sha256:aaaa
untagged: bar:latest
deleted: sha256:bbbb

Total reclaimed space: 1.2kB`
	if got := parsePruneDeleted(imageOut, "Deleted Images:"); got != 4 {
		t.Errorf("parsePruneDeleted(images) = %d, want 4", got)
	}

	netOut := `Deleted Networks:
net1
net2

`
	if got := parsePruneDeleted(netOut, "Deleted Networks:"); got != 2 {
		t.Errorf("parsePruneDeleted(networks) = %d, want 2", got)
	}

	if got := parsePruneDeleted("Total reclaimed space: 0B", "Deleted Images:"); got != 0 {
		t.Errorf("parsePruneDeleted(empty) = %d, want 0", got)
	}
}

func TestParseTotalReclaimed(t *testing.T) {
	tests := []struct {
		output string
		want   string
	}{
		{"Deleted Images:\n...\n\nTotal reclaimed space: 1.787GB", "1.787GB"},
		{"Total:\t118B", "118B"},
		{"Total reclaimed space: 0B", "0B"},
		{"no total line here", "0B"},
	}
	for _, tc := range tests {
		if got := parseTotalReclaimed(tc.output); got != tc.want {
			t.Errorf("parseTotalReclaimed(%q) = %q, want %q", tc.output, got, tc.want)
		}
	}
}

func TestDockerPruneStubContract(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("stub Prune() error = %v", err)
	}
	if res == nil || res.TotalReclaimed != "" {
		t.Fatalf("unexpected stub result: %+v", res)
	}
}
