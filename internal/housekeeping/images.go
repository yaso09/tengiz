package housekeeping

import "context"

func (m *Manager) danglingImages(ctx context.Context) ([]string, error) {
	data, err := m.exec(ctx, "images", "-q", "-f", "dangling=true")
	if err != nil {
		return nil, err
	}
	return splitLines(data), nil
}
