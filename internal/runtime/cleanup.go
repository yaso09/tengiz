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

type unusedImage struct {
	ID  string
	Ref string
}

func splitRepoTag(s string) (string, string) {
	if i := strings.LastIndex(s, "/"); i != -1 {
		if j := strings.LastIndex(s[i:], ":"); j != -1 {
			return s[:i+j], s[i+j+1:]
		}
		return s, "latest"
	}
	if i := strings.LastIndex(s, ":"); i != -1 {
		return s[:i], s[i+1:]
	}
	return s, "latest"
}

func shortID(id string) string {
	s := strings.TrimPrefix(id, "sha256:")
	if len(s) > 12 {
		s = s[:12]
	}
	return s
}

func hasTengizLabel(labels string) bool {
	for _, pair := range strings.Split(labels, ",") {
		kv := strings.SplitN(strings.TrimSpace(pair), "=", 2)
		if len(kv) == 2 && kv[0] == CleanupProtectLabel {
			return true
		}
	}
	return false
}

func filterUnmanagedContainers(output string) []string {
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		name := strings.TrimSpace(parts[0])
		if name == "" {
			continue
		}
		labels := ""
		if len(parts) == 2 {
			labels = parts[1]
		}
		if hasTengizLabel(labels) {
			continue
		}
		names = append(names, name)
	}
	return names
}

func computeUnusedImages(allOutput string, referenced []string, all bool) []unusedImage {
	type imageInfo struct {
		id   string
		repo string
		tag  string
	}
	var images []imageInfo
	for _, line := range strings.Split(strings.TrimSpace(allOutput), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		repo, tag := splitRepoTag(strings.TrimSpace(parts[1]))
		images = append(images, imageInfo{id: strings.TrimSpace(parts[0]), repo: repo, tag: tag})
	}

	refs := make(map[string]bool)
	ids := make(map[string]bool)
	for _, ref := range referenced {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if strings.HasPrefix(ref, "sha256:") {
			ids[ref] = true
			continue
		}
		repo, tag := splitRepoTag(ref)
		if tag == "latest" {
			refs[repo] = true
		}
		refs[repo+":"+tag] = true
	}

	var out []unusedImage
	for _, img := range images {
		if strings.HasPrefix(img.repo, "tengiz-apps/") {
			continue
		}
		if img.repo == "<none>" {
			if ids[img.id] {
				continue
			}
			out = append(out, unusedImage{ID: img.id, Ref: shortID(img.id)})
			continue
		}
		if ids[img.id] || refs[img.repo+":"+img.tag] || (img.tag == "latest" && refs[img.repo]) {
			continue
		}
		if all {
			out = append(out, unusedImage{ID: img.id, Ref: img.repo + ":" + img.tag})
		}
	}
	return out
}

func parseDanglingVolumes(output string) []string {
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names
}

var defaultNetworks = map[string]bool{"bridge": true, "host": true, "none": true}

func computeUnusedNetworks(lines []string, inUse map[string]bool) []string {
	var out []string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		id, name := fields[0], fields[1]
		if defaultNetworks[name] || inUse[id] || inUse[name] {
			continue
		}
		out = append(out, name)
	}
	return out
}

func (r *dockerRuntime) Prune(ctx context.Context, opts PruneOptions) (PruneSummary, error) {
	var s PruneSummary
	s.DryRun = opts.DryRun

	if opts.Containers {
		items, reclaimed, err := r.pruneContainers(ctx, opts.DryRun)
		if err != nil {
			return s, err
		}
		s.Containers = items
		s.ReclaimedBytes += reclaimed
	}
	if opts.Images {
		items, reclaimed, err := r.pruneImages(ctx, opts.All, opts.DryRun)
		if err != nil {
			return s, err
		}
		s.Images = items
		s.ReclaimedBytes += reclaimed
	}
	if opts.Networks {
		items, reclaimed, err := r.pruneNetworks(ctx, opts.DryRun)
		if err != nil {
			return s, err
		}
		s.Networks = items
		s.ReclaimedBytes += reclaimed
	}
	if opts.Volumes {
		items, reclaimed, err := r.pruneVolumes(ctx, opts.DryRun)
		if err != nil {
			return s, err
		}
		s.Volumes = items
		s.ReclaimedBytes += reclaimed
	}
	if opts.BuildCache {
		size, reclaimed, err := r.pruneBuildCache(ctx, opts.DryRun)
		if err != nil {
			return s, err
		}
		s.BuildCacheSize = size
		s.ReclaimedBytes += reclaimed
	}
	return s, nil
}

