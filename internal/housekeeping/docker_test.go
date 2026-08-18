package housekeeping

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPruneArgs(t *testing.T) {
	tests := []struct {
		cat      Category
		expected []string
	}{
		{CategoryContainers, []string{"container", "prune", "-f", "--filter", "label!=tengiz-app"}},
		{CategoryImages, []string{"image", "prune", "-f"}},
		{CategoryNetworks, []string{"network", "prune", "-f"}},
		{CategoryCache, []string{"builder", "prune", "-f"}},
		{CategoryVolumes, []string{"volume", "prune", "-f"}},
	}
	for _, tt := range tests {
		got, err := pruneArgs(tt.cat)
		if err != nil {
			t.Fatalf("pruneArgs(%s) error = %v", tt.cat, err)
		}
		if !reflect.DeepEqual(got, tt.expected) {
			t.Errorf("pruneArgs(%s) = %v, want %v", tt.cat, got, tt.expected)
		}
	}
}

func TestPruneArgsUnknownCategory(t *testing.T) {
	if _, err := pruneArgs(Category("bogus")); err == nil {
		t.Fatal("expected error for unknown category")
	}
}

func TestContainerCandidatesArgsProtectTengiz(t *testing.T) {
	args := containerCandidatesArgs()
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "status=exited") {
		t.Errorf("container candidates must target stopped containers, got: %v", args)
	}
	if !strings.Contains(joined, `{{.Label "tengiz-app"}}`) {
		t.Errorf("container candidates must surface the tengiz-app label for Go-side filtering, got: %v", args)
	}
}

func TestImageCandidatesArgsDanglingOnly(t *testing.T) {
	args := imageCandidatesArgs()
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "dangling=true") {
		t.Errorf("image candidates must target dangling images, got: %v", args)
	}
}

func TestNetworkCandidatesArgsUnusedOnly(t *testing.T) {
	args := networkCandidatesArgs()
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "dangling=true") {
		t.Errorf("network candidates must target unused networks, got: %v", args)
	}
}
// fakeDocker writes an executable `docker` shim into a temp dir and prepends it
// to PATH. The shim runs `script` for every invocation. Do NOT call t.Parallel().
func fakeDocker(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "docker")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake docker: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

const dfScript = `#!/bin/sh
if [ "$1" = "system" ] && [ "$2" = "df" ]; then
  printf '%s\n' \
    'TYPE            TOTAL     ACTIVE    SIZE      RECLAIMABLE' \
    'Images          6         1         1.234GB   1.1GB (89%)' \
    'Containers      3         1         23.5kB    12.5kB (53%)' \
    'Local Volumes   1         1         12MB      0B (0%)' \
    'Build Cache     12        0         45.6MB    45.6MB'
  exit 0
fi
exit 1
`

const pruneScript = `#!/bin/sh
case "$1" in
  container|image|network|builder|volume)
    printf 'Total reclaimed space: 1.25MB\n'
    exit 0
    ;;
esac
exit 1
`

const listScript = `#!/bin/sh
case "$1" in
  ps)
    printf '8f2a1bc9 nginx-proxy\n'
    exit 0
    ;;
  images)
    printf '7d3e4f5a <none>:<none>\n'
    exit 0
    ;;
  network)
    printf '9c0b2da1 bridge-net\n'
    exit 0
    ;;
esac
exit 1
`

func TestNewDockerFindsFakeBinary(t *testing.T) {
	fakeDocker(t, "#!/bin/sh\nexit 0\n")
	m, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() error = %v", err)
	}
	if m == nil {
		t.Fatal("NewDocker() returned nil")
	}
}

func TestDockerDiskUsage(t *testing.T) {
	fakeDocker(t, dfScript)
	m, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() error = %v", err)
	}
	u, err := m.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage() error = %v", err)
	}
	if u.ContainersReclaimable != 12500 {
		t.Errorf("ContainersReclaimable = %d, want 12500", u.ContainersReclaimable)
	}
	if u.ImagesReclaimable != 1100000000 {
		t.Errorf("ImagesReclaimable = %d, want 1100000000", u.ImagesReclaimable)
	}
	if u.CacheReclaimable != 45600000 {
		t.Errorf("CacheReclaimable = %d, want 45600000", u.CacheReclaimable)
	}
}

func TestDockerPruneApply(t *testing.T) {
	fakeDocker(t, pruneScript)
	m, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() error = %v", err)
	}
	res, err := m.Prune(context.Background(), Options{Apply: true, Categories: []Category{CategoryContainers, CategoryImages}})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if !res.Applied {
		t.Error("expected Applied=true")
	}
	if res.ReclaimedBytes != 2500000 {
		t.Errorf("ReclaimedBytes = %d, want 2500000", res.ReclaimedBytes)
	}
	if res.ReclaimedByCategory[CategoryContainers] != 1250000 {
		t.Errorf("ReclaimedByCategory[containers] = %d, want 1250000", res.ReclaimedByCategory[CategoryContainers])
	}
}

func TestDockerPruneDryRun(t *testing.T) {
	fakeDocker(t, listScript)
	m, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() error = %v", err)
	}
	res, err := m.Prune(context.Background(), Options{Apply: false})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if res.Applied {
		t.Error("expected Applied=false for dry run")
	}
	if len(res.Candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d: %+v", len(res.Candidates), res.Candidates)
	}
	if res.Candidates[0].ID != "8f2a1bc9" || res.Candidates[0].Name != "nginx-proxy" {
		t.Errorf("candidate[0] = %+v", res.Candidates[0])
	}
}

const protectScript = `#!/bin/sh
case "$1" in
  ps)
    printf '8f2a1bc9 nginx-proxy\n'
    printf 'deadbeef myapp tengiz-app\n'
    exit 0
    ;;
  images)
    exit 0
    ;;
  network)
    exit 0
    ;;
esac
exit 1
`

func TestDockerPruneDryRunProtectsTengizContainers(t *testing.T) {
	fakeDocker(t, protectScript)
	m, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() error = %v", err)
	}
	res, err := m.Prune(context.Background(), Options{Apply: false})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(res.Candidates) != 1 {
		t.Fatalf("expected 1 candidate (labeled container excluded), got %d: %+v", len(res.Candidates), res.Candidates)
	}
	if res.Candidates[0].ID != "8f2a1bc9" || res.Candidates[0].Name != "nginx-proxy" {
		t.Errorf("candidate[0] = %+v", res.Candidates[0])
	}
}

func TestDockerPruneDefaultsToAllSafeCategories(t *testing.T) {
	fakeDocker(t, pruneScript)
	m, err := NewDocker()
	if err != nil {
		t.Fatalf("NewDocker() error = %v", err)
	}
	res, err := m.Prune(context.Background(), Options{Apply: true})
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	// DefaultCategories excludes volumes, so 4 prune commands run
	if len(res.ReclaimedByCategory) != 4 {
		t.Errorf("expected 4 pruned categories, got %d: %+v", len(res.ReclaimedByCategory), res.ReclaimedByCategory)
	}
	if _, pruned := res.ReclaimedByCategory[CategoryVolumes]; pruned {
		t.Error("volumes must never be pruned by default")
	}
}
