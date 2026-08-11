package runtime

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestParseIDs(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{"empty", "", []string{}},
		{"single", "abc123\n", []string{"abc123"}},
		{"multiple", "abc def\nghi\n", []string{"abc", "def", "ghi"}},
		{"extra whitespace", "  abc   def  \n", []string{"abc", "def"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseIDs(tt.output)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseIDs(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestPruneByIDs(t *testing.T) {
	var removed []string
	remove := func(ctx context.Context, id string) error {
		removed = append(removed, id)
		return nil
	}
	n := pruneByIDs(context.Background(), []string{"a", "b", "c"}, remove, false)
	if n != 3 {
		t.Errorf("removed count = %d, want 3", n)
	}
	if len(removed) != 3 {
		t.Errorf("remove called %d times, want 3", len(removed))
	}
}

func TestPruneByIDsDryRun(t *testing.T) {
	var removed []string
	remove := func(ctx context.Context, id string) error {
		removed = append(removed, id)
		return nil
	}
	n := pruneByIDs(context.Background(), []string{"a", "b"}, remove, true)
	if n != 2 {
		t.Errorf("dry-run count = %d, want 2", n)
	}
	if len(removed) != 0 {
		t.Errorf("dry-run must not remove anything, removed %v", removed)
	}
}

func TestPruneByIDsIgnoresErrors(t *testing.T) {
	remove := func(ctx context.Context, id string) error {
		if id == "bad" {
			return errors.New("boom")
		}
		return nil
	}
	n := pruneByIDs(context.Background(), []string{"ok", "bad", "ok2"}, remove, false)
	if n != 2 {
		t.Errorf("removed count = %d, want 2 (failures skipped)", n)
	}
}

func TestPruneContainerArgs(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{
			"containers",
			collectContainersArgs(),
			[]string{"ps", "-aq",
				"--filter", "status=exited",
				"--filter", "label!=tengiz-app",
				"--filter", "label!=tengiz-env"},
		},
		{"remove container", removeContainerArgs("abc"), []string{"rm", "-f", "abc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}
