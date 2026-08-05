package housekeeping

import (
	"context"
	"reflect"
	"testing"
)

func TestDanglingImagesReturnsIDs(t *testing.T) {
	var records [][]string
	runner := fakeRunner(&records, map[string]string{
		"images -q -f dangling=true": "sha256:111\nsha256:222\n",
	})
	m := NewManager(runner)
	got, err := m.danglingImages(context.Background())
	if err != nil {
		t.Fatalf("danglingImages() error = %v", err)
	}
	want := []string{"sha256:111", "sha256:222"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("danglingImages() = %v, want %v", got, want)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 docker call, got %d", len(records))
	}
}

func TestDanglingImagesEmpty(t *testing.T) {
	m := NewManager(func(ctx context.Context, args ...string) ([]byte, error) {
		return []byte(""), nil
	})
	got, err := m.danglingImages(context.Background())
	if err != nil {
		t.Fatalf("danglingImages() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no dangling images, got %v", got)
	}
}
