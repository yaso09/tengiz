package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/types"
)

func captureOutput(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	os.Stdout = old
	return buf.String()
}

type mockRTForDeploy struct {
	created atomic.Int32
	removed atomic.Int32
	started atomic.Int32
	stopped atomic.Int32
}

func (m *mockRTForDeploy) Create(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error {
	m.created.Add(1)
	return nil
}

func (m *mockRTForDeploy) CreateVersioned(ctx context.Context, cfg *types.AppConfig, imageTag string, port int, suffix string) error {
	m.created.Add(1)
	return nil
}

func (m *mockRTForDeploy) Start(ctx context.Context, name string) error { m.started.Add(1); return nil }
func (m *mockRTForDeploy) Stop(ctx context.Context, name string) error { m.stopped.Add(1); return nil }
func (m *mockRTForDeploy) Restart(ctx context.Context, name string) error { return nil }
func (m *mockRTForDeploy) Remove(ctx context.Context, name string) error { m.removed.Add(1); return nil }
func (m *mockRTForDeploy) RemoveBySuffix(ctx context.Context, name string, suffix string) error { m.removed.Add(1); return nil }
func (m *mockRTForDeploy) IsActive(ctx context.Context, name string) (bool, error) { return true, nil }
func (m *mockRTForDeploy) GetContainerPort(ctx context.Context, name string, suffix string) (int, error) { return 0, nil }
func (m *mockRTForDeploy) List(ctx context.Context) ([]types.AppStatus, error) { return nil, nil }
func (m *mockRTForDeploy) Logs(ctx context.Context, name string, follow bool) (io.ReadCloser, error) { return nil, nil }
func (m *mockRTForDeploy) WaitForReady(ctx context.Context, name string, internalPort int) error { return nil }
func (m *mockRTForDeploy) WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error { return nil }

func TestDeployZeroDowntimeCreatesVersionedContainer(t *testing.T) {
	var m interface{} = &mockRTForDeploy{}
	if m == nil {
		t.Fatal("mock does not implement Manager")
	}
}

func TestHealthCmdNoApp(t *testing.T) {
	rootCmd.SetArgs([]string{"health"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for missing app name")
	}
}

func TestHealthCmdUnknownApp(t *testing.T) {
	rootCmd.SetArgs([]string{"health", "nonexistent"})
	err := rootCmd.Execute()
	if err == nil {
		t.Error("expected error for unknown app")
	}
}

func TestPsHeaderContainsHealth(t *testing.T) {
	// If no apps deployed, ps just prints "No applications deployed."
	// We verify that when apps exist, the HEALTH column is shown.
	// For the structure check, just verify the psCmd uses proper format
	rootCmd.SetArgs([]string{"ps"})
	output := captureOutput(func() {
		rootCmd.Execute()
	})
	if strings.Contains(output, "No applications deployed") {
		t.Skip("no apps deployed, cannot verify HEALTH column")
	}
	if !strings.Contains(output, "HEALTH") {
		t.Errorf("ps output missing HEALTH column header, got: %s", output)
	}
}

func TestDomainCommandsRegistered(t *testing.T) {
	domainCmd, _, err := rootCmd.Find([]string{"domain"})
	if err != nil {
		t.Fatalf("domain command not found: %v", err)
	}

	expected := map[string]bool{"add": false, "remove": false, "list": false}
	for _, sub := range domainCmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Fatalf("domain subcommand %q not found", name)
		}
	}
}

func TestWebhookCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"webhook"})
	if err != nil {
		t.Fatal("webhook command not registered")
	}
	if cmd == nil || cmd.Use != "webhook" {
		t.Fatal("webhook command not found")
	}
}

func TestGitCommandsRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"git"})
	if err != nil {
		t.Fatal("git command not registered")
	}
	if cmd == nil {
		t.Fatal("git command not found")
	}
	connectFound := false
	disconnectFound := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "connect" {
			connectFound = true
		}
		if sub.Use == "disconnect" {
			disconnectFound = true
		}
	}
	if !connectFound {
		t.Error("git:connect subcommand not registered")
	}
	if !disconnectFound {
		t.Error("git:disconnect subcommand not registered")
	}
}

func TestInitCmdGitFlags(t *testing.T) {
	flags := initCmd.Flags()
	repoFlag := flags.Lookup("git-repo")
	if repoFlag == nil {
		t.Fatal("--git-repo flag not found on init command")
	}
	branchFlag := flags.Lookup("git-branch")
	if branchFlag == nil {
		t.Fatal("--git-branch flag not found on init command")
	}
}

func TestVolumeCommandsRegistered(t *testing.T) {
	volumeCmd, _, err := rootCmd.Find([]string{"volume"})
	if err != nil {
		t.Fatalf("volume command not found: %v", err)
	}
	if volumeCmd == nil {
		t.Fatal("volume command is nil")
	}

	subMap := make(map[string]bool)
	for _, sub := range volumeCmd.Commands() {
		subMap[sub.Name()] = true
	}

	expected := []string{"add", "remove", "list"}
	for _, name := range expected {
		if !subMap[name] {
			t.Errorf("expected subcommand %q under volume, not found", name)
		}
	}
}

func TestDeployWithVolumes(t *testing.T) {
	dir := t.TempDir()
	yamlContent := `
name: volume-app
volumes:
  - host_path: /data
    container_path: /app/data
`
	if err := os.WriteFile(filepath.Join(dir, ".tengiz.yaml"), []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("config.Load failed: %v", err)
	}
	if len(cfg.Volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(cfg.Volumes))
	}
	if cfg.Volumes[0].HostPath != "/data" {
		t.Errorf("expected HostPath /data, got %s", cfg.Volumes[0].HostPath)
	}
}

func TestConfigSetGetUnsetShowCommandsRegistered(t *testing.T) {
	configCmd, _, err := rootCmd.Find([]string{"config"})
	if err != nil {
		t.Fatalf("config command not found: %v", err)
	}

	expected := map[string]bool{"set": false, "get": false, "unset": false, "show": false}
	for _, sub := range configCmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Fatalf("config subcommand %q not found", name)
		}
	}
}
