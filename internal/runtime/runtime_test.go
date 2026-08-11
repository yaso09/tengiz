package runtime

import (
	"context"
	"strings"
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
	volumes := []types.VolumeConfig{
		{HostPath: "/data", ContainerPath: "/app/data", ReadOnly: false},
		{HostPath: "/config", ContainerPath: "/etc/config", ReadOnly: true},
	}
	args := volumeArgs(volumes)
	expected := []string{"-v", "/data:/app/data", "-v", "/config:/etc/config:ro"}
	if len(args) != len(expected) {
		t.Fatalf("expected %d args, got %d: %v", len(expected), len(args), args)
	}
	for i := range expected {
		if args[i] != expected[i] {
			t.Fatalf("arg %d: expected %q, got %q", i, expected[i], args[i])
		}
	}
}

func TestVolumeArgsNil(t *testing.T) {
	args := volumeArgs(nil)
	if args != nil {
		t.Fatalf("expected nil for nil volumes, got %v", args)
	}
}

func TestVolumeArgsEmpty(t *testing.T) {
	args := volumeArgs([]types.VolumeConfig{})
	if args != nil {
		t.Fatalf("expected nil for empty volumes, got %v", args)
	}
}

func TestGetContainerConfigVolumes(t *testing.T) {
	const inspectOutput = `[{"/host/data": "/app/data", "/host/config:/etc/config:ro": ""}]`
	_ = inspectOutput
}

func TestStubCreateFromImage(t *testing.T) {
	m := NewStub()
	cfg := &types.AppConfig{Name: "testapp", Port: 3000}
	err := m.CreateFromImage(context.Background(), cfg, "tengiz-apps/testapp:v1", 9001)
	if err != nil {
		t.Fatalf("CreateFromImage() error = %v", err)
	}
}

func TestLogOptionsBuildArgs(t *testing.T) {
	tests := []struct {
		name     string
		opts     LogOptions
		expected []string
	}{
		{
			name:     "no options",
			opts:     LogOptions{},
			expected: []string{"logs", "tengiz-myapp"},
		},
		{
			name:     "follow only",
			opts:     LogOptions{Follow: true},
			expected: []string{"logs", "-f", "tengiz-myapp"},
		},
		{
			name:     "tail 50",
			opts:     LogOptions{Tail: 50},
			expected: []string{"logs", "--tail", "50", "tengiz-myapp"},
		},
		{
			name:     "since 5m",
			opts:     LogOptions{Since: "5m"},
			expected: []string{"logs", "--since", "5m", "tengiz-myapp"},
		},
		{
			name:     "until 2024-01-01T00:00:00Z",
			opts:     LogOptions{Until: "2024-01-01T00:00:00Z"},
			expected: []string{"logs", "--until", "2024-01-01T00:00:00Z", "tengiz-myapp"},
		},
		{
			name:     "tail + follow + since",
			opts:     LogOptions{Follow: true, Tail: 100, Since: "1h"},
			expected: []string{"logs", "-f", "--tail", "100", "--since", "1h", "tengiz-myapp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildLogArgs("tengiz-myapp", tt.opts)
			if len(args) != len(tt.expected) {
				t.Errorf("len mismatch: got %v, want %v", args, tt.expected)
				return
			}
			for i := range args {
				if args[i] != tt.expected[i] {
					t.Errorf("arg[%d]: got %q, want %q", i, args[i], tt.expected[i])
				}
			}
		})
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

func TestStubRun(t *testing.T) {
	m := NewStub()
	cfg := &types.AppConfig{Name: "testapp"}
	err := m.Run(context.Background(), cfg, "tengiz-apps/testapp:latest", []string{"echo", "hello"}, RunOptions{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestStubPrune(t *testing.T) {
	m := NewStub()
	report, err := m.Prune(context.Background(), PruneOptions{
		Containers: true,
		Images:     true,
		Volumes:    true,
		Networks:   true,
	})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if report.ContainersRemoved != 0 || report.ImagesRemoved != 0 ||
		report.VolumesRemoved != 0 || report.NetworksRemoved != 0 {
		t.Errorf("expected empty report, got %+v", report)
	}
}

func TestStubRunInteractive(t *testing.T) {
	m := NewStub()
	cfg := &types.AppConfig{Name: "testapp", Env: map[string]string{"FOO": "bar"}}
	err := m.Run(context.Background(), cfg, "tengiz-apps/testapp:v1", []string{"bash"}, RunOptions{Interactive: true})
	if err != nil {
		t.Fatalf("Run(interactive) error = %v", err)
	}
}

func TestRunArgsWithExtraEnv(t *testing.T) {
	cfg := &types.AppConfig{
		Name: "myapp",
		Env:  map[string]string{"BASE_URL": "http://localhost"},
	}
	opts := RunOptions{
		ExtraEnv: map[string]string{"MIGRATION_STEP": "001", "FORCE": "true"},
	}
	args := buildRunArgs(cfg, "tengiz-apps/myapp:v1", []string{"python", "migrate.py"}, opts)
	got := strings.Join(args, " ")
	for _, want := range []string{"-e BASE_URL=http://localhost", "-e MIGRATION_STEP=001", "-e FORCE=true"} {
		if !strings.Contains(got, want) {
			t.Errorf("buildRunArgs() missing %q in %q", want, got)
		}
	}
}

func TestRunArgs(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *types.AppConfig
		imageTag string
		cmd      []string
		opts     RunOptions
		expected string
	}{
		{
			name:     "simple command",
			cfg:      &types.AppConfig{Name: "myapp"},
			imageTag: "tengiz-apps/myapp:latest",
			cmd:      []string{"echo", "hello"},
			opts:     RunOptions{},
			expected: "run --rm --label tengiz-app=myapp tengiz-apps/myapp:latest echo hello",
		},
		{
			name: "interactive with env",
			cfg:  &types.AppConfig{Name: "myapp", Env: map[string]string{"DATABASE_URL": "postgres://localhost:5432/db"}},
			imageTag: "tengiz-apps/myapp:v1",
			cmd:      []string{"bash"},
			opts:     RunOptions{Interactive: true},
			expected: "run --rm -it --label tengiz-app=myapp -e DATABASE_URL=postgres://localhost:5432/db tengiz-apps/myapp:v1 bash",
		},
		{
			name: "with volumes",
			cfg: &types.AppConfig{
				Name: "myapp",
				Volumes: []types.VolumeConfig{
					{HostPath: "/data", ContainerPath: "/app/data"},
				},
			},
			imageTag: "tengiz-apps/myapp:latest",
			cmd:      []string{"ls", "/app/data"},
			opts:     RunOptions{},
			expected: "-v /data:/app/data",
		},
		{
			name: "with resources",
			cfg: &types.AppConfig{
				Name:      "myapp",
				Resources: &types.ResourceConfig{Memory: "512m", CPU: "1.0"},
			},
			imageTag: "tengiz-apps/myapp:latest",
			cmd:      []string{"node", "script.js"},
			opts:     RunOptions{},
			expected: "--memory 512m --cpus 1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildRunArgs(tt.cfg, tt.imageTag, tt.cmd, tt.opts)
			got := strings.Join(args, " ")
			if !strings.Contains(got, tt.expected) {
				t.Errorf("buildRunArgs() = %q, want substring %q", got, tt.expected)
			}
		})
	}
}
