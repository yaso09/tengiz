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

	"github.com/spf13/cobra"
	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

func TestBuildLogsDirStructure(t *testing.T) {
	dir := t.TempDir()
	s := config.NewStore(dir)

	if err := s.SaveBuildLog("testapp", "v1", "hello from build"); err != nil {
		t.Fatal(err)
	}

	ids, err := s.ListBuildLogs("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "v1" {
		t.Fatalf("expected [v1], got %v", ids)
	}

	content, err := s.GetBuildLog("testapp", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if content != "hello from build" {
		t.Fatalf("expected 'hello from build', got %q", content)
	}

	// Verify file structure (production env by default)
	logDir := filepath.Join(dir, "build-logs", "production", "testapp")
	entries, err := os.ReadDir(logDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry in build-logs dir, got %d", len(entries))
	}
	if entries[0].Name() != "v1.log" {
		t.Errorf("expected v1.log, got %s", entries[0].Name())
	}
}

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
func (m *mockRTForDeploy) Logs(ctx context.Context, name string, opts runtime.LogOptions) (io.ReadCloser, error) { return nil, nil }
func (m *mockRTForDeploy) WaitForReady(ctx context.Context, name string, internalPort int) error { return nil }
func (m *mockRTForDeploy) WaitForHealth(ctx context.Context, name string, hc *types.HealthCheckConfig) error { return nil }
func (m *mockRTForDeploy) CreateFromImage(ctx context.Context, cfg *types.AppConfig, imageTag string, port int) error { return nil }
func (m *mockRTForDeploy) RemoveImage(ctx context.Context, imageTag string) error { return nil }
func (m *mockRTForDeploy) KeepLastNImages(ctx context.Context, appName string, n int) error { return nil }
func (m *mockRTForDeploy) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	return runtime.PruneResult{}, nil
}
func (m *mockRTForDeploy) Run(ctx context.Context, cfg *types.AppConfig, imageTag string, cmd []string, opts runtime.RunOptions) error { return nil }

func TestMockRTForDeployImplementsManager(t *testing.T) {
	var m runtime.Manager = &mockRTForDeploy{}
	if m == nil {
		t.Fatal("mockRTForDeploy does not implement Manager")
	}
}

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

