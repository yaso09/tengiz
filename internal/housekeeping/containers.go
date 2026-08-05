package housekeeping

import (
	"context"
	"encoding/json"
	"strings"
)

type containerInfo struct {
	ID     string `json:"ID"`
	Name   string `json:"Name"`
	State  string `json:"State"`
	Labels string `json:"Labels"`
}

func (m *Manager) containers(ctx context.Context) ([]containerInfo, error) {
	data, err := m.exec(ctx, "ps", "-a", "--format", "{{json .}}")
	if err != nil {
		return nil, err
	}
	var out []containerInfo
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var ci containerInfo
		if json.Unmarshal([]byte(line), &ci) == nil {
			out = append(out, ci)
		}
	}
	return out, nil
}

func (m *Manager) orphanContainers(ctx context.Context) ([]string, error) {
	infos, err := m.containers(ctx)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, ci := range infos {
		if ci.State != "exited" && ci.State != "created" {
			continue
		}
		if strings.Contains(ci.Labels, labelApp) {
			continue
		}
		names = append(names, ci.ID)
	}
	return names, nil
}
