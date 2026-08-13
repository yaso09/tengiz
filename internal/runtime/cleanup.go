package runtime

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

func parseIDList(output string) []string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var ids []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	return ids
}

// containerListArgs returns `docker ps -aq` filters for stopped/created
// containers. Without appName, Tengiz-managed containers (label tengiz-app)
// are excluded. With appName, only that app's containers are matched.
func containerListArgs(appName string) []string {
	args := []string{"ps", "-aq", "--filter", "status=exited", "--filter", "status=created"}
	if appName != "" {
		args = append(args, "--filter", fmt.Sprintf("label=%s=%s", labelKey, appName))
	} else {
		args = append(args, "--filter", fmt.Sprintf("label!=%s", labelKey))
	}
	return args
}

func (r *dockerRuntime) listContainerIDs(ctx context.Context, appName string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", containerListArgs(appName)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(out))
	}
	return parseIDList(string(out)), nil
}

func (r *dockerRuntime) removeContainers(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"rm", "-f"}, ids...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rm: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) listDanglingImages(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "images", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(out))
	}
	return parseIDList(string(out)), nil
}

func (r *dockerRuntime) removeImages(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"rmi", "-f"}, ids...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rmi: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) listUnusedNetworks(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "network", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
	}
	return parseIDList(string(out)), nil
}

func (r *dockerRuntime) removeNetworks(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"network", "rm"}, ids...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker network rm: %w\n%s", err, string(out))
	}
	return nil
}

func (r *dockerRuntime) listUnusedVolumes(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-q", "--filter", "dangling=true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
	}
	return parseIDList(string(out)), nil
}

func (r *dockerRuntime) removeVolumes(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"volume", "rm"}, ids...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker volume rm: %w\n%s", err, string(out))
	}
	return nil
}

var spaceUnits = map[string]int64{
	"B":   1,
	"kB":  1e3,
	"MB":  1e6,
	"GB":  1e9,
	"TB":  1e12,
	"KiB": 1 << 10,
	"MiB": 1 << 20,
	"GiB": 1 << 30,
	"TiB": 1 << 40,
}

// parseReclaimedSpace extracts the "Total reclaimed space:" value from a
// docker prune command's output and converts it to bytes.
func parseReclaimedSpace(output string) int64 {
	const marker = "Total reclaimed space:"
	idx := strings.Index(output, marker)
	if idx < 0 {
		return 0
	}
	rest := strings.TrimSpace(output[idx+len(marker):])
	if rest == "" {
		return 0
	}
	numEnd := 0
	for numEnd < len(rest) {
		c := rest[numEnd]
		if (c >= '0' && c <= '9') || c == '.' || c == ',' {
			numEnd++
			continue
		}
		break
	}
	if numEnd == 0 {
		return 0
	}
	val, err := strconv.ParseFloat(strings.Replace(rest[:numEnd], ",", ".", 1), 64)
	if err != nil {
		return 0
	}
	unit := strings.TrimSpace(rest[numEnd:])
	mult, ok := spaceUnits[unit]
	if !ok {
		return 0
	}
	return int64(val * float64(mult))
}

// FormatBytes renders a byte count in a compact human-readable form.
func FormatBytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	units := []string{"B", "kB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	return fmt.Sprintf("%.2f %s", f, units[i])
}

func (r *dockerRuntime) pruneCache(ctx context.Context, dryRun bool) (int64, error) {
	if dryRun {
		// docker builder prune has no dry-run mode; report intent.
		return -1, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	return parseReclaimedSpace(string(out)), nil
}

func (r *dockerRuntime) Prune(ctx context.Context, opts CleanupOptions) (CleanupResult, error) {
	return CleanupResult{}, nil
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
