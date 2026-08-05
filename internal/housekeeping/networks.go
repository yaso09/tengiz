package housekeeping

import "context"

func (m *Manager) danglingNetworks(ctx context.Context) ([]string, error) {
	data, err := m.exec(ctx, "network", "ls", "-q", "-f", "dangling=true")
	if err != nil {
		return nil, err
	}
	return splitLines(data), nil
}
