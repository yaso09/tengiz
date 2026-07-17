package preview

import (
	"context"
	"testing"
	"time"

	"github.com/yaso09/tengiz/internal/config"
	"github.com/yaso09/tengiz/internal/runtime"
	"github.com/yaso09/tengiz/internal/types"
)

func TestManagerListFromStore(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)
	rt := runtime.NewStub()
	m := NewManager(dir, store, rt)

	store.AddPreview(types.PreviewEntry{
		AppName:       "myapp",
		PRNumber:      42,
		Branch:        "feature/awesome",
		ContainerName: "tengiz-myapp-pr-42",
		Status:        string(types.PreviewActive),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	})

	list, err := m.List(context.Background(), "myapp")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("List returned %d, want 1", len(list))
	}
}

func TestManagerDeleteFromStore(t *testing.T) {
	dir := t.TempDir()
	store := config.NewStore(dir)
	rt := runtime.NewStub()
	m := NewManager(dir, store, rt)

	store.AddPreview(types.PreviewEntry{
		AppName:       "myapp",
		PRNumber:      42,
		ContainerName: "tengiz-myapp-pr-42",
		Status:        string(types.PreviewActive),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	})

	if err := m.Delete(context.Background(), "myapp", 42); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := store.GetPreview("myapp", 42)
	if err == nil {
		t.Error("expected preview to be deleted")
	}
}
