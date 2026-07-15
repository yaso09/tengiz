package git

import (
	"context"
	"testing"
)

func TestDefaultDestDir(t *testing.T) {
	tests := []struct {
		repo string
		want string
	}{
		{"git@github.com:user/myapp.git", "myapp"},
		{"https://github.com/user/myapp.git", "myapp"},
		{"https://gitlab.com/group/sub-group/project.git", "project"},
	}
	for _, tc := range tests {
		got := DefaultDestDir(tc.repo)
		if got != tc.want {
			t.Errorf("DefaultDestDir(%q) = %q, want %q", tc.repo, got, tc.want)
		}
	}
}

func TestKeyPath(t *testing.T) {
	path := KeyPath("/tmp/.tengiz")
	want := "/tmp/.tengiz/ssh/id_ed25519"
	if path != want {
		t.Errorf("KeyPath = %q, want %q", path, want)
	}
}

func TestCloneDryRun(t *testing.T) {
	err := Clone(context.Background(), "", "main", "/tmp/nonexistent", "")
	if err == nil {
		t.Error("expected error for empty repo")
	}
}
