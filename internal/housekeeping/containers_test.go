package housekeeping

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func fakeRunner(records *[][]string, outputs map[string]string) execFunc {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		*records = append(*records, append([]string(nil), args...))
		key := strings.Join(args, " ")
		if out, ok := outputs[key]; ok {
			return []byte(out), nil
		}
		return []byte(""), nil
	}
}

func TestOrphanContainersFiltersManagedAndRunning(t *testing.T) {
	var records [][]string
	runner := fakeRunner(&records, map[string]string{
		"ps -a --format {{json .}}": strings.Join([]string{
			`{"ID":"abc","Names":"/tengiz-myapp","State":"exited","Labels":"tengiz-app=myapp,tengiz-env=production"}`,
			`{"ID":"def","Names":"/stray","State":"exited","Labels":""}`,
			`{"ID":"ghi","Names":"/another","State":"running","Labels":""}`,
			`{"ID":"jkl","Names":"/created-one","State":"created","Labels":""}`,
		}, "\n"),
	})

	m := NewManager(runner)
	got, err := m.orphanContainers(context.Background())
	if err != nil {
		t.Fatalf("orphanContainers() error = %v", err)
	}
	want := []string{"def", "jkl"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("orphanContainers() = %v, want %v", got, want)
	}
}

func TestOrphanContainersPropagatesDockerError(t *testing.T) {
	runner := func(ctx context.Context, args ...string) ([]byte, error) {
		return nil, context.DeadlineExceeded
	}
	m := NewManager(runner)
	if _, err := m.orphanContainers(context.Background()); err == nil {
		t.Error("expected error when docker ps fails")
	}
}

func TestNewManagerNonNil(t *testing.T) {
	m := NewManager(RealDocker)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
}
