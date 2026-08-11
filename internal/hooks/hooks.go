package hooks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Run executes each command in order via /bin/sh -c with the working
// directory set to dir. Output is streamed to the process's stdout/stderr.
// Execution stops at the first failure, and the deploy must abort.
func Run(ctx context.Context, dir string, commands []string) error {
	for _, command := range commands {
		fmt.Printf("[tengiz] pre_deploy: $ %s\n", command)
		cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
		cmd.Dir = dir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("pre_deploy hook %q failed: %w", command, err)
		}
	}
	return nil
}