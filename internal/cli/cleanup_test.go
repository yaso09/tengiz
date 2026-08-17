package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/yaso09/tengiz/internal/runtime"
)

type mockCleanupRuntime struct {
	runtime.Manager
	pruneCalls []runtime.PruneOptions
	dfCalls    int
}

func (m *mockCleanupRuntime) Prune(ctx context.Context, opts runtime.PruneOptions) (runtime.PruneResult, error) {
	m.pruneCalls = append(m.pruneCalls, opts)
	return runtime.PruneResult{Containers: "Total reclaimed space: 0B"}, nil
}

func (m *mockCleanupRuntime) SystemDF(ctx context.Context) (string, error) {
	m.dfCalls++
	return "TYPE\tTOTAL\tACTIVE\tSIZE\tRECLAIMABLE\n", nil
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

func TestCleanupFlags(t *testing.T) {
	if cleanupCmd.Flags().Lookup("yes") == nil {
		t.Fatal("cleanup missing --yes flag")
	}
	if cleanupCmd.Flags().Lookup("volumes") == nil {
		t.Fatal("cleanup missing --volumes flag")
	}
}

func TestRequestVolumeConfirmationYes(t *testing.T) {
	var out bytes.Buffer
	ok, err := requestVolumeConfirmation(strings.NewReader("yes\n"), &out)
	if err != nil {
		t.Fatalf("requestVolumeConfirmation() error = %v", err)
	}
	if !ok {
		t.Error("expected confirmation for 'yes'")
	}
}

func TestRequestVolumeConfirmationNo(t *testing.T) {
	var out bytes.Buffer
	ok, err := requestVolumeConfirmation(strings.NewReader("no\n"), &out)
	if err != nil {
		t.Fatalf("requestVolumeConfirmation() error = %v", err)
	}
	if ok {
		t.Error("expected no confirmation for 'no'")
	}
}

func TestCleanupDefaultRunsSafePrune(t *testing.T) {
	orig := newCleanupRuntime
	defer func() { newCleanupRuntime = orig }()

	cleanupCmd.Flags().Set("yes", "false")

	mock := &mockCleanupRuntime{}
	newCleanupRuntime = func() (runtime.Manager, error) { return mock, nil }

	rootCmd.SetArgs([]string{"cleanup"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("cleanup Execute() error = %v", err)
	}

	if len(mock.pruneCalls) != 1 {
		t.Fatalf("Prune called %d times, want 1", len(mock.pruneCalls))
	}
	if mock.pruneCalls[0].Volumes {
		t.Error("default cleanup passed Volumes=true, want false")
	}
	if mock.dfCalls != 1 {
		t.Errorf("SystemDF called %d times, want 1", mock.dfCalls)
	}
}

func TestCleanupWithVolumesAndYes(t *testing.T) {
	orig := newCleanupRuntime
	defer func() { newCleanupRuntime = orig }()

	mock := &mockCleanupRuntime{}
	newCleanupRuntime = func() (runtime.Manager, error) { return mock, nil }

	rootCmd.SetArgs([]string{"cleanup", "--volumes", "--yes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("cleanup Execute() error = %v", err)
	}

	if len(mock.pruneCalls) != 1 {
		t.Fatalf("Prune called %d times, want 1", len(mock.pruneCalls))
	}
	if !mock.pruneCalls[0].Volumes {
		t.Error("cleanup --volumes --yes did not pass Volumes=true")
	}
}

func TestCleanupVolumesCancelledWithoutConfirmation(t *testing.T) {
	orig := newCleanupRuntime
	defer func() { newCleanupRuntime = orig }()

	cleanupCmd.Flags().Set("yes", "false")

	mock := &mockCleanupRuntime{}
	newCleanupRuntime = func() (runtime.Manager, error) { return mock, nil }

	origStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	cleanup := func() { os.Stdin = origStdin }
	defer cleanup()
	go func() {
		w.Write([]byte("no\n"))
		w.Close()
	}()

	rootCmd.SetArgs([]string{"cleanup", "--volumes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("cleanup Execute() error = %v", err)
	}

	if len(mock.pruneCalls) != 0 {
		t.Errorf("Prune called %d times, want 0 after cancelled confirmation", len(mock.pruneCalls))
	}
}
