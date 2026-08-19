package runtime

import (
	"regexp"
	"strconv"
	"strings"
)

func cleanupContainerArgs() []string {
	return []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}
}

func cleanupImageArgs() []string {
	return []string{"image", "prune", "-f"}
}

func cleanupVolumeArgs() []string {
	return []string{"volume", "prune", "-f"}
}

func cleanupNetworkArgs() []string {
	return []string{"network", "prune", "-f"}
}

func cleanupCacheArgs() []string {
	return []string{"builder", "prune", "-f"}
}

func listContainerArgs() []string {
	return []string{"ps", "-a", "--filter", "label!=tengiz-app", "--filter", "status=exited", "--format", "{{.ID}}\t{{.Names}}\t{{.Status}}"}
}

func listDanglingImageArgs() []string {
	return []string{"images", "--filter", "dangling=true", "--format", "{{.ID}}\t{{.Repository}}:{{.Tag}}\t{{.Size}}"}
}

func listDanglingVolumeArgs() []string {
	return []string{"volume", "ls", "--filter", "dangling=true", "--format", "{{.Name}}"}
}

func listDanglingNetworkArgs() []string {
	return []string{"network", "ls", "--filter", "dangling=true", "--format", "{{.ID}}\t{{.Name}}"}
}

func cacheUsageArgs() []string {
	return []string{"system", "df"}
}

var reclaimedSpaceRe = regexp.MustCompile(`(?i)Total reclaimed space:\s*([0-9.]+)\s*([a-z]+)`)

func parseReclaimedBytes(output string) uint64 {
	m := reclaimedSpaceRe.FindStringSubmatch(output)
	if m == nil {
		return 0
	}
	val, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	var mult uint64 = 1
	switch strings.ToLower(m[2]) {
	case "kb":
		mult = 1000
	case "mb":
		mult = 1000 * 1000
	case "gb":
		mult = 1000 * 1000 * 1000
	case "tb":
		mult = 1000 * 1000 * 1000 * 1000
	case "pb":
		mult = 1000 * 1000 * 1000 * 1000 * 1000
	}
	return uint64(val * float64(mult))
}