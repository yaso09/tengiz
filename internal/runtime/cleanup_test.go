package runtime

import (
	"context"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{All: true, Volumes: true})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if res.ImagesRemoved != 0 || res.ContainersRemoved != 0 {
		t.Errorf("expected zeroed result, got %+v", res)
	}
}

func TestStubSatisfiesCleanup(t *testing.T) {
	m := NewStub()
	var iface Manager = m
	if iface == nil {
		t.Fatal("Manager interface not satisfied")
	}
}

func TestBuildCleanupArgs(t *testing.T) {
	tests := []struct {
		name string
		opts CleanupOptions
		want []string
	}{
		{
			name: "default",
			opts: CleanupOptions{},
			want: []string{"system", "prune", "-f"},
		},
		{
			name: "all",
			opts: CleanupOptions{All: true},
			want: []string{"system", "prune", "-f", "--all"},
		},
		{
			name: "volumes",
			opts: CleanupOptions{Volumes: true},
			want: []string{"system", "prune", "-f", "--volumes"},
		},
		{
			name: "all and volumes",
			opts: CleanupOptions{All: true, Volumes: true},
			want: []string{"system", "prune", "-f", "--all", "--volumes"},
		},
		{
			name: "with app filter",
			opts: CleanupOptions{App: "myapp"},
			want: []string{"system", "prune", "-f", "--filter", "label!=tengiz-app"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCleanupArgs(tt.opts)
			if len(got) != len(tt.want) {
				t.Fatalf("buildCleanupArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("buildCleanupArgs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}