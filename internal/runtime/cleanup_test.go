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

func TestStubPrune(t *testing.T) {
	m := NewStub()
	res, err := m.Prune(context.Background(), PruneOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.DryRun {
		t.Error("Prune() result DryRun = true, want false")
	}
}

func TestPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		opts PruneOptions
		want []string
	}{
		{
			name: "default",
			opts: PruneOptions{},
			want: []string{"system", "prune", "--force", "--filter", "label!=tengiz-app"},
		},
		{
			name: "all",
			opts: PruneOptions{All: true},
			want: []string{"system", "prune", "--force", "--filter", "label!=tengiz-app", "--all"},
		},
		{
			name: "volumes",
			opts: PruneOptions{Volumes: true},
			want: []string{"system", "prune", "--force", "--filter", "label!=tengiz-app", "--volumes"},
		},
		{
			name: "all and volumes",
			opts: PruneOptions{All: true, Volumes: true},
			want: []string{"system", "prune", "--force", "--filter", "label!=tengiz-app", "--all", "--volumes"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pruneArgs(tt.opts)
			if len(got) != len(tt.want) {
				t.Fatalf("pruneArgs(%+v) = %v (len=%d), want %v (len=%d)", tt.opts, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("pruneArgs(%+v)[%d] = %q, want %q", tt.opts, i, got[i], tt.want[i])
				}
			}
		})
	}
}
