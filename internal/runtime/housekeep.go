package runtime

import (
	"context"
	"encoding/json"
	"strings"
)

// CleanupOptions selects which categories of Docker objects to remove.
type CleanupOptions struct {
	Containers     bool
	Images         bool
	Volumes        bool
	Networks       bool
	BuildCache     bool
	DryRun         bool
	KeepContainers map[string]bool
	KeepImages     map[string]bool
}

// CleanupResult reports what was removed (or, in dry-run mode, what would be).
type CleanupResult struct {
	RemovedContainers []string
	RemovedImages     []string
	RemovedVolumes    []string
	RemovedNetworks   []string
	RemovedBuildCache []string
}

// Cleaner is implemented by runtimes that support disk housekeeping.
// It is deliberately separate from Manager so test mocks stay unaffected.
type Cleaner interface {
	Cleanup(ctx context.Context, opts CleanupOptions) (*CleanupResult, error)
}

type containerEntry struct {
	ID     string
	Names  string
	State  string
	Labels string
}

type imageEntry struct {
	Repository string
	Tag        string
	ID         string
}

func parseJSONLines[T any](out string) ([]T, error) {
	var result []T
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var v T
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, nil
}

// staleTengizContainers returns stopped tengiz container names to remove.
// Containers that never started or are still running and names present in
// keep are always protected.
func staleTengizContainers(entries []containerEntry, keep map[string]bool) []string {
	var out []string
	for _, e := range entries {
		name := strings.TrimPrefix(e.Names, "/")
		if name == "" {
			continue
		}
		if e.State != "exited" && e.State != "dead" {
			continue
		}
		if keep[name] {
			continue
		}
		out = append(out, name)
	}
	return out
}

// tengizImagesToRemove returns tengiz-apps image tags to remove.
// latest and <env>-latest aliases plus tags in keep are always protected.
func tengizImagesToRemove(images []imageEntry, keep map[string]bool) []string {
	var out []string
	for _, img := range images {
		if !strings.HasPrefix(img.Repository, "tengiz-apps/") {
			continue
		}
		if img.Tag == "" || img.Tag == "<none>" || img.Tag == "latest" {
			continue
		}
		if strings.HasSuffix(img.Tag, "-latest") {
			continue
		}
		full := img.Repository + ":" + img.Tag
		if keep[full] {
			continue
		}
		out = append(out, full)
	}
	return out
}
