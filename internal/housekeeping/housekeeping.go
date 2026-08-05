package housekeeping

import (
	"context"
	"strings"
)

const labelApp = "tengiz-app"

type execFunc func(ctx context.Context, args ...string) ([]byte, error)

type Options struct {
	Containers bool
	Images     bool
	Volumes    bool
	Networks   bool
	DryRun     bool
}

type Result struct {
	ContainersRemoved []string
	ImagesRemoved     []string
	VolumesRemoved    []string
	NetworksRemoved   []string
}

type Manager struct {
	exec execFunc
}

func NewManager(exec execFunc) *Manager {
	return &Manager{exec: exec}
}

func splitLines(data []byte) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
