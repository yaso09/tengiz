package cleanup

import (
	"encoding/json"
	"strings"
)

const (
	labelKey    = "tengiz-app"
	envLabelKey = "tengiz-env"
)

// stoppedContainer mirrors a `docker ps -a --format "{{json .}}"` record.
type stoppedContainer struct {
	ID     string
	Names  string
	Image  string
	Labels string
	State  string
}

func buildListContainersArgs() []string {
	return []string{"ps", "-a", "--format", "{{json .}}"}
}

func parseContainer(line string) (stoppedContainer, error) {
	var c stoppedContainer
	err := json.Unmarshal([]byte(line), &c)
	return c, err
}

func isStopped(state string) bool {
	switch state {
	case "exited", "created", "dead":
		return true
	default:
		return false
	}
}

func isTengizManaged(labels string) bool {
	for _, part := range strings.Split(labels, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		if kv[0] == labelKey || kv[0] == envLabelKey {
			return true
		}
	}
	return false
}

// partitionContainers splits stopped containers into those Tengiz may remove
// (unmanaged) and those that must be preserved (Tengiz-managed). Running,
// paused, and restarting containers are ignored entirely.
func partitionContainers(records []stoppedContainer) (remove, keep []stoppedContainer) {
	for _, c := range records {
		if !isStopped(c.State) {
			continue
		}
		if isTengizManaged(c.Labels) {
			keep = append(keep, c)
		} else {
			remove = append(remove, c)
		}
	}
	return remove, keep
}

func buildRemoveContainersArgs(ids []string) []string {
	return append([]string{"rm", "-f"}, ids...)
}

func ids(cs []stoppedContainer) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		if c.ID != "" {
			out = append(out, c.ID)
		}
	}
	return out
}

func names(cs []stoppedContainer) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		name := strings.TrimPrefix(c.Names, "/")
		if name == "" {
			name = c.ID
		}
		out = append(out, name)
	}
	return out
}