package builder

import (
	"context"
	"testing"
)

type mockStrategy struct {
	imageTag string
	buildLog string
	err      error
}

func (m *mockStrategy) Build(ctx context.Context, dir, appName, env, deploymentID string, detection *Detection) (string, string, error) {
	return m.imageTag, m.buildLog, m.err
}

func TestBuilderDelegatesToStrategy(t *testing.T) {
	b := New("/tmp/test-data")
	mock := &mockStrategy{imageTag: "tengiz-apps/test:v1", buildLog: "build ok"}
	b.SetStrategy(mock)

	tag, log, err := b.Build(context.Background(), "/tmp/dir", "test", "production", &Detection{Framework: FrameworkStatic}, "v1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tag != "tengiz-apps/test:v1" {
		t.Errorf("expected tag tengiz-apps/test:v1, got %s", tag)
	}
	if log != "build ok" {
		t.Errorf("expected log 'build ok', got %s", log)
	}
}
