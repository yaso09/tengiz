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

func TestParseContainerInfo(t *testing.T) {
	line := `{"ID":"abc","Names":"/tengiz-myapp-1700000000","State":"exited","Ports":"","Labels":"tengiz-app=myapp,tengiz-env=production,tengiz-deployment=1700000000"}`
	info, ok := parseContainerInfo(line)
	if !ok {
		t.Fatal("parseContainerInfo returned ok=false")
	}
	if info.Name != "tengiz-myapp-1700000000" {
		t.Errorf("Name = %q, want %q", info.Name, "tengiz-myapp-1700000000")
	}
	if info.State != "exited" {
		t.Errorf("State = %q, want %q", info.State, "exited")
	}
	if info.AppName != "myapp" {
		t.Errorf("AppName = %q, want %q", info.AppName, "myapp")
	}
	if info.Env != "production" {
		t.Errorf("Env = %q, want %q", info.Env, "production")
	}
	if info.Deployment != "1700000000" {
		t.Errorf("Deployment = %q, want %q", info.Deployment, "1700000000")
	}
}

func TestParseContainerInfoInvalidJSON(t *testing.T) {
	if _, ok := parseContainerInfo("not-json"); ok {
		t.Fatal("expected ok=false for invalid JSON")
	}
}

func TestParseContainerInfoNameKey(t *testing.T) {
	// Older docker ps JSON emits the "Name" key instead of "Names"
	line := `{"ID":"abc","Name":"/tengiz-myapp-1700000000","State":"exited","Ports":"","Labels":""}`
	info, ok := parseContainerInfo(line)
	if !ok {
		t.Fatal("parseContainerInfo returned ok=false")
	}
	if info.Name != "tengiz-myapp-1700000000" {
		t.Errorf("Name = %q, want %q", info.Name, "tengiz-myapp-1700000000")
	}
}

func TestParseContainerInfoNoLabels(t *testing.T) {
	line := `{"ID":"abc","Names":"/plain","State":"running","Ports":"","Labels":""}`
	info, ok := parseContainerInfo(line)
	if !ok {
		t.Fatal("parseContainerInfo returned ok=false")
	}
	if info.AppName != "" || info.Env != "" || info.Deployment != "" {
		t.Fatalf("expected empty label fields, got %+v", info)
	}
}

func TestFilterCleanableContainers(t *testing.T) {
	containers := []ContainerInfo{
		{Name: "tengiz-myapp-111", State: "exited"},
		{Name: "tengiz-myapp-222", State: "dead"},
		{Name: "tengiz-myapp", State: "exited"},       // protected: current app
		{Name: "tengiz-other", State: "running"},      // running: never clean
		{Name: "tengiz-myapp-333", State: "created"},  // created: never clean
	}
	protected := map[string]bool{"tengiz-myapp": true}
	got := FilterCleanableContainers(containers, protected)
	if len(got) != 2 {
		t.Fatalf("expected 2 cleanable, got %d: %+v", len(got), got)
	}
	for _, c := range got {
		if c.Name == "tengiz-myapp" || (c.State != "exited" && c.State != "dead") {
			t.Errorf("unexpected cleanable container: %+v", c)
		}
	}
}

func TestStubListTengizContainers(t *testing.T) {
	m := NewStub()
	containers, err := m.ListTengizContainers(context.Background())
	if err != nil {
		t.Fatalf("ListTengizContainers() error = %v", err)
	}
	if containers != nil {
		t.Fatalf("expected nil, got %v", containers)
	}
}
