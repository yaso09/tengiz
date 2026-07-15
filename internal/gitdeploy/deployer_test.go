package gitdeploy

import (
	"context"
	"testing"
)

func TestExtractAppName(t *testing.T) {
	tests := []struct {
		repo string
		want string
	}{
		{"https://github.com/user/my-app.git", "my-app"},
		{"git@github.com:user/my_app.git", "my_app"},
		{"https://gitlab.com/group/sub/project.git", "project"},
	}
	for _, tc := range tests {
		got := extractAppName(tc.repo)
		if got != tc.want {
			t.Errorf("extractAppName(%q) = %q, want %q", tc.repo, got, tc.want)
		}
	}
}

func TestPipelineStartsDeploy(t *testing.T) {
	p := NewPipeline("/tmp/test-tengiz", nil, nil)
	err := p.Deploy(context.Background(), "https://github.com/user/nonexistent.git", "main", "github")
	if err == nil {
		t.Error("expected error for nonexistent repo")
	}
}
