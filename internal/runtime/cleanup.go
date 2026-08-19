package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

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

const CleanupProtectLabel = "tengiz-app"

type PruneOptions struct {
	Containers bool
	Images     bool
	Networks   bool
	Volumes    bool
	BuildCache bool
	All        bool
	DryRun     bool
}

type PruneSummary struct {
	Containers     []string
	Images         []string
	Networks       []string
	Volumes        []string
	BuildCacheSize int64
	ReclaimedBytes int64
	DryRun         bool
}

var byteMultipliers = map[string]float64{
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

func parseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("parse byte size: empty string")
	}
	i := 0
	for i < len(s) {
		c := s[i]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' {
			i++
			continue
		}
		break
	}
	if i == 0 {
		return 0, fmt.Errorf("parse byte size: no numeric prefix in %q", s)
	}
	num, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, fmt.Errorf("parse byte size %q: %w", s, err)
	}
	unit := strings.TrimSpace(s[i:])
	mult, ok := byteMultipliers[unit]
	if !ok {
		return 0, fmt.Errorf("parse byte size: unknown unit %q in %q", unit, s)
	}
	return int64(num * mult), nil
}

func parsePruneReclaimed(output string) (int64, error) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Total reclaimed space: "):
			return parseByteSize(strings.TrimPrefix(line, "Total reclaimed space: "))
		case strings.HasPrefix(line, "Total:"):
			return parseByteSize(strings.TrimPrefix(line, "Total:"))
		}
	}
	return 0, nil
}

func parsePruneItems(output, header string) []string {
	var items []string
	inSection := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, header) {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if line == "" {
			break
		}
		items = append(items, line)
	}
	return items
}

func parseSystemDFBuildCache(rows []byte) (int64, error) {
	for _, line := range strings.Split(string(rows), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row struct {
			Type string `json:"Type"`
			Size string `json:"Size"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if row.Type == "Build Cache" {
			return parseByteSize(row.Size)
		}
	}
	return 0, nil
}
