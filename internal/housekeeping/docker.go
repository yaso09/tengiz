package housekeeping

import (
	"context"
	"fmt"
	"os/exec"
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
func NewDocker() (Manager, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker not found in PATH: %w", err)
	}
	return &dockerManager{}, nil
}

func (m *dockerManager) DiskUsage(ctx context.Context) (*Usage, error) {
	cmd := exec.CommandContext(ctx, "docker", "system", "df")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker system df: %w\n%s", err, string(out))
	}
	return parseDfOutput(string(out))
}

func (m *dockerManager) Prune(ctx context.Context, opts Options) (*PruneResult, error) {
	cats := opts.Categories
	if len(cats) == 0 {
		cats = DefaultCategories
	}

	result := &PruneResult{
		Applied:             opts.Apply,
		ReclaimedByCategory: make(map[Category]int64),
	}

	if !opts.Apply {
		for _, cat := range cats {
			switch cat {
			case CategoryContainers:
				cands, err := m.listCandidates(ctx, cat, containerCandidatesArgs())
				if err != nil {
					return nil, err
				}
				result.Candidates = append(result.Candidates, cands...)
			case CategoryImages:
				cands, err := m.listCandidates(ctx, cat, imageCandidatesArgs())
				if err != nil {
					return nil, err
				}
				result.Candidates = append(result.Candidates, cands...)
			case CategoryNetworks:
				cands, err := m.listCandidates(ctx, cat, networkCandidatesArgs())
				if err != nil {
					return nil, err
				}
				result.Candidates = append(result.Candidates, cands...)
			case CategoryCache:
				// build cache cannot be enumerated cheaply; the CLI reports
				// its reclaimable bytes from DiskUsage
			}
		}
		return result, nil
	}

	for _, cat := range cats {
		args, err := pruneArgs(cat)
		if err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
		}
		if reclaimed, perr := parseReclaimed(string(out)); perr == nil {
			result.ReclaimedBytes += reclaimed
			result.ReclaimedByCategory[cat] = reclaimed
		}
	}
	return result, nil
}

func (m *dockerManager) listCandidates(ctx context.Context, cat Category, args []string) ([]Candidate, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker %s: %w\n%s", args[0], err, string(out))
	}
	return parseCandidates(string(out), cat), nil
}
