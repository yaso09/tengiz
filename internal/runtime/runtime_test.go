package runtime

import (
	"context"
	"testing"

	"github.com/yaso09/tengiz/internal/types"
)

func TestNewStub(t *testing.T) {
	m := NewStub()
	if m == nil {
		t.Fatal("NewStub() returned nil")
	}
}

func TestStubSatisfiesInterface(t *testing.T) {
	m := NewStub()
	var iface Manager = m
	if iface == nil {
		t.Fatal("Manager interface not satisfied")
	}
}

func TestStubCreateVersioned(t *testing.T) {
	m := NewStub()
	cfg := &types.AppConfig{Name: "testapp", Port: 3000}
	err := m.CreateVersioned(context.Background(), cfg, "test:latest", 9000, "v2")
	if err != nil {
		t.Fatalf("CreateVersioned() error = %v", err)
	}
}

func TestStubRemoveBySuffix(t *testing.T) {
	m := NewStub()
	err := m.RemoveBySuffix(context.Background(), "testapp", "v2")
	if err != nil {
		t.Fatalf("RemoveBySuffix() error = %v", err)
	}
}

func TestCreateWithEnv(t *testing.T) {
	var m Manager = NewStub()
	cfg := &types.AppConfig{
		Name: "testapp",
		Env: map[string]string{
			"MY_VAR": "myval",
		},
	}
	if err := m.Create(context.Background(), cfg, "test:latest", 9000); err != nil {
		t.Fatalf("Create with env: %v", err)
	}
}

func TestCreateVersionedWithEnv(t *testing.T) {
	var m Manager = NewStub()
	cfg := &types.AppConfig{
		Name: "testapp",
		Env: map[string]string{
			"MY_VAR": "myval",
		},
	}
	if err := m.CreateVersioned(context.Background(), cfg, "test:latest", 9001, "v2"); err != nil {
		t.Fatalf("CreateVersioned with env: %v", err)
	}
}

func TestStubWaitForHealth(t *testing.T) {
	m := NewStub()
	hc := &types.HealthCheckConfig{Enabled: true, Endpoint: "/health", Timeout: 1}
	if err := m.WaitForHealth(context.Background(), "testapp", hc); err != nil {
		t.Fatalf("WaitForHealth() error = %v", err)
	}
}

func TestStubRestart(t *testing.T) {
	m := NewStub()
	if err := m.Restart(context.Background(), "testapp"); err != nil {
		t.Fatalf("Restart() error = %v", err)
	}
}

func TestResourceArgs(t *testing.T) {
	tests := []struct {
		name     string
		rc       *types.ResourceConfig
		expected []string
	}{
		{
			name:     "nil config",
			rc:       nil,
			expected: nil,
		},
		{
			name:     "both empty",
			rc:       &types.ResourceConfig{},
			expected: nil,
		},
		{
			name:     "memory only",
			rc:       &types.ResourceConfig{Memory: "512m"},
			expected: []string{"--memory", "512m"},
		},
		{
			name:     "cpu only",
			rc:       &types.ResourceConfig{CPU: "1.5"},
			expected: []string{"--cpus", "1.5"},
		},
		{
			name:     "both cpu and memory",
			rc:       &types.ResourceConfig{CPU: "2", Memory: "1g"},
			expected: []string{"--memory", "1g", "--cpus", "2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resourceArgs(tt.rc)
			if len(got) != len(tt.expected) {
				t.Fatalf("resourceArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("resourceArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestVolumeArgs(t *testing.T) {
	tests := []struct {
		name     string
		volumes  []types.VolumeMount
		expected []string
	}{
		{
			name:     "nil volumes",
			volumes:  nil,
			expected: nil,
		},
		{
			name:     "empty volumes",
			volumes:  []types.VolumeMount{},
			expected: nil,
		},
		{
			name:     "single host volume",
			volumes:  []types.VolumeMount{{HostPath: "/data", ContainerPath: "/app/data"}},
			expected: []string{"-v", "/data:/app/data"},
		},
		{
			name:     "read-only volume",
			volumes:  []types.VolumeMount{{HostPath: "/data", ContainerPath: "/app/data", ReadOnly: "true"}},
			expected: []string{"-v", "/data:/app/data:ro"},
		},
		{
			name:     "docker named volume",
			volumes:  []types.VolumeMount{{HostPath: "mydata", ContainerPath: "/var/lib/data"}},
			expected: []string{"-v", "mydata:/var/lib/data"},
		},
		{
			name: "multiple volumes",
			volumes: []types.VolumeMount{
				{HostPath: "/data", ContainerPath: "/app/data"},
				{HostPath: "/logs", ContainerPath: "/app/logs", ReadOnly: "true"},
			},
			expected: []string{"-v", "/data:/app/data", "-v", "/logs:/app/logs:ro"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := volumeArgs(tt.volumes)
			if len(got) != len(tt.expected) {
				t.Fatalf("volumeArgs() = %v (len=%d), want %v (len=%d)", got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Fatalf("volumeArgs()[%d] = %q, want %q", i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestCreateWithVolumes(t *testing.T) {
	var m Manager = NewStub()
	cfg := &types.AppConfig{
		Name: "testapp",
		Volumes: []types.VolumeMount{
			{HostPath: "/data", ContainerPath: "/app/data"},
		},
	}
	if err := m.Create(context.Background(), cfg, "test:latest", 9000); err != nil {
		t.Fatalf("Create with volumes: %v", err)
	}
}

func TestCreateVersionedWithVolumes(t *testing.T) {
	var m Manager = NewStub()
	cfg := &types.AppConfig{
		Name: "testapp",
		Volumes: []types.VolumeMount{
			{HostPath: "/data", ContainerPath: "/app/data"},
		},
	}
	if err := m.CreateVersioned(context.Background(), cfg, "test:latest", 9001, "v2"); err != nil {
		t.Fatalf("CreateVersioned with volumes: %v", err)
	}
}

func TestStubGetContainerPort(t *testing.T) {
	m := NewStub()
	port, err := m.GetContainerPort(context.Background(), "testapp", "v2")
	if err != nil {
		t.Fatalf("GetContainerPort() error = %v", err)
	}
	if port != 0 {
		t.Errorf("port = %d, want 0", port)
	}
}
