package gitdeploy

import (
	"context"
	"os"
	"path/filepath"
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

func TestPipelineDeployWithNixpacksDetectionOverride(t *testing.T) {
	t.Skip("integration test requires Docker + nixpacks")
}

func TestPipelineStartsDeploy(t *testing.T) {
	p := NewPipeline("/tmp/test-tengiz", nil, nil)
	err := p.Deploy(context.Background(), "https://github.com/user/nonexistent.git", "main", "github")
	if err == nil {
		t.Error("expected error for nonexistent repo")
	}
}

func TestRunPreDeployHooks(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte("name: myapp\npre_deploy:\n  - touch hook-ran\n"), 0644)
	p := NewPipeline(t.TempDir(), nil, nil)
	if err := p.runPreDeployHooks(context.Background(), dir); err != nil {
		t.Fatalf("runPreDeployHooks() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hook-ran")); err != nil {
		t.Errorf("expected hook to run: %v", err)
	}
}

func TestRunPreDeployHooksMissingConfig(t *testing.T) {
	dir := t.TempDir()
	p := NewPipeline(t.TempDir(), nil, nil)
	if err := p.runPreDeployHooks(context.Background(), dir); err != nil {
		t.Fatalf("runPreDeployHooks() with no config error = %v", err)
	}
}

func TestRunPreDeployHooksFailure(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte("name: myapp\npre_deploy:\n  - exit 1\n"), 0644)
	p := NewPipeline(t.TempDir(), nil, nil)
	if err := p.runPreDeployHooks(context.Background(), dir); err == nil {
		t.Fatal("runPreDeployHooks() expected error for failing hook")
	}
}
