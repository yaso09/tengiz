package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
)

var containerPruneFilters = []string{
	"--filter", "status=exited",
	"--filter", "label!=tengiz-app",
	"--filter", "label!=tengiz-env",
}

func parseIDs(output string) []string {
	fields := strings.Fields(output)
	ids := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			ids = append(ids, f)
		}
	}
	return ids
}

func pruneByIDs(ctx context.Context, ids []string, remove func(context.Context, string) error, dryRun bool) int {
	removed := 0
	for _, id := range ids {
		if dryRun {
			removed++
			continue
		}
		if err := remove(ctx, id); err != nil {
			log.Printf("[runtime] cleanup: failed to remove %s: %v", id, err)
			continue
		}
		removed++
	}
	return removed
}

func collectContainersArgs() []string {
	return append([]string{"ps", "-aq"}, containerPruneFilters...)
}

func removeContainerArgs(id string) []string {
	return []string{"rm", "-f", id}
}

func execDocker(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	return cmd.CombinedOutput()
}

func collectRefs(ctx context.Context, args []string) ([]string, error) {
	out, err := execDocker(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", strings.Join(args, " "), err, string(out))
	}
	return parseIDs(string(out)), nil
}

func (r *dockerRuntime) removeContainer(ctx context.Context, id string) error {
	out, err := execDocker(ctx, removeContainerArgs(id)...)
	if err != nil {
		return fmt.Errorf("docker rm: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) (int, error) {
	ids, err := collectRefs(ctx, collectContainersArgs())
	if err != nil {
		return 0, err
	}
	return pruneByIDs(ctx, ids, r.removeContainer, dryRun), nil
}
