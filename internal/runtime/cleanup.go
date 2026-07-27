package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func parsePruneOutput(out []byte) PruneReport {
	output := string(out)
	var reclaimed uint64
	re := regexp.MustCompile(`Total reclaimed space:\s+([0-9.]+)(kB|MB|GB|TB|B)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) == 3 {
		val, _ := strconv.ParseFloat(matches[1], 64)
		switch matches[2] {
		case "B":
			reclaimed = uint64(val)
		case "kB":
			reclaimed = uint64(val * 1024)
		case "MB":
			reclaimed = uint64(val * 1024 * 1024)
		case "GB":
			reclaimed = uint64(val * 1024 * 1024 * 1024)
		case "TB":
			reclaimed = uint64(val * 1024 * 1024 * 1024 * 1024)
		}
	}
	return PruneReport{
		ReclaimedBytes: reclaimed,
		Output:         output,
	}
}

func countLinesStartingWith(out []byte, prefix string) int {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	count := 0
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			count++
		}
	}
	return count
}

func (r *dockerRuntime) PruneContainers(ctx context.Context) (PruneReport, error) {
	cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f",
		"--filter", "label!=tengiz-app",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneReport{}, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	report := parsePruneOutput(out)
	report.ObjectsDeleted = countLinesStartingWith(out, "Deleted:")
	return report, nil
}

func (r *dockerRuntime) PruneImages(ctx context.Context) (PruneReport, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneReport{}, fmt.Errorf("docker image prune: %w\n%s", err, string(out))
	}
	report := parsePruneOutput(out)
	report.ObjectsDeleted = countLinesStartingWith(out, "Deleted:")
	return report, nil
}

func (r *dockerRuntime) PruneVolumes(ctx context.Context) (PruneReport, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneReport{}, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	report := parsePruneOutput(out)
	report.ObjectsDeleted = countLinesStartingWith(out, "Deleted:")
	return report, nil
}

func (r *dockerRuntime) PruneNetworks(ctx context.Context) (PruneReport, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneReport{}, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	report := parsePruneOutput(out)
	report.ObjectsDeleted = countLinesStartingWith(out, "Deleted:")
	return report, nil
}

func (r *dockerRuntime) PruneBuildCache(ctx context.Context) (PruneReport, error) {
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f", "-a")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return PruneReport{}, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	report := parsePruneOutput(out)
	report.ObjectsDeleted = countLinesStartingWith(out, "Deleted:")
	return report, nil
}

func (r *dockerRuntime) KeepLastNContainers(ctx context.Context, appName string, n int) error {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", fmt.Sprintf("name=tengiz-%s", appName),
		"--format", "{{.ID}}|{{.Names}}|{{.CreatedAt}}",
		"--no-trunc",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker ps: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) <= n {
		return nil
	}

	type entry struct {
		id   string
		name string
		time time.Time
	}
	var entries []entry
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(parts[2]))
		if err != nil {
			continue
		}
		entries = append(entries, entry{id: parts[0], name: parts[1], time: t})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].time.Before(entries[j].time)
	})

	for i := 0; i < len(entries)-n; i++ {
		cname := entries[i].name
		log.Printf("[runtime] removing old container %s", cname)
		rmCmd := exec.CommandContext(ctx, "docker", "rm", "-f", cname)
		if rmOut, rmErr := rmCmd.CombinedOutput(); rmErr != nil {
			log.Printf("[runtime] failed to remove old container %s: %v\n%s", cname, rmErr, string(rmOut))
		}
	}
	return nil
}

func (r *dockerRuntime) RemoveImage(ctx context.Context, imageTag string) error {
	cmd := exec.CommandContext(ctx, "docker", "rmi", "-f", imageTag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) KeepLastNImages(ctx context.Context, appName string, n int) error {
	cmd := exec.CommandContext(ctx, "docker", "images",
		"--filter", fmt.Sprintf("reference=tengiz-apps/%s:*", appName),
		"--format", "{{.Repository}}:{{.Tag}}|{{.CreatedAt}}",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker images: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) <= n {
		return nil
	}

	sort.Slice(lines, func(i, j int) bool {
		partsI := strings.SplitN(lines[i], "|", 2)
		partsJ := strings.SplitN(lines[j], "|", 2)
		if len(partsI) < 2 || len(partsJ) < 2 {
			return false
		}
		return partsI[1] < partsJ[1]
	})

	for i := 0; i < len(lines)-n; i++ {
		parts := strings.SplitN(lines[i], "|", 2)
		if len(parts) < 1 {
			continue
		}
		tag := parts[0]
		if strings.HasSuffix(tag, ":latest") {
			continue
		}
		if err := r.RemoveImage(ctx, tag); err != nil {
			log.Printf("[runtime] failed to remove old image %s: %v", tag, err)
		}
	}
	return nil
}
