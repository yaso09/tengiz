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

func TestHasLabel(t *testing.T) {
	tests := []struct {
		labels string
		key    string
		want   bool
	}{
		{"tengiz-app=myapp,tengiz-env=production", "tengiz-app", true},
		{"tengiz-app=myapp", "tengiz-env", false},
		{"tengiz-env=production", "tengiz-app", false},
		{"", "tengiz-app", false},
		{"random=value,tengiz-app=other", "tengiz-app", true},
	}
	for _, tt := range tests {
		if got := hasLabel(tt.labels, tt.key); got != tt.want {
			t.Errorf("hasLabel(%q, %q) = %v, want %v", tt.labels, tt.key, got, tt.want)
		}
	}
}

func TestSelectCleanupContainers(t *testing.T) {
	output := `{"ID":"aaa111","Name":"/junk","State":"Exited (0) 2 hours ago","Ports":"","Labels":""}
{"ID":"bbb222","Name":"/tengiz-myapp","State":"Exited (0) 1 hour ago","Ports":"","Labels":"tengiz-app=myapp,tengiz-env=production"}
{"ID":"ccc333","Name":"/web","State":"running","Ports":"","Labels":""}
{"ID":"ddd444","Name":"/sidecar","State":"Dead","Ports":"","Labels":"com.example=x"}
{"ID":"eee555","Name":"/staging","State":"Exited (137) 3 days ago","Ports":"","Labels":"tengiz-app=staging,tengiz-env=staging"}
`
	got := selectCleanupContainers(output)
	want := []string{"aaa111", "ddd444"}
	if len(got) != len(want) {
		t.Fatalf("selectCleanupContainers() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("selectCleanupContainers()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
