package housekeeping

import (
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
	if !strings.Contains(joined, "label!=tengiz-app") {
		t.Errorf("container candidates must exclude tengiz-app label, got: %v", args)
	}
	if !strings.Contains(joined, "status=exited") {
		t.Errorf("container candidates must target stopped containers, got: %v", args)
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