func (r *dockerRuntime) pruneContainers(ctx context.Context, dryRun bool) ([]string, int64, error) {
	if dryRun {
		cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
			"--filter", "status=created",
			"--filter", "status=exited",
			"--format", "{{.Names}}\t{{.Labels}}")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, 0, fmt.Errorf("docker ps: %w\n%s", err, string(out))
		}
		return filterUnmanagedContainers(string(out)), 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "container", "prune", "-f",
		"--filter", "label!="+CleanupProtectLabel)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, 0, fmt.Errorf("docker container prune: %w\n%s", err, string(out))
	}
	reclaimed, err := parsePruneReclaimed(string(out))
	if err != nil {
		return nil, 0, err
	}
	return parsePruneItems(string(out), "Deleted Containers:"), reclaimed, nil
}

func (r *dockerRuntime) pruneImages(ctx context.Context, all, dryRun bool) ([]string, int64, error) {
	candidates, err := r.unusedImages(ctx, all)
	if err != nil {
		return nil, 0, err
	}
	var items []string
	var reclaimed int64
	for _, img := range candidates {
		size, err := r.imageSize(ctx, img.ID)
		if err == nil {
			reclaimed += size
		}
		if dryRun {
			items = append(items, img.Ref)
			continue
		}
		if err := r.RemoveImage(ctx, img.ID); err != nil {
			log.Printf("[runtime] cleanup: failed to remove image %s: %v", img.Ref, err)
			continue
		}
		items = append(items, img.Ref)
	}
	return items, reclaimed, nil
}

func (r *dockerRuntime) unusedImages(ctx context.Context, all bool) ([]unusedImage, error) {
	imgCmd := exec.CommandContext(ctx, "docker", "images", "-a",
		"--format", "{{.ID}}\t{{.Repository}}:{{.Tag}}")
	imgOut, err := imgCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker images: %w\n%s", err, string(imgOut))
	}
	psCmd := exec.CommandContext(ctx, "docker", "ps", "-a", "--format", "{{.Image}}")
	psOut, err := psCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w\n%s", err, string(psOut))
	}
	referenced := strings.Split(strings.TrimSpace(string(psOut)), "\n")
	return computeUnusedImages(string(imgOut), referenced, all), nil
}

func (r *dockerRuntime) imageSize(ctx context.Context, id string) (int64, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect",
		"--format", "{{.Size}}", id)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("docker image inspect: %w\n%s", err, string(out))
	}
	return strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
}

func (r *dockerRuntime) pruneNetworks(ctx context.Context, dryRun bool) ([]string, int64, error) {
	if dryRun {
		cmd := exec.CommandContext(ctx, "docker", "network", "ls", "--format", "{{.ID}} {{.Name}}")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, 0, fmt.Errorf("docker network ls: %w\n%s", err, string(out))
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		inUse := make(map[string]bool)
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			id := fields[0]
			cntCmd := exec.CommandContext(ctx, "docker", "network", "inspect",
				"--format", "{{len .Containers}}", id)
			cntOut, err := cntCmd.CombinedOutput()
			if err != nil {
				continue
			}
			if strings.TrimSpace(string(cntOut)) != "0" {
				inUse[id] = true
			}
		}
		return computeUnusedNetworks(lines, inUse), 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "network", "prune", "-f",
		"--filter", "label!="+CleanupProtectLabel)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, 0, fmt.Errorf("docker network prune: %w\n%s", err, string(out))
	}
	reclaimed, err := parsePruneReclaimed(string(out))
	if err != nil {
		return nil, 0, err
	}
	return parsePruneItems(string(out), "Deleted Networks:"), reclaimed, nil
}

func (r *dockerRuntime) pruneVolumes(ctx context.Context, dryRun bool) ([]string, int64, error) {
	if dryRun {
		cmd := exec.CommandContext(ctx, "docker", "volume", "ls", "-q",
			"--filter", "dangling=true")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, 0, fmt.Errorf("docker volume ls: %w\n%s", err, string(out))
		}
		return parseDanglingVolumes(string(out)), 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "volume", "prune", "-f",
		"--filter", "label!="+CleanupProtectLabel)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, 0, fmt.Errorf("docker volume prune: %w\n%s", err, string(out))
	}
	reclaimed, err := parsePruneReclaimed(string(out))
	if err != nil {
		return nil, 0, err
	}
	return parsePruneItems(string(out), "Deleted Volumes:"), reclaimed, nil
}

func (r *dockerRuntime) pruneBuildCache(ctx context.Context, dryRun bool) (int64, int64, error) {
	dfCmd := exec.CommandContext(ctx, "docker", "system", "df", "--format", "json")
	dfOut, err := dfCmd.CombinedOutput()
	var size int64
	if err == nil {
		size, err = parseSystemDFBuildCache(dfOut)
		if err != nil {
			log.Printf("[runtime] cleanup: failed to read build cache size: %v", err)
			size = 0
		}
	}
	if dryRun {
		return size, 0, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "builder", "prune", "-f", "-a")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return size, 0, fmt.Errorf("docker builder prune: %w\n%s", err, string(out))
	}
	reclaimed, err := parsePruneReclaimed(string(out))
	if err != nil {
		return size, 0, err
	}
	return size, reclaimed, nil
}
