package housekeeping

import (
	"fmt"
)

const tengizLabelKey = "tengiz-app"

type dockerManager struct{}

func pruneArgs(cat Category) ([]string, error) {
	switch cat {
	case CategoryContainers:
		return []string{"container", "prune", "-f", "--filter", "label!=" + tengizLabelKey}, nil
	case CategoryImages:
		return []string{"image", "prune", "-f"}, nil
	case CategoryNetworks:
		return []string{"network", "prune", "-f"}, nil
	case CategoryCache:
		return []string{"builder", "prune", "-f"}, nil
	case CategoryVolumes:
		return []string{"volume", "prune", "-f"}, nil
	}
	return nil, fmt.Errorf("unknown category %q", cat)
}

func containerCandidatesArgs() []string {
	return []string{
		"ps", "-a",
		"--filter", "status=exited",
		"--filter", "label!=" + tengizLabelKey,
		"--format", "{{.ID}} {{.Names}}",
	}
}

func imageCandidatesArgs() []string {
	return []string{
		"images",
		"-f", "dangling=true",
		"--format", "{{.ID}} {{.Repository}}:{{.Tag}}",
	}
}

func networkCandidatesArgs() []string {
	return []string{
		"network", "ls",
		"--filter", "dangling=true",
		"--format", "{{.ID}} {{.Name}}",
	}
}