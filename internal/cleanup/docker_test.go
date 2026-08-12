package cleanup

import (
	"context"
	"reflect"
	"testing"
)

func TestSplitLines(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a\nb\nc", []string{"a", "b", "c"}},
	}
	for _, tt := range tests {
		got := splitLines(tt.in)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("splitLines(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestBuildExitedContainerListArgs(t *testing.T) {
	got := buildExitedContainerListArgs()
	want := []string{"ps", "-a", "--filter", "status=exited", "--filter", "label!=tengiz-app", "--format", "{{.Names}}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildExitedContainerListArgs() = %v, want %v", got, want)
	}
}

func TestBuildContainerRemoveArgs(t *testing.T) {
	got := buildContainerRemoveArgs([]string{"c1", "c2"})
	want := []string{"rm", "c1", "c2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildContainerRemoveArgs() = %v, want %v", got, want)
	}
}

func TestStubPruneDoesNotCallDocker(t *testing.T) {
	m := NewStub()
	rep, err := m.Prune(context.Background(), Options{All: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if rep.Total() != 0 {
		t.Fatalf("expected empty report, got %+v", rep)
	}
}
