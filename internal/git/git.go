package git

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func DefaultDestDir(repo string) string {
	name := repo
	name = strings.TrimSuffix(name, ".git")
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return name
}

func KeyPath(dataDir string) string {
	return filepath.Join(dataDir, "ssh", "id_ed25519")
}

func sshCommand(keyPath string) string {
	return fmt.Sprintf("ssh -o StrictHostKeyChecking=accept-new -i %s", keyPath)
}

func Clone(ctx context.Context, repo, branch, destDir, sshKeyPath string) error {
	if repo == "" {
		return fmt.Errorf("repo URL is required")
	}
	if branch == "" {
		branch = "main"
	}
	if destDir == "" {
		destDir = DefaultDestDir(repo)
	}

	args := []string{"clone", "--depth", "1", "--branch", branch, repo, destDir}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if sshKeyPath != "" {
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("GIT_SSH_COMMAND=%s", sshCommand(sshKeyPath)),
		)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone: %w", err)
	}
	return nil
}

func Pull(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "pull")
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git pull: %w", err)
	}
	return nil
}

func Checkout(ctx context.Context, dir, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "checkout", branch)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git checkout: %w", err)
	}
	return nil
}