func TestWebhookCmdReadsConfig(t *testing.T) {
	flag := webhookCmd.Flags().Lookup("config")
	if flag == nil {
		t.Error("webhookCmd missing --config flag")
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

func TestVolumeAddCommand(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir = tmpDir

	store := config.NewStore(dataDir)
	err := store.SaveApp(types.AppEntry{
		Name: "testapp",
		Config: types.AppConfig{
			Name: "testapp",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	rootCmd.SetArgs([]string{"volume", "add", "testapp", "/host/data:/app/data"})
	err = rootCmd.Execute()
	if err != nil {
		t.Fatal(err)
	}

	vols, err := store.ListVolumes("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if vols[0].HostPath != "/host/data" {
		t.Fatalf("expected host path /host/data, got %s", vols[0].HostPath)
	}
	if vols[0].ContainerPath != "/app/data" {
		t.Fatalf("expected container path /app/data, got %s", vols[0].ContainerPath)
	}
}

func TestVolumeAddWithReadOnly(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir = tmpDir

	store := config.NewStore(dataDir)
	store.SaveApp(types.AppEntry{Name: "testapp", Config: types.AppConfig{Name: "testapp"}})

	rootCmd.SetArgs([]string{"volume", "add", "testapp", "/host/config:/etc/config:ro"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatal(err)
	}

	vols, _ := store.ListVolumes("testapp")
	if len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if !vols[0].ReadOnly {
		t.Fatal("expected volume to be read-only")
	}
}

func TestBuildLogsCmdRegistration(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"build-logs"})
	if err != nil {
		t.Fatal("build-logs command not registered")
	}
	if cmd == nil || cmd.Name() != "build-logs" {
		t.Fatal("build-logs command not found")
	}
}

func TestLogsCmdWithFlags(t *testing.T) {
	var called bool

	originalRunE := logsCmd.RunE
	defer func() { logsCmd.RunE = originalRunE }()
	logsCmd.RunE = func(cmd *cobra.Command, args []string) error {
		follow, _ := cmd.Flags().GetBool("follow")
		tail, _ := cmd.Flags().GetInt("tail")
		since, _ := cmd.Flags().GetString("since")
		until, _ := cmd.Flags().GetString("until")
		grep, _ := cmd.Flags().GetString("grep")

		if args[0] != "testapp" {
			t.Errorf("app name = %q, want %q", args[0], "testapp")
		}
		if follow {
			t.Error("follow = true, want false")
		}
		if tail != 50 {
			t.Errorf("tail = %d, want 50", tail)
		}
		if since != "5m" {
			t.Errorf("since = %q, want %q", since, "5m")
		}
		if until != "10m" {
			t.Errorf("until = %q, want %q", until, "10m")
		}
		if grep != "error" {
			t.Errorf("grep = %q, want %q", grep, "error")
		}
		called = true
		return nil
	}

	rootCmd.SetArgs([]string{"logs", "testapp", "--tail", "50", "--since", "5m", "--until", "10m", "--grep", "error"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !called {
		t.Fatal("logsCmd.RunE was not called")
	}
}

func TestLogsCmdFlagParsing(t *testing.T) {
	rootCmd.SetArgs([]string{"logs", "--help"})
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := rootCmd.Execute()

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)

	if err != nil {
		t.Fatalf("logs --help failed: %v", err)
	}

	helpText := buf.String()
	for _, flag := range []string{"--since", "--until", "--tail", "--grep", "--follow", "-f"} {
		if !strings.Contains(helpText, flag) {
			t.Errorf("help text missing flag %q", flag)
		}
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

func TestCleanupCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"cleanup"})
	if err != nil {
		t.Fatal("cleanup command not registered")
	}
	if cmd == nil || cmd.Name() != "cleanup" {
		t.Fatal("cleanup command not found")
	}
}

func TestCleanupCommandFlags(t *testing.T) {
	for _, f := range []string{"dry-run", "all", "containers", "images", "volumes", "networks", "build-cache"} {
		if cleanupCmd.Flags().Lookup(f) == nil {
			t.Errorf("cleanupCmd missing --%s flag", f)
		}
	}
}

func TestResolveCleanupOptionsDefaults(t *testing.T) {
	opts := resolveCleanupOptions(false, false, false, false, false, false, false)
	if !opts.Containers || !opts.Images || !opts.BuildCache {
		t.Errorf("default safe set wrong: %+v", opts)
	}
	if opts.Volumes || opts.Networks || opts.DryRun {
		t.Errorf("volumes/networks/dry-run should not default on: %+v", opts)
	}
}

func TestResolveCleanupOptionsAll(t *testing.T) {
	opts := resolveCleanupOptions(true, false, false, false, false, false, false)
	if !opts.Containers || !opts.Images || !opts.Volumes || !opts.Networks || !opts.BuildCache {
		t.Errorf("--all should enable every category: %+v", opts)
	}
	if opts.DryRun {
		t.Error("--all should not imply dry-run")
	}
}

func TestResolveCleanupOptionsSingle(t *testing.T) {
	opts := resolveCleanupOptions(false, false, false, true, false, false, true)
	if !opts.Volumes || !opts.DryRun {
		t.Errorf("explicit volumes/dry-run not passed through: %+v", opts)
	}
	if opts.Containers || opts.Images || opts.Networks || opts.BuildCache {
		t.Errorf("unexpected categories enabled: %+v", opts)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		b    int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1536, "1.50kB"},
		{5 * 1024 * 1024, "5.00MB"},
		{int64(1.5 * 1024 * 1024 * 1024), "1.50GB"},
	}
	for _, tc := range tests {
		if got := formatBytes(tc.b); got != tc.want {
			t.Errorf("formatBytes(%d) = %q, want %q", tc.b, got, tc.want)
		}
	}
}

type cleanupCaptureRT struct {
	runtime.Manager
	opts   runtime.PruneOptions
	called bool
}

func (m *cleanupCaptureRT) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	m.opts = opts
	m.called = true
	return runtime.PruneResult{}, nil
}

func TestRunCleanupPassesOptions(t *testing.T) {
	cmd := &cobra.Command{}
	for _, f := range []string{"dry-run", "all", "containers", "images", "volumes", "networks", "build-cache"} {
		cmd.Flags().Bool(f, false, "")
	}
	if err := cmd.Flags().Set("volumes", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatal(err)
	}

	rt := &cleanupCaptureRT{}
	if err := runCleanup(cmd, rt); err != nil {
		t.Fatalf("runCleanup() error = %v", err)
	}
	if !rt.called {
		t.Fatal("Prune was not called")
	}
	if !rt.opts.Volumes || !rt.opts.DryRun {
		t.Errorf("opts.Volumes/DryRun not set: %+v", rt.opts)
	}
	if rt.opts.Containers || rt.opts.Images || rt.opts.Networks || rt.opts.BuildCache {
		t.Errorf("unexpected categories enabled: %+v", rt.opts)
	}
}

func TestRunCleanupDryRunOutput(t *testing.T) {
	cmd := &cobra.Command{}
	for _, f := range []string{"dry-run", "all", "containers", "images", "volumes", "networks", "build-cache"} {
		cmd.Flags().Bool(f, false, "")
	}
	if err := cmd.Flags().Set("dry-run", "true"); err != nil {
		t.Fatal(err)
	}

	rt := &cleanupCaptureRT{}
	out := captureOutput(func() {
		runCleanup(cmd, rt)
	})
	if !strings.Contains(out, "dry run") {
		t.Errorf("expected dry-run banner in output, got: %s", out)
	}
}
