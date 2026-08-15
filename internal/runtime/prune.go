package runtime

import (
	"regexp"
	"strconv"
)

var reclaimedSpaceRe = regexp.MustCompile(`Total reclaimed space:\s*([0-9]+(?:\.[0-9]+)?)\s*([A-Za-z]+)`)

var sizeFactor = map[string]float64{
	"B":   1,
	"kB":  1e3,
	"KB":  1e3,
	"MB":  1e6,
	"GB":  1e9,
	"TB":  1e12,
	"KiB": 1 << 10,
	"MiB": 1 << 20,
	"GiB": 1 << 30,
	"TiB": 1 << 40,
}

func parseReclaimedSpace(output string) (uint64, bool) {
	matches := reclaimedSpaceRe.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return 0, false
	}
	m := matches[len(matches)-1]
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	factor, ok := sizeFactor[m[2]]
	if !ok {
		return 0, false
	}
	return uint64(val * factor), true
}

type CleanupTarget string

const (
	TargetContainers CleanupTarget = "containers"
	TargetImages     CleanupTarget = "images"
	TargetVolumes    CleanupTarget = "volumes"
	TargetNetworks   CleanupTarget = "networks"
	TargetBuilder    CleanupTarget = "builder"
)

type CleanupOptions struct {
	DryRun         bool
	PruneAllImages bool
	PruneVolumes   bool
}

type CleanupStats struct {
	SpaceReclaimed uint64
	Detail         string
}

func cleanupCommand(t CleanupTarget, pruneAllImages bool) []string {
	switch t {
	case TargetContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
	case TargetImages:
		args := []string{"image", "prune", "-f"}
		if pruneAllImages {
			args = append(args, "-a", "--filter", "reference!=tengiz-apps/*")
		}
		return args
	case TargetVolumes:
		return []string{"volume", "prune", "-f"}
	case TargetNetworks:
		return []string{"network", "prune", "-f", "--filter", "label!=tengiz-app"}
	case TargetBuilder:
		return []string{"builder", "prune", "-f"}
	default:
		return nil
	}
}