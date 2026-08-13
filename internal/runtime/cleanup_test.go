package runtime

import (
	"context"
	"testing"
)

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

func TestContainerRmArgs(t *testing.T) {
	got := containerRmArgs([]string{"abc", "def"})
	want := []string{"rm", "-f", "abc", "def"}
	if len(got) != len(want) {
		t.Fatalf("containerRmArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("containerRmArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestImageRmiArgs(t *testing.T) {
	got := imageRmiArgs([]string{"tengiz-apps/myapp:v1", "dangling-id"})
	want := []string{"rmi", "-f", "tengiz-apps/myapp:v1", "dangling-id"}
	if len(got) != len(want) {
		t.Fatalf("imageRmiArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("imageRmiArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestVolumeRmArgs(t *testing.T) {
	got := volumeRmArgs([]string{"vol1", "vol2"})
	want := []string{"volume", "rm", "vol1", "vol2"}
	if len(got) != len(want) {
		t.Fatalf("volumeRmArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("volumeRmArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNetworkRmArgs(t *testing.T) {
	got := networkRmArgs([]string{"n1"})
	want := []string{"network", "rm", "n1"}
	if len(got) != len(want) {
		t.Fatalf("networkRmArgs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("networkRmArgs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
