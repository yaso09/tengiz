package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestBuildPruneArgs(t *testing.T) {
	tests := []struct {
		name string
		opts PruneOptions
		want []string
	}{
		{
			name: "default keeps tengiz containers",
			opts: PruneOptions{All: false},
			want: []string{
				"system", "prune", "-f",
				"--filter", "label!=tengiz-app",
				"--filter", "label!=tengiz-env",
			},
		},
		{
			name: "all appends -a",
			opts: PruneOptions{All: true},
			want: []string{
				"system", "prune", "-f",
				"--filter", "label!=tengiz-app",
				"--filter", "label!=tengiz-env",
				"-a",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildPruneArgs(tc.opts)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("buildPruneArgs(%+v) = %v, want %v", tc.opts, got, tc.want)
			}
		})
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	out, err := m.Prune(context.Background(), PruneOptions{All: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if out != "" {
		t.Errorf("stub Prune() = %q, want empty string", out)
	}
}
