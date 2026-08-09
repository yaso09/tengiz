package runtime

import (
	"context"
	"reflect"
	"testing"
)

func TestStubCleanup(t *testing.T) {
	m := NewStub()
	res, err := m.Cleanup(context.Background(), CleanupOptions{
		Containers: true, Images: true, Volumes: true, Networks: true,
	})
	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	empty := CleanupResult{}
	if !reflect.DeepEqual(res, empty) {
		t.Fatalf("stub Cleanup should return empty result, got %+v", res)
	}
}
