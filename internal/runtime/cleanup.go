package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

type containerLine struct {
	Names  string
	State  string
	Labels string
}

func parseContainerLines(output string) []containerLine {
	var result []containerLine
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		var c containerLine
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue
		}
		result = append(result, c)
	}
	return result
}

func labelsToMap(labels string) map[string]string {
	m := make(map[string]string)
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			m[kv[0]] = kv[1]
		}
	}
	return m
}

func filterStaleContainers(containers []containerLine, env string, keep map[string]string) []string {
	if env == "" {
		env = "production"
	}
	var stale []string
	for _, c := range containers {
		labels := labelsToMap(c.Labels)
		deployment := labels["tengiz-deployment"]
		if deployment == "" {
			continue
		}
		if c.State == "running" {
			continue
		}
		cEnv := labels[envLabelKey]
		if cEnv == "" {
			cEnv = "production"
		}
		if cEnv != env {
			continue
		}
		if keep[labels[labelKey]] == deployment {
			continue
		}
		stale = append(stale, strings.TrimPrefix(c.Names, "/"))
	}
	return stale
}

func parseImageIDLines(output string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if line == "" {
			continue
		}
		ids = append(ids, line)
	}
	return ids
}

func parseReclaimedSpace(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Total reclaimed space:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Total reclaimed space:"))
		}
		if strings.HasPrefix(line, "Total:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "Total:"))
			v = strings.TrimSpace(strings.TrimPrefix(v, "\t"))
			if v != "" && v != "0B" {
				return v
			}
		}
	}
	return ""
}

func oldImageTags(output string, n int) []string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
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
	var old []string
	for i := 0; i < len(lines)-n; i++ {
		parts := strings.SplitN(lines[i], "|", 2)
		if len(parts) < 1 {
			continue
		}
		tag := parts[0]
		if strings.HasSuffix(tag, ":latest") || strings.HasSuffix(tag, "-latest") {
			continue
		}
		old = append(old, tag)
	}
	return old
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
	for _, tag := range oldImageTags(string(out), n) {
		if err := r.RemoveImage(ctx, tag); err != nil {
			log.Printf("[runtime] failed to remove old image %s: %v", tag, err)
		}
	}
	return nil
}