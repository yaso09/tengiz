package runtime

import (
	"context"
	"testing"
)

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0B", 0},
		{"1B", 1},
		{"500B", 500},
		{"1kB", 1000},
		{"1.5MB", 1500000},
		{"1.787GB", 1787000000},
		{"1KiB", 1024},
		{"1MiB", 1048576},
		{"1GiB", 1073741824},
		{" 2.5 GB ", 2500000000},
	}
	for _, tt := range tests {
		got, err := parseByteSize(tt.in)
		if err != nil {
			t.Fatalf("parseByteSize(%q) error = %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("parseByteSize(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestParseByteSizeInvalid(t *testing.T) {
	if _, err := parseByteSize(""); err == nil {
		t.Error("parseByteSize(\"\") expected error")
	}
	if _, err := parseByteSize("abc"); err == nil {
		t.Error("parseByteSize(\"abc\") expected error")
	}
	if _, err := parseByteSize("10XB"); err == nil {
		t.Error("parseByteSize(\"10XB\") expected error")
	}
}

func TestParsePruneReclaimed(t *testing.T) {
	out := "Deleted Containers:\nabc123\n\nTotal reclaimed space: 1.5MB\n"
	got, err := parsePruneReclaimed(out)
	if err != nil {
		t.Fatalf("parsePruneReclaimed() error = %v", err)
	}
	if got != 1500000 {
		t.Errorf("parsePruneReclaimed() = %d, want 1500000", got)
	}
}

func TestParsePruneReclaimedBuilder(t *testing.T) {
	got, err := parsePruneReclaimed("Total:\t2.1GB\n")
	if err != nil {
		t.Fatalf("parsePruneReclaimed() error = %v", err)
	}
	if got != 2100000000 {
		t.Errorf("parsePruneReclaimed() = %d, want 2100000000", got)
	}
}

func TestParsePruneReclaimedNone(t *testing.T) {
	got, err := parsePruneReclaimed("Total reclaimed space: 0B\n")
	if err != nil {
		t.Fatalf("parsePruneReclaimed() error = %v", err)
	}
	if got != 0 {
		t.Errorf("parsePruneReclaimed() = %d, want 0", got)
	}
}

func TestParsePruneItems(t *testing.T) {
	out := "Deleted Containers:\nabc123\ndef456\n\nTotal reclaimed space: 0B\n"
	items := parsePruneItems(out, "Deleted Containers:")
	want := []string{"abc123", "def456"}
	if len(items) != len(want) {
		t.Fatalf("parsePruneItems() = %v, want %v", items, want)
	}
	for i := range want {
		if items[i] != want[i] {
			t.Errorf("parsePruneItems()[%d] = %q, want %q", i, items[i], want[i])
		}
	}
}

func TestParsePruneItemsEmpty(t *testing.T) {
	items := parsePruneItems("Total reclaimed space: 0B\n", "Deleted Containers:")
	if len(items) != 0 {
		t.Fatalf("parsePruneItems() = %v, want empty", items)
	}
}

func TestParseSystemDFBuildCache(t *testing.T) {
	rows := []byte(`{"Active":"0","Reclaimable":"0B","Size":"0B","TotalCount":"0","Type":"Images"}
{"Active":"0","Reclaimable":"1.2GB","Size":"1.2GB","TotalCount":"0","Type":"Build Cache"}`)
	got, err := parseSystemDFBuildCache(rows)
	if err != nil {
		t.Fatalf("parseSystemDFBuildCache() error = %v", err)
	}
	if got != 1200000000 {
		t.Errorf("parseSystemDFBuildCache() = %d, want 1200000000", got)
	}
}

func TestParseSystemDFBuildCacheMissing(t *testing.T) {
	rows := []byte(`{"Active":"0","Reclaimable":"0B","Size":"0B","TotalCount":"0","Type":"Images"}`)
	got, err := parseSystemDFBuildCache(rows)
	if err != nil {
		t.Fatalf("parseSystemDFBuildCache() error = %v", err)
	}
	if got != 0 {
		t.Errorf("parseSystemDFBuildCache() = %d, want 0", got)
	}
}

func TestSplitRepoTag(t *testing.T) {
	tests := []struct {
		in   string
		repo string
		tag  string
	}{
		{"alpine", "alpine", "latest"},
		{"alpine:latest", "alpine", "latest"},
		{"alpine:3.19", "alpine", "3.19"},
		{"tengiz-apps/myapp:v1", "tengiz-apps/myapp", "v1"},
		{"localhost:5000/myapp:tag", "localhost:5000/myapp", "tag"},
		{"nginx", "nginx", "latest"},
	}
	for _, tt := range tests {
		repo, tag := splitRepoTag(tt.in)
		if repo != tt.repo || tag != tt.tag {
			t.Errorf("splitRepoTag(%q) = (%q, %q), want (%q, %q)", tt.in, repo, tag, tt.repo, tt.tag)
		}
	}
}

func TestShortID(t *testing.T) {
	if got := shortID("sha256:abcdef1234567890"); got != "abcdef123456" {
		t.Errorf("shortID() = %q, want %q", got, "abcdef123456")
	}
}

func TestHasTengizLabel(t *testing.T) {
	if !hasTengizLabel("tengiz-app=myapp") {
		t.Error("hasTengizLabel(\"tengiz-app=myapp\") = false, want true")
	}
	if !hasTengizLabel("tengiz-app=myapp,tengiz-env=production") {
		t.Error("hasTengizLabel with two labels = false, want true")
	}
	if hasTengizLabel("com.docker.compose.project=foo") {
		t.Error("hasTengizLabel(other) = true, want false")
	}
	if hasTengizLabel("") {
		t.Error("hasTengizLabel(\"\") = true, want false")
	}
}

func TestFilterUnmanagedContainers(t *testing.T) {
	out := "web-test\t\n" +
		"myapp\ttengiz-app=myapp,tengiz-env=production\n" +
		"temp-build\tcom.docker.compose.project=foo\n"
	got := filterUnmanagedContainers(out)
	want := []string{"web-test", "temp-build"}
	if len(got) != len(want) {
		t.Fatalf("filterUnmanagedContainers() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("filterUnmanagedContainers()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFilterUnmanagedContainersEmpty(t *testing.T) {
	if got := filterUnmanagedContainers(""); len(got) != 0 {
		t.Fatalf("filterUnmanagedContainers(\"\") = %v, want empty", got)
	}
}

func TestComputeUnusedImagesDangling(t *testing.T) {
	all := "sha256:1111111111aaaa\t<none>:<none>\n" +
		"sha256:2222222222bbbb\talpine:latest\n"
	referenced := []string{"alpine:latest"}
	got := computeUnusedImages(all, referenced, false)
	want := []unusedImage{{ID: "sha256:1111111111aaaa", Ref: "1111111111aa"}}
	if len(got) != len(want) {
		t.Fatalf("computeUnusedImages() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("computeUnusedImages()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestComputeUnusedImagesNoDanglingWithoutAll(t *testing.T) {
	all := "sha256:2222222222bbbb\talpine:latest\n"
	got := computeUnusedImages(all, nil, false)
	if len(got) != 0 {
		t.Fatalf("computeUnusedImages() = %+v, want empty (tagged image needs --all)", got)
	}
}

func TestComputeUnusedImagesAll(t *testing.T) {
	all := "sha256:1111111111aaaa\t<none>:<none>\n" +
		"sha256:2222222222bbbb\talpine:latest\n" +
		"sha256:5555555555eeee\talpine:3.19\n" +
		"sha256:3333333333cccc\ttengiz-apps/myapp:v1\n" +
		"sha256:4444444444dddd\tredis:7\n"
	referenced := []string{"alpine:latest", "sha256:1111111111aaaa"}
	got := computeUnusedImages(all, referenced, true)
	want := []unusedImage{
		{ID: "sha256:5555555555eeee", Ref: "alpine:3.19"},
		{ID: "sha256:4444444444dddd", Ref: "redis:7"},
	}
	if len(got) != len(want) {
		t.Fatalf("computeUnusedImages() = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("computeUnusedImages()[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseDanglingVolumes(t *testing.T) {
	out := "vol1\nvol2\n"
	got := parseDanglingVolumes(out)
	want := []string{"vol1", "vol2"}
	if len(got) != len(want) {
		t.Fatalf("parseDanglingVolumes() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("parseDanglingVolumes()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseDanglingVolumesEmpty(t *testing.T) {
	if got := parseDanglingVolumes(""); len(got) != 0 {
		t.Fatalf("parseDanglingVolumes(\"\") = %v, want empty", got)
	}
}

func TestComputeUnusedNetworks(t *testing.T) {
	lines := []string{
		"54532e5ef3f2 bridge",
		"ecb53337d4ee host",
		"f61bb3e36b11 none",
		"aa11bb22cc33 mynet",
		"dd44ee55ff66 othernet",
	}
	inUse := map[string]bool{"aa11bb22cc33": true}
	got := computeUnusedNetworks(lines, inUse)
	want := []string{"othernet"}
	if len(got) != len(want) {
		t.Fatalf("computeUnusedNetworks() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("computeUnusedNetworks()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestComputeUnusedNetworksEmpty(t *testing.T) {
	if got := computeUnusedNetworks(nil, nil); len(got) != 0 {
		t.Fatalf("computeUnusedNetworks(nil) = %v, want empty", got)
	}
}

func TestStubRemoveImage(t *testing.T) {
	m := NewStub()
	if err := m.RemoveImage(context.Background(), "tengiz-apps/testapp:v1"); err != nil {
		t.Fatalf("RemoveImage() error = %v", err)
	}
}

func TestStubKeepLastNImages(t *testing.T) {
	m := NewStub()
	if err := m.KeepLastNImages(context.Background(), "testapp", 5); err != nil {
		t.Fatalf("KeepLastNImages() error = %v", err)
	}
}
