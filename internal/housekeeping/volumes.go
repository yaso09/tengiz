package housekeeping

import "context"

func (m *Manager) danglingVolumes(ctx context.Context) ([]string, error) {
	data, err := m.exec(ctx, "volume", "ls", "-q", "-f", "dangling=true")
	if err != nil {
		return nil, err
	}
	return splitLines(data), nil
}
